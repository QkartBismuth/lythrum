package widgets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// ── CDP-рендерер: headless Chromium в отдельном процессе ────────────────
//
// Изоляция отказов (тест 4.4): браузер — чужой процесс; крах JS, кодеков
// или самого браузера виден как ошибка/паника рендерера, которую Manager
// перехватывает и перезапускает с backoff. Ядро и соседние виджеты живы.
//
// Протокол: Chrome DevTools Protocol по WebSocket (coder/websocket, pure Go).

// CDPFactory — фабрика рендереров через браузер. Безопасна для
// одновременных вызовов: каждый виджет получает СВОЙ процесс браузера.
type CDPFactory struct {
	// Bin — путь к браузеру; пусто → автопоиск (Chromium/Chrome/Electron).
	Bin string
}

// кандидатные бинарники в порядке предпочтения.
var browserCandidates = []string{
	"chromium", "chromium-browser", "google-chrome-stable", "google-chrome",
	"electron", "electron43", "electron39",
}

// findBrowser ищет бинарник браузера: LYTHRUM_BROWSER → CDPFactory.Bin → PATH.
func (f CDPFactory) findBrowser() (string, error) {
	if env := os.Getenv("LYTHRUM_BROWSER"); env != "" {
		if p, err := exec.LookPath(env); err == nil {
			return p, nil
		}
		return env, nil // возможно, абсолютный путь
	}
	if f.Bin != "" {
		if p, err := exec.LookPath(f.Bin); err == nil {
			return p, nil
		}
		return f.Bin, nil
	}
	for _, c := range browserCandidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", errors.New("widgets: браузер не найден (LYTHRUM_BROWSER или PATH)")
}

// FindBrowser — экспортированная обёртка для точки сборки (main):
// узнать заранее, есть ли браузер, чтобы выбрать фабрику.
func (f CDPFactory) FindBrowser() (string, error) { return f.findBrowser() }

// Build реализует RendererFactory: процесс браузера → соединение CDP.
func (f CDPFactory) Build(ctx context.Context, w *Widget) (WidgetRenderer, error) {
	bin, err := f.findBrowser()
	if err != nil {
		return nil, err
	}

	// Свободный порт подбираем заранее: надёжнее парсинга stderr.
	port, err := freePort()
	if err != nil {
		return nil, err
	}

	profile, err := os.MkdirTemp("", "lythrum-widget-*")
	if err != nil {
		return nil, err
	}

	// ВАЖНО: никакого позиционного URL — Electron счёл бы его путём к
	// приложению. Навигация делается по CDP (Page.navigate).
	cmd := exec.Command(bin,
		"--headless",
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--disable-extensions",
		"--no-first-run",
		"--user-data-dir="+profile,
		"--remote-debugging-port="+fmt.Sprint(port),
	)
	if err := cmd.Start(); err != nil {
		os.RemoveAll(profile)
		return nil, err
	}

	r := &CDPRenderer{
		bin:     bin,
		cmd:     cmd,
		port:    port,
		profile: profile,
	}

	// Подключение к странице-таргету.
	if err := r.connect(ctx); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// freePort — короткоживущий слушатель на :0.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// ── CDPRenderer ─────────────────────────────────────────────────────────

// CDPRenderer — рендерер одного виджета поверх CDP-соединения.
type CDPRenderer struct {
	bin     string
	cmd     *exec.Cmd
	port    int
	profile string

	conn *websocket.Conn
	next atomic.Int64
	mu   sync.Mutex // сериализация запрос-ответ по WS
}

// cdpTarget — элемент /json/list.
type cdpTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// connect ждёт DevTools-порт и подключается к page-таргету.
func (r *CDPRenderer) connect(ctx context.Context) error {
	base := fmt.Sprintf("http://127.0.0.1:%d", r.port)

	// Порт поднимается асинхронно: поллинг с таймаутом.
	var targets []cdpTarget
	deadline := time.Now().Add(15 * time.Second)
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		resp, err := client.Get(base + "/json/list")
		if err == nil {
			err = json.NewDecoder(resp.Body).Decode(&targets)
			resp.Body.Close()
			if err == nil {
				break
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("widgets: DevTools-порт не открылся: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	// Ищем page-таргет (у Electron их может быть несколько).
	var wsURL string
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" {
			wsURL = t.WebSocketDebuggerURL
			break
		}
	}
	if wsURL == "" {
		return errors.New("widgets: нет page-таргета у браузера")
	}

	// CDP требует Host: localhost — ip-литерал 127.0.0.1 тоже валиден.
	wsURL = strings.Replace(wsURL, "ws://127.0.0.1:", "ws://localhost:", 1)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Host: "localhost"})
	if err != nil {
		return fmt.Errorf("widgets: ws-диал: %w", err)
	}
	r.conn = conn
	return nil
}

// call — CDP-запрос с ожиданием ответа по id.
func (r *CDPRenderer) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if r.conn == nil {
		return nil, errors.New("widgets: соединение закрыто")
	}
	id := r.next.Add(1)
	req, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.conn.Write(wctx, websocket.MessageText, req); err != nil {
		return nil, err
	}

	// Читаем события, пока не придёт ответ с нашим id.
	for {
		_, data, err := r.conn.Read(ctx)
		if err != nil {
			return nil, err
		}
		var msg struct {
			ID    int64 `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // событие не-JSON — пропускаем
		}
		if msg.ID != id {
			continue // событие — читаем дальше
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("widgets: cdp %s: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	}
}

// Load реализует WidgetRenderer: размер вьюпорта + навигация на URL.
func (r *CDPRenderer) Load(ctx context.Context, rawURL string, w, h int) error {
	if _, err := r.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             w,
		"height":            h,
		"deviceScaleFactor": 1,
		"mobile":            false,
	}); err != nil {
		return err
	}
	if _, err := r.call(ctx, "Page.enable", map[string]any{}); err != nil {
		return err
	}
	if _, err := r.call(ctx, "Page.navigate", map[string]any{"url": normalizeURL(rawURL)}); err != nil {
		return err
	}
	// Даём странице время на первый layout; кадр догонит цикл рендера.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(120 * time.Millisecond):
	}
	return nil
}

// normalizeURL — URL без схемы считается file-путём (виджеты с диска).
func normalizeURL(raw string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") ||
		strings.HasPrefix(raw, "file://") || strings.HasPrefix(raw, "data:") {
		return raw
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return raw
	}
	return "file://" + (&url.URL{Path: abs}).String()
}

// Resize реализует WidgetRenderer.
func (r *CDPRenderer) Resize(w, h int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             w,
		"height":            h,
		"deviceScaleFactor": 1,
		"mobile":            false,
	})
	return err
}

// Input реализует WidgetRenderer: ввод в ЛОКАЛЬНЫХ координатах виджета.
func (r *CDPRenderer) Input(ev InputEvent) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch ev.Type {
	case "mouse_down", "mouse_up":
		typ := "mousePressed"
		if ev.Type == "mouse_up" {
			typ = "mouseReleased"
		}
		button := "left"
		if ev.Button == 2 {
			button = "middle"
		} else if ev.Button == 3 {
			button = "right"
		}
		_, err := r.call(ctx, "Input.dispatchMouseEvent", map[string]any{
			"type": typ, "x": ev.X, "y": ev.Y,
			"button": button, "clickCount": 1,
		})
		return err
	case "mouse_move":
		_, err := r.call(ctx, "Input.dispatchMouseEvent", map[string]any{
			"type": "mouseMoved", "x": ev.X, "y": ev.Y,
		})
		return err
	case "wheel":
		_, err := r.call(ctx, "Input.dispatchMouseEvent", map[string]any{
			"type": "mouseWheel", "x": ev.X, "y": ev.Y,
			"deltaX": ev.DX, "deltaY": ev.DY,
		})
		return err
	case "key":
		params := map[string]any{"key": ev.Key}
		if ev.Text != "" {
			params["type"] = "keyDown"
			params["text"] = ev.Text
		} else {
			params["type"] = "rawKeyDown"
		}
		if _, err := r.call(ctx, "Input.dispatchKeyEvent", params); err != nil {
			return err
		}
		up := map[string]any{"key": ev.Key, "type": "keyUp"}
		_, err := r.call(ctx, "Input.dispatchKeyEvent", up)
		return err
	}
	return fmt.Errorf("widgets: неизвестный ввод %q", ev.Type)
}

// Frame реализует WidgetRenderer: Page.captureScreenshot → PNG → image.
func (r *CDPRenderer) Frame(ctx context.Context) (image.Image, error) {
	res, err := r.call(ctx, "Page.captureScreenshot", map[string]any{
		"format": "png",
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(raw))
}

// Close реализует WidgetRenderer: процесс браузера завершается, профиль
// удаляется. Утечки процессов недопустимы.
func (r *CDPRenderer) Close() error {
	if r.conn != nil {
		r.conn.CloseNow()
		r.conn = nil
	}
	if r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}
	r.cmd.Wait()
	return os.RemoveAll(r.profile)
}

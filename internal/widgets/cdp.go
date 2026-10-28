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
	r.rctx, r.rcancel = context.WithCancel(context.Background())
	r.pending = map[int64]chan cdpReply{}

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
//
// Модель конкурентности: единственная горутина-читатель (readLoop)
// ВЛАДЕЕТ conn.Read; все остальные шлют запросы call()'ом и ждут
// future по id. Это демультиплексирует события браузера
// (Runtime.bindingCalled — транспорт IPC-команд страницы), которые
// наивное «читать до ответа» выбрасывало.
type CDPRenderer struct {
	bin     string
	cmd     *exec.Cmd
	port    int
	profile string

	conn *websocket.Conn
	next atomic.Int64

	writeMu sync.Mutex // сериализация записей в WS (чтение — у readLoop)

	pendingMu sync.Mutex
	pending   map[int64]chan cdpReply // id → future ответа

	ipcHook atomic.Value // func(string): IPC-команда страницы (payload JSON)

	rctx    context.Context    // жизнь читателя и всех вызовов
	rcancel context.CancelFunc // Close() отменяет читателя

	closed atomic.Bool // защита от двойного Close
}

// cdpReply — разрешённый future call()'а.
type cdpReply struct {
	result json.RawMessage
	err    error
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

	// Единственный читатель сокета: разруливает ответы и события.
	go r.readLoop()
	return nil
}

// call — CDP-запрос с ожиданием ответа по id (future из pending).
// Запись под мьютексом; чтение делает readLoop и разрешает future.
func (r *CDPRenderer) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if r.conn == nil || r.closed.Load() {
		return nil, errors.New("widgets: соединение закрыто")
	}
	id := r.next.Add(1)
	req, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}

	ch := make(chan cdpReply, 1) // буфер 1: читатель не блокируется, даже если нас бросили
	r.pendingMu.Lock()
	r.pending[id] = ch
	r.pendingMu.Unlock()
	drop := func() {
		r.pendingMu.Lock()
		delete(r.pending, id)
		r.pendingMu.Unlock()
	}

	r.writeMu.Lock()
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = r.conn.Write(wctx, websocket.MessageText, req)
	cancel()
	r.writeMu.Unlock()
	if err != nil {
		drop()
		return nil, err
	}

	// Таймаут дублирует wctx: если страница зависла, call не вечен.
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case rep := <-ch:
		return rep.result, rep.err
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case <-timer.C:
		drop()
		return nil, fmt.Errorf("widgets: cdp %s: таймаут ответа", method)
	case <-r.rctx.Done():
		drop()
		return nil, errors.New("widgets: соединение закрыто")
	}
}

// readLoop — горутина-владелец conn.Read: ответы уходят в futures,
// события Runtime.bindingCalled — в IPC-хук (в отдельной горутине:
// хук сам делает call() и при синхронном вызове упёрся бы в себя).
func (r *CDPRenderer) readLoop() {
	// Ссылка на сокет захватывается один раз: Close() больше не пишет
	// r.conn, а лишь закрывает соединение (Read вернёт ошибку).
	conn := r.conn
	for {
		_, data, err := conn.Read(r.rctx)
		if err != nil {
			r.failPending(err)
			return
		}
		var msg struct {
			ID     int64  `json:"id"` // 0 — событие (id начинаются с 1)
			Method string `json:"method"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result json.RawMessage `json:"result"`
			Params struct {
				Name    string `json:"name"`
				Payload string `json:"payload"`
			} `json:"params"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue // мусорный кадр — пропускаем
		}

		if msg.ID != 0 { // ответ на запрос
			r.pendingMu.Lock()
			ch := r.pending[msg.ID]
			delete(r.pending, msg.ID)
			r.pendingMu.Unlock()
			if ch != nil {
				if msg.Error != nil {
					ch <- cdpReply{err: fmt.Errorf("cdp: %s", msg.Error.Message)}
				} else {
					ch <- cdpReply{result: msg.Result}
				}
			}
			continue
		}

		// Событие: транспорт IPC — binding нашего имени; остальное шум.
		if msg.Method == "Runtime.bindingCalled" && msg.Params.Name == bindingName {
			if fn, ok := r.ipcHook.Load().(func(string)); ok {
				go fn(msg.Params.Payload)
			}
		}
	}
}

// failPending — сокет умер: все ждущие call() получают ошибку.
func (r *CDPRenderer) failPending(err error) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	for id, ch := range r.pending {
		ch <- cdpReply{err: fmt.Errorf("widgets: соединение закрыто: %w", err)}
		delete(r.pending, id)
	}
}

// bindingName — имя binding'а в странице: window.lythrum(payload).
// Объявлено отдельно: страница и композитор должны назвать его одинаково.
const bindingName = "lythrum"

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
	// Runtime: IPC-канал страницы. addBinding ДО навигации — binding
	// живёт во всех контекстах и ждёт window.lythrum(...) сразу.
	if _, err := r.call(ctx, "Runtime.enable", map[string]any{}); err != nil {
		return err
	}
	if _, err := r.call(ctx, "Runtime.addBinding", map[string]any{
		"name": bindingName,
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

// SetIPCHook — колбэк IPC-команд страницы: payload — JSON, который
// страница передала в window.lythrum(...). Ставится Manager'ом сразу
// после фабрики (до Load) и никогда не nil после этого.
func (r *CDPRenderer) SetIPCHook(fn func(string)) {
	r.ipcHook.Store(fn)
}

// DeliverResponse доставляет ответ композитора странице: CustomEvent
// 'lythrum:resp' с detail-строкой (страница делает JSON.parse сама).
// Fire-and-forget: страница могла уйти в навигацию — ошибка доставки
// не падает и не роняет рендерер.
func (r *CDPRenderer) DeliverResponse(payload string) {
	raw, err := json.Marshal(payload) // безопасный JS-литерал строки
	if err != nil {
		return
	}
	expr := fmt.Sprintf("window.dispatchEvent(new CustomEvent('lythrum:resp',{detail:%s}))", raw)
	go func() {
		ctx, cancel := context.WithTimeout(r.rctx, 5*time.Second)
		defer cancel()
		_, _ = r.call(ctx, "Runtime.evaluate", map[string]any{
			"expression":    expr,
			"returnByValue": true,
		})
	}()
}

// Close реализует WidgetRenderer: отмена читателя, сокет, процесс
// браузера, профиль. Утечки процессов недопустимы.
func (r *CDPRenderer) Close() error {
	if r.closed.Swap(true) {
		return nil
	}
	r.rcancel() // читатель выйдет и по отмене контекста
	if r.conn != nil {
		r.conn.CloseNow() // читатель выйдет по ошибке чтения
	}
	if r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}
	_ = r.cmd.Wait()
	return os.RemoveAll(r.profile)
}

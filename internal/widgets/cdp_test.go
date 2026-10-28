package widgets

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lythrum/internal/core"
)

// requireBrowser пропускает тест, если браузера нет.
func requireBrowser(t *testing.T) string {
	t.Helper()
	bin, err := CDPFactory{}.findBrowser()
	if err != nil {
		t.Skip("браузер не найден:", err)
	}
	return bin
}

// writeHTML — страница с монотонным фоном и заголовком.
func writeHTML(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return "file://" + path
}

// Интеграция: реальный браузер по CDP — Load, кадр нужного размера.
func TestCDPLoadAndFrame(t *testing.T) {
	requireBrowser(t)

	dir := t.TempDir()
	page := writeHTML(t, dir, "w.html",
		`<!doctype html><html><body style="margin:0;background:#204080">
		 <h1 style="color:#fff">Lythrum widget</h1></body></html>`)

	w := New(page, core.Rect{X: 0, Y: 0, W: 320, H: 200}, PermNotify)
	f, err := CDPFactory{}.Build(context.Background(), w)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := f.Load(ctx, page, 320, 200); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Кадр: ждём появления (страница должна отрисоваться).
	img := waitFrame(t, f, ctx, 10*time.Second)
	if img.Bounds().Dx() != 320 || img.Bounds().Dy() != 200 {
		t.Fatalf("кадр: %v, want 320x200", img.Bounds())
	}
	// Фон #204080 в углу кадра.
	if c, ok := img.At(0, 0).(color.RGBA); ok && c.B < 0x40 {
		t.Fatalf("фон страницы не отрисовался: %v", c)
	}
}

// Интеграция: крах CDP-процесса → Manager перезапускает виджет (4.4).
func TestCDPProcessCrashRestart(t *testing.T) {
	requireBrowser(t)

	dir := t.TempDir()
	page := writeHTML(t, dir, "w.html", `<!doctype html><html><body>hi</body></html>`)

	ff := &CDPFactory{}
	m := NewManager(ff.Build)
	m.backoffBase, m.backoffMax = 50*time.Millisecond, 500*time.Millisecond

	w := New(page, core.Rect{X: 0, Y: 0, W: 200, H: 100})
	m.Sync([]*Widget{w})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go func() { _ = m.Run(ctx) }()

	// Ждём первую жизнь.
	waitStatus(t, m, w.ID, StatusRunning, 15*time.Second)

	// Убиваем процесс браузера (симуляция краха кодека/JS).
	killed := 0
	for _, p := range m.procs {
		if r, ok := p.getRenderer().(*CDPRenderer); ok && r.cmd.Process != nil {
			r.cmd.Process.Kill()
			killed++
		}
	}
	if killed == 0 {
		t.Fatal("не нашли CDP-процесс для убийства")
	}

	// Manager обязан заметить крах и перезапустить виджет: сначала
	// растёт счётчик (ошибка распространилась), потом статус снова running.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if m.Restarts() >= 1 && m.StatusOf(w.ID) == StatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if m.Restarts() < 1 {
		t.Fatalf("перезапусков: %d, want ≥1", m.Restarts())
	}
	if m.StatusOf(w.ID) != StatusRunning {
		t.Fatalf("после краха статус: %q, want running", m.StatusOf(w.ID))
	}
}

// ── хелперы ─────────────────────────────────────────────────────────────

// waitFrame — кадр рендерера в пределах таймаута.
func waitFrame(t *testing.T, r WidgetRenderer, ctx context.Context, within time.Duration) image.Image {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		im, err := r.Frame(ctx)
		if err == nil && im != nil {
			return im
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("кадр не получен за таймаут")
	return nil
}

func waitStatus(t *testing.T, m *Manager, id string, want Status, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if got := m.StatusOf(id); got == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("статус %q не достигнут за %v (сейчас %q)", want, within, m.StatusOf(id))
}

// Интеграция (Спринт 7): полный круг IPC-канала. Страница зовёт
// window.lythrum(JSON) → binding доставляет запрос в хук → Handler
// (токен → белый список) → DeliverResponse возвращает ответ событием
// 'lythrum:resp' → страница кладёт его в title → тест читает title
// обратно по CDP. Заодно гоняет конкурентные call() под -race.
func TestCDPIPCBindingRoundTrip(t *testing.T) {
	requireBrowser(t)

	dir := t.TempDir()
	w := New("about:blank", core.Rect{X: 0, Y: 0, W: 320, H: 200}, PermNotify)
	page := writeHTML(t, dir, "ipc.html", fmt.Sprintf(
		`<!doctype html><html><head><title>init</title></head><body>
		 <script>
		 window.addEventListener('lythrum:resp', e => { document.title = e.detail; });
		 window.addEventListener('load', () => {
		   window.lythrum(JSON.stringify({token: %q, cmd: 'ping'}));
		 });
		 </script></body></html>`, w.Token))

	r, err := CDPFactory{}.Build(context.Background(), w)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer r.Close()

	cdp := r.(*CDPRenderer)

	// Хук — как у Manager.runOnce: запрос → шлюз → ответ странице.
	cdp.SetIPCHook(func(payload string) {
		var req Request
		if json.Unmarshal([]byte(payload), &req) != nil {
			return
		}
		resp := NewHandler(w, &CompositorAPI{}).Handle(req) // ping: исполнители не нужны
		if data, err := json.Marshal(resp); err == nil {
			cdp.DeliverResponse(string(data))
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := cdp.Load(ctx, page, 320, 200); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Ответ вернулся странице: title стал телом ответа.
	var title string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := cdp.call(ctx, "Runtime.evaluate", map[string]any{
			"expression":    "document.title",
			"returnByValue": true,
		})
		if err == nil {
			var out struct {
				Result struct{ Value string }
			}
			if json.Unmarshal(res, &out) == nil && strings.HasPrefix(out.Result.Value, "{") {
				title = out.Result.Value
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if title != `{"ok":true,"data":"pong"}` {
		t.Fatalf("ответ не вернулся странице: title=%q, want pong-ответ", title)
	}
}

// Интеграция: битый токен отклоняется на полном пути (страница шлёт
// чужой токен — в title ложится отказ, а не исполнение).
func TestCDPIPCBindingWrongToken(t *testing.T) {
	requireBrowser(t)

	dir := t.TempDir()
	w := New("about:blank", core.Rect{X: 0, Y: 0, W: 320, H: 200}, PermNotify)
	page := writeHTML(t, dir, "ipc.html",
		`<!doctype html><html><head><title>init</title></head><body>
		 <script>
		 window.addEventListener('lythrum:resp', e => { document.title = e.detail; });
		 window.addEventListener('load', () => {
		   window.lythrum(JSON.stringify({token: 'чужой', cmd: 'ping'}));
		 });
		 </script></body></html>`)

	r, err := CDPFactory{}.Build(context.Background(), w)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer r.Close()

	cdp := r.(*CDPRenderer)
	cdp.SetIPCHook(func(payload string) {
		var req Request
		if json.Unmarshal([]byte(payload), &req) != nil {
			return
		}
		resp := NewHandler(w, &CompositorAPI{}).Handle(req)
		if data, err := json.Marshal(resp); err == nil {
			cdp.DeliverResponse(string(data))
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := cdp.Load(ctx, page, 320, 200); err != nil {
		t.Fatalf("Load: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := cdp.call(ctx, "Runtime.evaluate", map[string]any{
			"expression":    "document.title",
			"returnByValue": true,
		})
		if err == nil {
			var out struct {
				Result struct{ Value string }
			}
			if json.Unmarshal(res, &out) == nil && strings.HasPrefix(out.Result.Value, "{") {
				if !strings.Contains(out.Result.Value, "токен отклонён") {
					t.Fatalf("чужой токен прошёл: %q", out.Result.Value)
				}
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("ответ об отказе не вернулся странице")
}

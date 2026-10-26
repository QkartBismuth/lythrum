package widgets

import (
	"context"
	"image"
	"image/color"
	"os"
	"path/filepath"
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

package kernel

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"lythrum/internal/canvas"
	"lythrum/internal/core"
	"lythrum/internal/input"
	"lythrum/internal/module"
	"lythrum/internal/session"
)

// harness — интеграционный стенд: InputProcessor → шина → Kernel (2.8).
type harness struct {
	bus  *module.ChanPublisher
	ip   *input.InputProcessor
	k    *Kernel
	pipe func() // прокидывает накопившиеся события шины в ядро
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	bus := &module.ChanPublisher{C: make(chan module.Event, 64)}
	k := New(canvas.New(1920, 1080), session.NewManager(filepath.Join(t.TempDir(), "session.json")))
	ip := input.NewProcessor(bus, input.NewLayoutMachine())
	return &harness{
		bus: bus,
		ip:  ip,
		k:   k,
		pipe: func() {
			for done := false; !done; {
				select {
				case ev := <-bus.C:
					k.Apply(ev)
				default:
					done = true
				}
			}
		},
	}
}

// TestPipelineSingleMetaResetsCamera — полный путь: клавиатура → ввод → ядро.
func TestPipelineSingleMetaResetsCamera(t *testing.T) {
	h := newHarness(t)
	h.k.State.Cam = core.Camera{X: 500, Y: 300, Zoom: 2.5}

	h.ip.HandleKeyEvent(input.KeyEvent{Key: input.KeyMeta, State: input.Press})
	h.ip.HandleKeyEvent(input.KeyEvent{Key: input.KeyMeta, State: input.Release})
	h.pipe()

	if !h.k.State.Cam.IsDefault() {
		t.Fatalf("камера не сброшена в дефолт: %+v", h.k.State.Cam)
	}
}

// TestPipelineCapsLockLayout — Caps Lock доезжает до ядра как смена раскладки.
func TestPipelineCapsLockLayout(t *testing.T) {
	h := newHarness(t)
	if h.ip.Layout() != "us" {
		t.Fatalf("стартовая раскладка = %q, want us", h.ip.Layout())
	}
	h.ip.HandleKeyEvent(input.KeyEvent{Key: input.KeyCapsLock, State: input.Press})
	h.pipe()
	if h.ip.Layout() != "ru" {
		t.Fatalf("Caps Lock не переключил раскладку: %q", h.ip.Layout())
	}
}

// TestCycleFocusCentersCamera — Meta+Tab: фокус по кольцу + плавный
// доворот камеры на окно (за 1 тик позиции монотонны, в конце — центр).
func TestCycleFocusCentersCamera(t *testing.T) {
	h := newHarness(t)
	far := h.k.State.AddWindow("Far", core.Rect{X: 4000, Y: 3000, W: 400, H: 300}) // ID 1, стартовый фокус
	first := h.k.State.AddWindow("First", core.Rect{X: 0, Y: 0, W: 200, H: 100})   // ID 2

	// Один Tab: Far → First. Второй Tab: First → Far (кольцо).
	h.ip.HandleKeyEvent(input.KeyEvent{Key: input.KeyTab, Mods: input.ModMeta, State: input.Press})
	h.pipe()
	if w, ok := h.k.State.Window(h.k.State.Focus); !ok || w.ID != first.ID {
		t.Fatalf("первый Tab: фокус должен перейти на First, got %+v", w)
	}

	h.ip.HandleKeyEvent(input.KeyEvent{Key: input.KeyTab, Mods: input.ModMeta, State: input.Press})
	h.pipe()

	w, ok := h.k.State.Window(h.k.State.Focus)
	if !ok || w.ID != far.ID {
		t.Fatalf("второй Tab: фокус должен вернуться на Far, got %+v", w)
	}

	// Анимация: тикаем до завершения, камера обязана прийти в центр окна.
	for i := 0; i < 120 && h.k.tween != nil; i++ {
		h.k.Tick(1.0 / 60)
	}
	if h.k.tween != nil {
		t.Fatal("твин камеры не завершился")
	}
	want := h.k.State.CenterCamPos(w)
	if h.k.State.Cam.X != want.X || h.k.State.Cam.Y != want.Y {
		t.Fatalf("камера не в центре окна: got (%.2f, %.2f), want (%.2f, %.2f)",
			h.k.State.Cam.X, h.k.State.Cam.Y, want.X, want.Y)
	}
}

// TestTileMetaArrows — Meta+Стрелки кладут окно в половину фрустума.
func TestTileMetaArrows(t *testing.T) {
	h := newHarness(t)
	h.k.State.AddWindow("W", core.Rect{X: 10, Y: 10, W: 500, H: 400})

	h.ip.HandleKeyEvent(input.KeyEvent{Key: input.KeyLeft, Mods: input.ModMeta, State: input.Press})
	h.pipe()

	w := h.k.State.Windows()[0]
	vp := h.k.State.Cam.Frustum(1920, 1080)
	if w.Rect != core.TileDirection(core.DirLeft, vp) {
		t.Fatalf("окно не в левой половине: %+v", w.Rect)
	}
}

// TestMinimizeAllSleeps — Meta+D сворачивает всё; куллинг отправляет в сон.
func TestMinimizeAllSleeps(t *testing.T) {
	h := newHarness(t)
	h.k.State.AddWindow("A", core.Rect{X: 0, Y: 0, W: 100, H: 100})
	h.k.State.AddWindow("B", core.Rect{X: 100, Y: 0, W: 100, H: 100})

	h.ip.HandleKeyEvent(input.KeyEvent{Key: input.KeyD, Mods: input.ModMeta, State: input.Press})
	h.pipe()
	h.k.Tick(0.016)

	for _, w := range h.k.State.Windows() {
		if !w.Minimized || !w.Sleeping {
			t.Fatalf("окно %q не свернуто/не спит: %+v", w.Title, w)
		}
	}
}

// TestSessionSaveOnEvent — EvSessionSave пишет файл, Load читает обратно.
func TestSessionSaveOnEvent(t *testing.T) {
	h := newHarness(t)
	h.k.State.AddWindow("Persist", core.Rect{X: 7, Y: 8, W: 90, H: 80})
	h.k.State.Cam = core.Camera{X: 5, Y: 6, Zoom: 1.25}

	h.k.Apply(module.Event{Type: module.EvSessionSave})

	snap, err := h.k.sess.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.Camera.X != 5 || snap.Camera.Zoom != 1.25 || len(snap.Windows) != 1 {
		t.Fatalf("снапшот исказился: %+v", snap)
	}
}

// TestLaunchHookIsolated — системные команды идут через hook (в тестах — заглушку).
func TestLaunchHookIsolated(t *testing.T) {
	h := newHarness(t)
	var called atomic.Int32
	done := make(chan struct{})
	h.k.Launch = func(cmdline string) {
		called.Store(1)
		close(done)
	}

	h.k.Apply(module.Event{Type: module.EvLaunch, Str: "echo hi"})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hook запуска не вызван")
	}
	if called.Load() != 1 {
		t.Fatal("hook вызван некорректно")
	}
}

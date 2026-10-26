package kernel

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"lythrum/internal/canvas"
	"lythrum/internal/core"
	"lythrum/internal/input"
	"lythrum/internal/module"
	"lythrum/internal/render"
	"lythrum/internal/session"
	"lythrum/internal/widgets"
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

// Тестовая картинка: карточка для дропа (PNG 1×1).
var pngBytes = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, // сигнатура
}

// 3.4/ядро — дроп файла создаёт карточку Live Preview на холсте.
func TestDroppedFileBecomesCard(t *testing.T) {
	h := newHarness(t)

	// Валидный PNG через image/png: генерируем честный файл.
	img := core.Rect{X: 10, Y: 20, W: 160, H: 120}
	_ = img
	path := filepath.Join(t.TempDir(), "shot.png")
	writeTestPNG(t, path, 8, 6)

	h.bus.Publish(module.Event{
		Type: module.EvDroppedFile,
		Str:  path,
		Vec:  core.Vec2{X: 55, Y: 66},
	})
	h.pipe()

	cards := h.k.State.Cards()
	if len(cards) != 1 {
		t.Fatalf("карточек после дропа: %d, want 1", len(cards))
	}
	c := cards[0]
	if c.Path != path || c.Kind != "image" {
		t.Fatalf("поля карточки: path=%q kind=%q", c.Path, c.Kind)
	}
	if c.Rect.X != 55 || c.Rect.Y != 66 {
		t.Fatalf("позиция карточки: %v, want точка дропа (55,66)", c.Rect)
	}

	// Перемещение и удаление карточки — теми же событиями.
	h.bus.Publish(module.Event{
		Type: module.EvCardMove, Str: c.ID, Vec: core.Vec2{X: 1, Y: 2},
	})
	h.pipe()
	if moved, _ := h.k.State.Card(c.ID); moved.Rect.X != 1 || moved.Rect.Y != 2 {
		t.Fatalf("карточка не переместилась: %v", moved.Rect)
	}
	h.bus.Publish(module.Event{Type: module.EvCardRemove, Str: c.ID})
	h.pipe()
	if len(h.k.State.Cards()) != 0 {
		t.Fatal("карточка не удалена")
	}
}

// Дроп неизвестного файла не создаёт карточку и не паникует.
func TestDroppedUnknownFileIgnored(t *testing.T) {
	h := newHarness(t)
	h.bus.Publish(module.Event{
		Type: module.EvDroppedFile,
		Str:  "/nonexistent/archive.zip",
		Vec:  core.Vec2{},
	})
	h.pipe()
	if len(h.k.State.Cards()) != 0 {
		t.Fatal("для unknown-медиа не должно быть карточки")
	}
}

// writeTestPNG пишет валидный PNG w×h (синхронная генерация image/png).
func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// 4.4/4.7 (ядро) — события виджетов: create → move → remove.
func TestWidgetLifecycleEvents(t *testing.T) {
	h := newHarness(t)

	url := "file:///tmp/counter.html"
	h.bus.Publish(module.Event{Type: module.EvWidgetCreate, Str: url, Vec: core.Vec2{X: 40, Y: 50}})
	h.pipe()

	ws := h.k.State.Widgets()
	if len(ws) != 1 {
		t.Fatalf("виджетов: %d, want 1", len(ws))
	}
	w := ws[0]
	if w.URL != url || w.Rect.X != 40 || w.Rect.Y != 50 {
		t.Fatalf("поля виджета: %+v", w)
	}
	if w.Token == "" {
		t.Fatal("IPC-токен пуст — виджет не сможет аутентифицироваться")
	}

	// Перемещение.
	h.bus.Publish(module.Event{Type: module.EvWidgetMove, Str: w.ID, Vec: core.Vec2{X: 10, Y: 10}})
	h.pipe()
	if moved, _ := h.k.State.Widget(w.ID); moved.Rect.X != 10 || moved.Rect.Y != 10 {
		t.Fatalf("виджет не переместился: %v", moved.Rect)
	}

	// Удаление.
	h.bus.Publish(module.Event{Type: module.EvWidgetRemove, Str: w.ID})
	h.pipe()
	if len(h.k.State.Widgets()) != 0 {
		t.Fatal("виджет не удалён")
	}
}

// 4.7 — панорама: виджеты вне фрустума помечаются спящими; ввод роутится
// только в видимый виджет.
func TestPanoramaSleepsWidgetsAndRoutesInput(t *testing.T) {
	h := newHarness(t)

	// Два виджета по горизонтали, окно между ними.
	a := widgets.New("file:///a.html", core.Rect{X: 0, Y: 0, W: 200, H: 200}, widgets.PermNotify)
	b := widgets.New("file:///b.html", core.Rect{X: 3000, Y: 0, W: 200, H: 200})
	h.k.State.AddWidget(a)
	h.k.State.AddWidget(b)
	h.k.State.AddWindow("Terminal", core.Rect{X: 600, Y: 0, W: 400, H: 300})

	// Камера в дефолте: a видим (0..1920), b — далеко за фрустумом.
	cam := h.k.State.Cam
	for _, wg := range h.k.State.Widgets() {
		sleep := render.ShouldSleep(cam, h.k.State.ViewportW, h.k.State.ViewportH, wg.Rect)
		if wg.ID == a.ID && sleep {
			t.Fatal("виджет a в фрустуме — не должен спать")
		}
		if wg.ID == b.ID && !sleep {
			t.Fatal("виджет b за фрустумом — должен спать")
		}
	}

	// Ввод по экрану: клик по виджету a (зум 1: экран == холст).
	hit, local, ok := widgets.HitAt(h.k.State.Widgets(), cam, 1920, 1080, core.Vec2{X: 100, Y: 50})
	if !ok || hit.ID != a.ID || local.X != 100 || local.Y != 50 {
		t.Fatalf("ввод не попал в виджет a: %v %+v ok=%v", hit, local, ok)
	}

	// Панорама вправо на 3000: теперь b в фрустуме, a — нет.
	h.k.State.Cam.X = 3000
	cam = h.k.State.Cam
	if !render.ShouldSleep(cam, 1920, 1080, a.Rect) {
		t.Fatal("после панорамы a должен спать")
	}
	if render.ShouldSleep(cam, 1920, 1080, b.Rect) {
		t.Fatal("после панорамы b должен быть видим")
	}

	// Ввод теперь попадает в b.
	hit, _, ok = widgets.HitAt(h.k.State.Widgets(), cam, 1920, 1080, core.Vec2{X: 200, Y: 50})
	if !ok || hit.ID != b.ID {
		t.Fatalf("после панорамы ввод должен идти в b: %v ok=%v", hit, ok)
	}
}

package kernel

import (
	"context"
	"image"
	"sync/atomic"
	"testing"
	"time"

	"lythrum/internal/core"
	"lythrum/internal/render"
	"lythrum/internal/widgets"
)

// 5.5 — frame-suspension: 50 окон и панорама за пределы. Окна вне фрустума
// получают флаг Sleeping (wayland-бэкенд перестаёт слать frame-events),
// виджеты — перестают кадрить (CPU приложений не растёт).
func TestFrameSuspension50Windows(t *testing.T) {
	h := newHarness(t)

	// 50 окон в колонку: при зуме 1 видно ~7 штук.
	const nWindows = 50
	for i := range nWindows {
		h.k.State.AddWindow("w", core.Rect{
			X: float64(i) * 300, Y: 0, W: 250, H: 200,
		})
	}
	ws := []*widgets.Widget{
		widgets.New("file:///a.html", core.Rect{X: 0, Y: 1000, W: 200, H: 200}),
		widgets.New("file:///b.html", core.Rect{X: 400, Y: 1000, W: 200, H: 200}),
		widgets.New("file:///c.html", core.Rect{X: 800, Y: 1000, W: 200, H: 200}),
	}
	for _, wg := range ws {
		h.k.State.AddWidget(wg)
	}

	visible := h.k.State.ApplyCulling()
	if visible == 0 || visible >= nWindows {
		t.Fatalf("видимых окон в старте: %d — куллинг не работает", visible)
	}

	// Панорама в пустоту: НИ одно окно не пересекает фрустум.
	h.k.State.Cam.X = 50000
	h.k.State.Cam.Y = 50000
	if got := h.k.State.ApplyCulling(); got != 0 {
		t.Fatalf("после панорамы видимых: %d, want 0", got)
	}
	for _, w := range h.k.State.Windows() {
		if !w.Sleeping {
			t.Fatalf("окно %d не уснуло — CPU клиента будет гореть", w.ID)
		}
	}

	// Виджеты: кадринг останавливается после SetSleep.
	ff := &countingFactory{}
	wm := widgets.NewManager(ff.build)
	defer wm.Stop()
	wm.Sync(ws)

	// Ждём, пока рендереры начнут кадрить.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && ff.frames() < 6 {
		time.Sleep(5 * time.Millisecond)
	}
	if ff.frames() < 6 {
		t.Fatalf("виджеты не кадрят до сна: %d кадров", ff.frames())
	}

	// «Панорама уводит виджеты за фрустум» → main loop усыпляет их.
	for _, wg := range ws {
		wm.SetSleep(wg.ID, render.ShouldSleep(h.k.State.Cam, 1920, 1080, wg.Rect))
	}

	// За окно наблюдения счётчик кадров НЕ растёт (кадринг приостановлен).
	// Сначала стабилизация: кадры, уже летевшие в момент SetSleep (один
	// ин-флайт вызов на виджет), должны доиграть до замера, иначе строгая
	// проверка «ровно ноль» ловит собственную гонку замера.
	time.Sleep(100 * time.Millisecond)
	frozen := ff.frames()
	time.Sleep(250 * time.Millisecond)
	if got := ff.frames(); got != frozen {
		t.Fatalf("спящие виджеты продолжили кадрить: %d → %d", frozen, got)
	}

	// Панорама обратно — кадринг возобновляется.
	h.k.State.Cam = core.NewCamera()
	for _, wg := range ws {
		wm.SetSleep(wg.ID, render.ShouldSleep(h.k.State.Cam, 1920, 1080, wg.Rect))
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && ff.frames() <= frozen {
		time.Sleep(5 * time.Millisecond)
	}
	if ff.frames() <= frozen {
		t.Fatal("после пробуждения кадринг не возобновился")
	}
}

// countingFactory — фабрика со счётчиком кадров всех созданных рендереров.
type countingFactory struct {
	framesTotal atomic.Int64
}

func (cf *countingFactory) build(ctx context.Context, w *widgets.Widget) (widgets.WidgetRenderer, error) {
	return &countingRenderer{factory: cf}, nil
}

func (cf *countingFactory) frames() int64 { return cf.framesTotal.Load() }

// countingRenderer — рендерер, считающий запросы кадров.
type countingRenderer struct{ factory *countingFactory }

func (cr *countingRenderer) Load(ctx context.Context, url string, w, h int) error {
	return nil
}
func (cr *countingRenderer) Resize(w, h int) error             { return nil }
func (cr *countingRenderer) Input(ev widgets.InputEvent) error { return nil }
func (cr *countingRenderer) Frame(ctx context.Context) (image.Image, error) {
	cr.factory.framesTotal.Add(1)
	return image.NewRGBA(image.Rect(0, 0, 4, 4)), nil
}
func (cr *countingRenderer) Close() error { return nil }

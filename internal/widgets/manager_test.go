package widgets

import (
	"context"
	"image"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lythrum/internal/core"
)

// ── Тестовая фабрика рендереров ─────────────────────────────────────────

type fakeRenderer struct {
	id        string
	url       string
	mu        sync.Mutex
	frames    int64
	resizes   int64
	lastW     int64 // атомарно: последний шириной resize
	inputs    []InputEvent
	closed    bool
	frameGate chan struct{} // если не nil, Frame блокируется до закрытия
}

func (f *fakeRenderer) Load(ctx context.Context, url string, w, h int) error {
	f.mu.Lock()
	f.url = url
	f.mu.Unlock()
	return nil
}

func (f *fakeRenderer) Resize(w, h int) error {
	atomic.AddInt64(&f.resizes, 1)
	atomic.StoreInt64(&f.lastW, int64(w))
	return nil
}

func (f *fakeRenderer) Input(ev InputEvent) error {
	f.mu.Lock()
	f.inputs = append(f.inputs, ev)
	f.mu.Unlock()
	return nil
}

func (f *fakeRenderer) Frame(ctx context.Context) (image.Image, error) {
	if f.frameGate != nil {
		select {
		case <-f.frameGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	atomic.AddInt64(&f.frames, 1)
	return image.NewRGBA(image.Rect(0, 0, 4, 4)), nil
}

func (f *fakeRenderer) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeRenderer) inputCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.inputs)
}

type fakeFactory struct {
	mu      sync.Mutex
	made    []*fakeRenderer
	calls   int // всего вызовов фабрики (включая паникующие)
	panicOn int // вызов (1-based), на котором паниковать
}

func (ff *fakeFactory) build(ctx context.Context, w *Widget) (WidgetRenderer, error) {
	ff.mu.Lock()
	ff.calls++
	n := ff.calls
	ff.mu.Unlock()
	if ff.panicOn == n {
		panic("CDP process boom")
	}
	ff.mu.Lock()
	defer ff.mu.Unlock()
	fr := &fakeRenderer{id: w.ID}
	ff.made = append(ff.made, fr)
	return fr, nil
}

func (ff *fakeFactory) count() int {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	return len(ff.made)
}

// callsCount — всего вызовов фабрики (включая паниковавшие).
func (ff *fakeFactory) callsCount() int {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	return ff.calls
}

// get — рендерер по индексу (nil, если ещё не создан).
func (ff *fakeFactory) get(i int) *fakeRenderer {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	if i >= len(ff.made) {
		return nil
	}
	return ff.made[i]
}

// byID — рендерер конкретного виджета (порядок фабрикаций не важен:
// map-итерация в Sync рандомизирована).
func (ff *fakeFactory) byID(id string) *fakeRenderer {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	for _, fr := range ff.made {
		if fr.id == id {
			return fr
		}
	}
	return nil
}

// ── 4.4: крах рендерера изолирован, виджет перезапускается ─────────────

func TestRendererCrashIsolatedAndRestarted(t *testing.T) {
	ff := &fakeFactory{panicOn: 1} // первая жизнь — паника «CDP-процесса»
	m := NewManager(ff.build)
	defer m.Stop()
	m.backoffBase, m.backoffMax = 5*time.Millisecond, 30*time.Millisecond

	w := New("file:///widget.html", core.Rect{X: 0, Y: 0, W: 200, H: 100})
	m.Sync([]*Widget{w})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.Run(ctx) }()

	// Ждём восстановления: вторая жизнь должна закадрить кадр.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.StatusOf(w.ID) == StatusRunning && m.Frame(w.ID) != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := m.StatusOf(w.ID); got != StatusRunning {
		t.Fatalf("статус после краха: %q, want running", got)
	}
	if m.Frame(w.ID) == nil {
		t.Fatal("кадр после восстановления не появился")
	}
	if m.Restarts() < 1 {
		t.Fatalf("перезапусков: %d, want ≥1", m.Restarts())
	}
	// Вызовов фабрики ≥2 (первый паниковал), успешных ≥1.
	if ff.callsCount() < 2 {
		t.Fatalf("фабрика вызвана %d раз, want ≥2", ff.callsCount())
	}
	if ff.count() < 1 {
		t.Fatalf("успешных сборок: %d, want ≥1", ff.count())
	}

	// Удаление виджета: процесс останавливается.
	m.Sync(nil)
	time.Sleep(20 * time.Millisecond)
	if got := m.StatusOf(w.ID); got != StatusStopped {
		t.Fatalf("после удаления статус: %q, want stopped", got)
	}
}

// Отложенный ввод: клик до готовности рендерера доходит после Load.
func TestInputDeferredUntilReady(t *testing.T) {
	ff := &fakeFactory{}
	m := NewManager(ff.build)
	defer m.Stop()
	m.backoffBase, m.backoffMax = 5*time.Millisecond, 30*time.Millisecond

	w := New("file:///w.html", core.Rect{X: 0, Y: 0, W: 100, H: 100})
	m.Sync([]*Widget{w})

	// Клик сразу: рендерер ещё грузится.
	m.Input(w.ID, InputEvent{Type: "mouse_down", X: 5, Y: 7, Button: 1})

	// Ждём рабочее состояние.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && m.StatusOf(w.ID) != StatusRunning {
		time.Sleep(5 * time.Millisecond)
	}

	// Отложенный ввод доставлен ровно один раз.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fr := ff.get(0); fr != nil && fr.inputCount() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	fr := ff.get(0)
	if fr == nil || fr.inputCount() != 1 {
		t.Fatalf("отложенных событий: %v, want 1", fr)
	}
}

// ── 4.7: сон виджетов вне фрустума ──────────────────────────────────────

func TestWidgetsSleepOutsideFrustum(t *testing.T) {
	ff := &fakeFactory{}
	m := NewManager(ff.build)
	defer m.Stop()
	m.backoffBase, m.backoffMax = 5*time.Millisecond, 30*time.Millisecond

	// Два виджета рядом; камера смотрит на них.
	a := New("file:///a.html", core.Rect{X: 0, Y: 0, W: 200, H: 200})
	b := New("file:///b.html", core.Rect{X: 300, Y: 0, W: 200, H: 200})
	m.Sync([]*Widget{a, b})

	// Спим оба (панорама увела их за фрустум).
	m.SetSleep(a.ID, true)
	m.SetSleep(b.ID, true)

	pa := m.procs[a.ID]
	if !pa.isSleeping() {
		t.Fatal("виджет a должен спать")
	}
	if got := m.StatusOf(a.ID); got != StatusSleeping {
		t.Fatalf("статус спящего: %q, want sleeping", got)
	}

	// Пробуждение.
	m.SetSleep(b.ID, false)
	if got := m.StatusOf(b.ID); got != StatusRunning {
		t.Fatalf("статус пробуждённого: %q, want running", got)
	}

	// Resize по Sync доходит до рендерера ВИДЖЕТА a: ищем его по ID
	// (порядок фабрикаций не детерминирован — map-итерация в Sync).
	var fr *fakeRenderer
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && fr == nil {
		time.Sleep(5 * time.Millisecond)
		fr = ff.byID(a.ID)
	}
	if fr == nil {
		t.Fatal("рендерер не создан")
	}
	a.Rect.W = 400
	m.Sync([]*Widget{a, b})
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&fr.lastW) == 400 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt64(&fr.lastW) != 400 {
		t.Fatalf("resize не доставлен, lastW=%d", atomic.LoadInt64(&fr.lastW))
	}
}

// Компиляционная проверка: fakeRenderer удовлетворяет интерфейсу.
var _ WidgetRenderer = (*fakeRenderer)(nil)

package widgets

import (
	"context"
	"image"
	"log"
	"sync"
	"time"

	"lythrum/internal/module"
)

// Manager — модуль виджетов: владеет рендерерами, изолирует их отказы
// и синхронизирует жизненный цикл с желаемым состоянием холста.
//
// Контракт владения: геометрию виджетов меняет ТОЛЬКО main loop (через
// canvas.State), затем одним вызовом Sync докладывает её менеджеру.
// Manager в ответ запускает/останавливает/сейзит/усыпляет рендереры —
// в своей горутине, с recover() и backoff (паттерн Supervisor).
type Manager struct {
	factory RendererFactory

	// backoff конфигурируется для тестов.
	backoffBase time.Duration
	backoffMax  time.Duration

	mu       sync.RWMutex
	procs    map[string]*proc
	restarts int // метрика для Control Center
}

// proc — жизненный цикл одного рендерера.
type proc struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	spec     *Widget        // СНАПШОТ желаемого состояния (замена целиком под mu)
	r        WidgetRenderer // активный рендерер; nil — между жизнями
	status   Status
	frame    image.Image  // последний скомпонованный кадр
	sleeping bool         // вне фрустума: кадры не запрашиваем
	pending  []InputEvent // ввод, пришедший до готовности рендерера

	pendingResize *sizeWH // resize, пришедший до готовности рендерера
}

// NewManager создаёт менеджер с фабрикой рендереров.
func NewManager(factory RendererFactory) *Manager {
	return &Manager{
		factory:     factory,
		procs:       make(map[string]*proc),
		backoffBase: 200 * time.Millisecond,
		backoffMax:  5 * time.Second,
	}
}

// Name — module.Module.
func (m *Manager) Name() string { return "widgets" }

// Run — жизненный цикл модуля: чистое ожидание отмены (вся работа в
// горутинах виджетов, запущенных Sync).
func (m *Manager) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// Restarts — суммарное число перезапусков рендереров (метрика).
func (m *Manager) Restarts() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.restarts
}

// WidgetsCount — число активных процессов виджетов (метрика).
func (m *Manager) WidgetsCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.procs)
}

// Sync приводит процессы виджетов к желаемому состоянию. Вызывается из
// main loop после Apply: новые виджеты → запуск, исчезнувшие → останов,
// изменившийся размер → Resize.
func (m *Manager) Sync(ws []*Widget) {
	desired := make(map[string]*Widget, len(ws))
	for _, w := range ws {
		desired[w.ID] = w
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Останов исчезнувших.
	for id, p := range m.procs {
		if _, keep := desired[id]; !keep {
			p.cancel()
			delete(m.procs, id)
		}
	}

	// Запуск новых и Resize изменившихся.
	for id, w := range desired {
		p, ok := m.procs[id]
		if !ok {
			p = m.startProcLocked(copyWidget(w))
			m.procs[id] = p
			continue
		}
		changed := w.Rect.W != p.getSpec().Rect.W || w.Rect.H != p.getSpec().Rect.H
		p.setSpec(copyWidget(w))
		if changed {
			go p.resize(int(w.Rect.W), int(w.Rect.H))
		}
	}
}

// copyWidget — мелкая копия виджета для снапшота менеджера: геометрия
// принадлежит main loop, горутины рендера читают только копию.
// Perms/Token иммутабельны после создания — шарить их безопасно.
func copyWidget(w *Widget) *Widget {
	c := *w
	return &c
}

// startProcLocked строит proc и запускает его горутину-надзирателя.
// Вызывается под m.mu.
func (m *Manager) startProcLocked(w *Widget) *proc {
	ctx, cancel := context.WithCancel(context.Background())
	p := &proc{ctx: ctx, cancel: cancel, spec: w, status: StatusStarting}
	go m.supervise(p)
	return p
}

// supervise — надзиратель виджета: запуск → работа → падение/ошибка →
// backoff → перезапуск. Паника рендерера перехватывается и считается
// крахом процесса (тест 4.4). Цикл живёт до отмены proc.
func (m *Manager) supervise(p *proc) {
	attempt := 0
	for {
		if p.ctx.Err() != nil {
			p.setStatus(StatusStopped)
			return
		}
		err := m.runOnce(p)
		if p.ctx.Err() != nil {
			p.setStatus(StatusStopped)
			return
		}
		m.mu.Lock()
		m.restarts++
		m.mu.Unlock()
		p.setStatus(StatusCrashed)
		if err != nil {
			log.Printf("widgets: рендерер %q упал: %v", p.getSpec().ID, err)
		}
		// Экспоненциальная пауза перед перезапуском.
		d := m.backoffBase << uint(min(attempt, 20))
		if d <= 0 || d > m.backoffMax {
			d = m.backoffMax
		}
		select {
		case <-p.ctx.Done():
			p.setStatus(StatusStopped)
			return
		case <-time.After(d):
		}
		attempt++
	}
}

// runOnce — одна жизнь рендерера: фабрика → Load → цикл кадров.
// Паника любого шага превращается в ошибку (изоляция краха CDP-процесса).
func (m *Manager) runOnce(p *proc) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &crashError{what: r}
		}
		p.setRenderer(nil)
	}()

	// Снапшот спецификации на эту жизнь: Sync может заменить её в любой
	// момент (геометрия принадлежит main loop).
	spec := p.getSpec()

	r, err := m.factory(p.ctx, spec)
	if err != nil {
		return err
	}
	p.setRenderer(r)

	m.setStatus(p, StatusStarting)
	if err := r.Load(p.ctx, spec.URL, int(spec.Rect.W), int(spec.Rect.H)); err != nil {
		return err
	}
	m.setStatus(p, StatusRunning)

	// Отложенный resize (пришёл до готовности) — применяем немедленно.
	if sz := p.takePendingResize(); sz != nil {
		if err := r.Resize(sz.w, sz.h); err != nil {
			return err
		}
	}

	// Откладанный ввод (пришёл до готовности) — доставляем теперь.
	for _, ev := range p.takePending() {
		if err := r.Input(ev); err != nil {
			return err
		}
	}

	// Цикл кадров: пока виджет жив — обновляем последний кадр.
	// Во сне кадры не запрашиваются: CPU вне фрустума не тратится (4.7).
	for {
		if p.ctx.Err() != nil {
			return nil
		}
		if p.isSleeping() {
			select {
			case <-p.ctx.Done():
				return nil
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		img, err := r.Frame(p.ctx)
		if err != nil {
			return err
		}
		p.setFrame(img)
	}
}

func (m *Manager) setStatus(p *proc, s Status) { p.setStatus(s) }

// crashError — паника рендерера как ошибка.
type crashError struct{ what any }

func (e *crashError) Error() string {
	return "крах рендерера (паника процесса)"
}

// ── Управление из main loop ─────────────────────────────────────────────

// Input доставляет ввод в виджет в локальных координатах. Если рендерер
// ещё не готов — ввод откладывается (клик пользователя не теряется).
func (m *Manager) Input(widgetID string, ev InputEvent) {
	m.mu.RLock()
	p, ok := m.procs[widgetID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	p.sendInput(ev)
}

// SetSleep переключает спячку виджета (вне фрустума — спит, тест 4.7).
func (m *Manager) SetSleep(widgetID string, sleep bool) {
	m.mu.RLock()
	p, ok := m.procs[widgetID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	p.setSleep(sleep)
}

// Frame — последний кадр виджета (nil, если ещё не готов).
func (m *Manager) Frame(widgetID string) image.Image {
	m.mu.RLock()
	p, ok := m.procs[widgetID]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.frame
}

// StatusOf — статус жизненного цикла виджета.
func (m *Manager) StatusOf(widgetID string) Status {
	m.mu.RLock()
	p, ok := m.procs[widgetID]
	m.mu.RUnlock()
	if !ok {
		return StatusStopped
	}
	return p.getStatus()
}

// Stop останавливает все процессы (завершение сессии).
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, p := range m.procs {
		p.cancel()
		delete(m.procs, id)
	}
}

// ── Методы proc (потокобезопасный доступ к полям под p.mu) ──────────────

func (p *proc) setRenderer(r WidgetRenderer) {
	p.mu.Lock()
	p.r = r
	p.mu.Unlock()
}

func (p *proc) getRenderer() WidgetRenderer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.r
}

func (p *proc) getSpec() *Widget {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.spec
}

func (p *proc) setSpec(w *Widget) {
	p.mu.Lock()
	p.spec = w
	p.mu.Unlock()
}

func (p *proc) setStatus(s Status) {
	p.mu.Lock()
	p.status = s
	p.mu.Unlock()
}

func (p *proc) getStatus() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *proc) setFrame(img image.Image) {
	p.mu.Lock()
	p.frame = img
	p.mu.Unlock()
}

func (p *proc) setSleep(sleep bool) {
	p.mu.Lock()
	p.sleeping = sleep
	if sleep {
		p.status = StatusSleeping
	} else {
		p.status = StatusRunning
	}
	p.mu.Unlock()
}

func (p *proc) isSleeping() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sleeping
}

// sendInput доставляет ввод в рендерер или откладывает до готовности.
func (p *proc) sendInput(ev InputEvent) {
	p.mu.Lock()
	if p.r == nil {
		p.pending = append(p.pending, ev)
		p.mu.Unlock()
		return
	}
	r := p.r
	p.mu.Unlock()

	if err := r.Input(ev); err != nil {
		log.Printf("widgets: ввод %q: %v", p.getSpec().ID, err)
	}
}

// takePending забирает отложенный ввод.
func (p *proc) takePending() []InputEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	pend := p.pending
	p.pending = nil
	return pend
}

// resize меняет размер рендерера (горутина Sync; вызов безопасен и если
// рендерера ещё нет — размер применится при готовности).
func (p *proc) resize(w, h int) {
	p.mu.Lock()
	if p.r == nil {
		p.pendingResize = &sizeWH{w, h} // применит runOnce после Load
		p.mu.Unlock()
		return
	}
	r := p.r
	p.mu.Unlock()
	if err := r.Resize(w, h); err != nil {
		log.Printf("widgets: resize %q: %v", p.getSpec().ID, err)
	}
}

// sizeWH — размер вьюпорта виджета.
type sizeWH struct{ w, h int }

// takePendingResize забирает отложенный resize (для runOnce).
func (p *proc) takePendingResize() *sizeWH {
	p.mu.Lock()
	defer p.mu.Unlock()
	sz := p.pendingResize
	p.pendingResize = nil
	return sz
}

// Компиляционная проверка: Manager — модуль.
var _ module.Module = (*Manager)(nil)

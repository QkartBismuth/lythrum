package module

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Status — снапшот состояния модуля (для Control Center и логов).
type Status struct {
	Name     string    `json:"name"`
	Running  bool      `json:"running"`
	Restarts int       `json:"restarts"`
	LastErr  string    `json:"last_error,omitempty"`
	Since    time.Time `json:"since"`
}

type supervised struct {
	m Module

	mu       sync.Mutex
	running  bool
	restarts int
	lastErr  string
	since    time.Time
}

func (sv *supervised) snapshot() Status {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return Status{
		Name:     sv.m.Name(),
		Running:  sv.running,
		Restarts: sv.restarts,
		LastErr:  sv.lastErr,
		Since:    sv.since,
	}
}

func (sv *supervised) markRunning() {
	sv.mu.Lock()
	sv.running = true
	sv.since = time.Now()
	sv.mu.Unlock()
}

func (sv *supervised) markStopped(err error) {
	sv.mu.Lock()
	sv.running = false
	if err != nil {
		sv.lastErr = err.Error()
		sv.restarts++ // счётчик падений — метрика здоровья модуля
	}
	sv.mu.Unlock()
}

// Supervisor — надзиратель модулей (паттерн Supervisor).
//
// Цикл: модуль падает (ошибка или паника) → экспоненциальный backoff →
// рестарт. Возврат nil без отмены контекста считается штатным завершением:
// такой модуль ожидает ручного Restart (кнопка Control Center).
type Supervisor struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu          sync.Mutex
	mods        map[string]*supervised
	order       []string
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

// NewSupervisor создаёт надзирателя на базе родительского контекста.
func NewSupervisor(parent context.Context) *Supervisor {
	ctx, cancel := context.WithCancel(parent)
	return &Supervisor{
		ctx:         ctx,
		cancel:      cancel,
		mods:        make(map[string]*supervised),
		baseBackoff: 100 * time.Millisecond,
		maxBackoff:  5 * time.Second,
	}
}

// SetBackoff настраивает паузы рестартов (для тестов и Control Center).
func (s *Supervisor) SetBackoff(base, max time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baseBackoff, s.maxBackoff = base, max
}

// Go запускает модуль под надзором. Повторный Go с тем же именем игнорируется.
func (s *Supervisor) Go(m Module) {
	s.mu.Lock()
	if _, dup := s.mods[m.Name()]; dup {
		s.mu.Unlock()
		return
	}
	sv := &supervised{m: m}
	s.mods[m.Name()] = sv
	s.order = append(s.order, m.Name())
	s.mu.Unlock()

	s.wg.Add(1)
	go s.supervise(sv)
}

// Restart вручную перезапускает остановившийся модуль (Control Center).
// Для ещё работающего модуля — no-op: его цикл рестарта уже активен.
func (s *Supervisor) Restart(name string) bool {
	s.mu.Lock()
	sv, ok := s.mods[name]
	s.mu.Unlock()
	if !ok || sv.isRunning() {
		return false
	}
	s.wg.Add(1)
	go s.supervise(sv)
	return true
}

// Stop отменяет контекст всех модулей и ждёт их завершения.
func (s *Supervisor) Stop() {
	s.cancel()
	s.wg.Wait()
}

// Statuses — снапшот статусов всех модулей под надзором.
func (s *Supervisor) Statuses() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Status, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, s.mods[n].snapshot())
	}
	return out
}

func (sv *supervised) isRunning() bool {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.running
}

// supervise — жизненный цикл одного модуля: run → падение? → backoff → run.
func (s *Supervisor) supervise(sv *supervised) {
	defer s.wg.Done()
	attempt := 0
	for {
		sv.markRunning()
		err := s.runOnce(sv.m)
		sv.markStopped(err)

		// Остановка композитора — штатный выход независимо от ошибки модуля.
		if s.ctx.Err() != nil {
			return
		}
		if err == nil {
			return // модуль завершился сам: ждём ручного Restart
		}

		backoff := s.backoffFor(attempt)
		attempt++
		select {
		case <-time.After(backoff):
		case <-s.ctx.Done():
			return
		}
	}
}

// runOnce запускает Run с перехватом паники. КЛЮЧЕВАЯ точка изоляции:
// крах любого модуля превращается в обычную ошибку и не доходит до ядра.
func (s *Supervisor) runOnce(m Module) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return m.Run(s.ctx)
}

// backoffFor — экспоненциальная пауза с ограничением сверху: 100мс, 200мс,
// 400мс... не дольше maxBackoff. Штормящие падения не грузят CPU.
func (s *Supervisor) backoffFor(attempt int) time.Duration {
	s.mu.Lock()
	base, max := s.baseBackoff, s.maxBackoff
	s.mu.Unlock()

	d := base << uint(min(attempt, 20))
	if d <= 0 || d > max {
		d = max
	}
	return d
}

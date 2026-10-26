package module

import "context"

// Module — изолированная подсистема Lythrum.
//
// Контракт:
//   - Run блокируется до завершения работы или ошибки; возврат ошибки
//     означает «модуль упал» и ведёт к рестарту Supervisor'ом (backoff).
//   - Паника внутри Run перехватывается Supervisor'ом и трактуется как ошибка:
//     ядро композитора (main loop) остаётся живым.
//   - Общение с ядром — только через шину событий (Publisher) или каналы,
//     никогда — прямыми вызовами состояния холста.
type Module interface {
	Name() string
	Run(ctx context.Context) error
}

// NewFunc адаптирует функцию к интерфейсу Module — для коротких подсистем.
func NewFunc(name string, fn func(ctx context.Context) error) Module {
	return funcModule{name: name, fn: fn}
}

type funcModule struct {
	name string
	fn   func(ctx context.Context) error
}

func (f funcModule) Name() string                      { return f.name }
func (f funcModule) Run(ctx context.Context) error     { return f.fn(ctx) }

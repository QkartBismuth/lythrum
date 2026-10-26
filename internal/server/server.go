// Package server — интеграция с дисплеем: интерфейс DisplayBackend изолирует
// ядро Lythrum от деталей протокола Wayland.
//
// Реализации:
//   - Headless (этот файл): виртуальный дисплей для CI и разработки ядра.
//   - Wayland (wayland.go, build tag `wayland`): реальный сервер на cgo
//     libwayland-server. Чисто-Go серверных биндингов не существует
//     (go-wayland поставляет только client-side), поэтому — cgo.
//
// Обмен с ядром: backend публикует события (surface created/destroyed,
// input, frame requests) в общую шину module.Event — ядро остаётся
// единственным владельцем состояния холста.
package server

import "context"

// OutputInfo — геометрия монитора (вьюпорта Lythrum).
type OutputInfo struct {
	Name  string
	W     int
	H     int
	Scale int
}

// Backend — серверная сторона композитора.
type Backend interface {
	// Run блокируется до завершения контекста. Ошибка = «дисплей упал»:
	// Supervisor перезапустит бэкенд (ядро переживёт).
	Run(ctx context.Context) error
	// Outputs — текущая геометрия мониторов.
	Outputs() []OutputInfo
}

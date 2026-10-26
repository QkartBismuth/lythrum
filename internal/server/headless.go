package server

import "context"

// Headless — виртуальный дисплей фиксированного размера.
//
// Используется, пока Wayland-бэкенд (build tag `wayland`) не подключён:
// ядро крутит полный цикл обработки событий, интеграционные тесты гоняются
// в CI без реального дисплея. Run() живёт до отмены контекста — это и есть
// контракт Backend.
type Headless struct {
	w, h int
}

// NewHeadless создаёт виртуальный дисплей w×h.
func NewHeadless(w, h int) *Headless {
	return &Headless{w: w, h: h}
}

func (h *Headless) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (h *Headless) Outputs() []OutputInfo {
	return []OutputInfo{{
		Name:  "HEADLESS-1",
		W:     h.w,
		H:     h.h,
		Scale: 1,
	}}
}

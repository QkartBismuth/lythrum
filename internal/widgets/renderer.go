package widgets

import (
	"context"
	"image"
	"image/color"
)

// InputEvent — событие ввода для виджета в его ЛОКАЛЬНЫХ координатах
// (преобразование экран→виджет делает HitAt / Manager).
type InputEvent struct {
	Type   string // "mouse_down", "mouse_up", "mouse_move", "key", "wheel"
	X, Y   float64
	Button int     // 1 — левая, 2 — средняя, 3 — правая
	DX, DY float64 // wheel
	Key    string  // keysym ("Escape", "Return", "a"…)
	Text   string  // символ для key (после раскладки)
}

// WidgetRenderer — рендерер одного виджета. Реализации: CDP-браузер
// (продакшн) и фейки (тесты). Контракт: все методы безопасно звать из
// горутины-владельца виджета; ошибки НЕ паникуют (паника — внештат,
// изолируется Manager'ом как крах процесса).
type WidgetRenderer interface {
	// Load открывает URL и задаёт размер вьюпорта. Блокируется до
	// готовности первого кадра или ошибки.
	Load(ctx context.Context, url string, w, h int) error
	// Resize меняет размер вьюпорта виджета.
	Resize(w, h int) error
	// Input доставляет событие ввода (локальные координаты).
	Input(ev InputEvent) error
	// Frame возвращает последний скомпонованный кадр. Блокируется до
	// кадра, ошибки или отмены ctx.
	Frame(ctx context.Context) (image.Image, error)
	// Close завершает рендерер (процесс браузера).
	Close() error
}

// RendererFactory — фабрика рендереров: Manager строит по ней новый
// процесс при каждом (пере)запуске виджета. Подменяется в тестах.
type RendererFactory func(ctx context.Context, w *Widget) (WidgetRenderer, error)

// NullFactory — фабрика без браузера: виджет живёт как заглушка
// (серый кадр-заглушка), IPC и сон работают. Для CI и систем без
// Chromium — композитор не должен страдать от отсутствия браузера.
func NullFactory(ctx context.Context, w *Widget) (WidgetRenderer, error) {
	return &nullRenderer{id: w.ID}, nil
}

// nullRenderer — рендерер-заглушка: кадр серого цвета нужного размера.
type nullRenderer struct {
	id string
	w  int
	h  int
}

func (n *nullRenderer) Load(ctx context.Context, url string, w, h int) error {
	n.w, n.h = w, h
	return nil
}

func (n *nullRenderer) Resize(w, h int) error {
	n.w, n.h = w, h
	return nil
}

func (n *nullRenderer) Input(InputEvent) error { return nil }

func (n *nullRenderer) Frame(ctx context.Context) (image.Image, error) {
	img := image.NewRGBA(image.Rect(0, 0, max(n.w, 1), max(n.h, 1)))
	for y := 0; y < n.h; y++ {
		for x := 0; x < n.w; x++ {
			img.Set(x, y, color.RGBA{R: 60, G: 60, B: 64, A: 255})
		}
	}
	return img, nil
}

func (n *nullRenderer) Close() error { return nil }

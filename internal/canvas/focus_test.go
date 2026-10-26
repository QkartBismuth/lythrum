package canvas

import (
	"testing"

	"lythrum/internal/core"
)

// Спринт 2: кольцо фокуса Meta/Alt+Tab.
func TestCycleFocus(t *testing.T) {
	s := New(1920, 1080)
	a := s.AddWindow("A", core.Rect{X: 0, Y: 0, W: 10, H: 10})
	b := s.AddWindow("B", core.Rect{X: 20, Y: 0, W: 10, H: 10})
	c := s.AddWindow("C", core.Rect{X: 40, Y: 0, W: 10, H: 10})

	// Фокус на A (первое окно). Таб: A→B→C→A.
	for i, want := range []*Window{b, c, a} {
		w, ok := s.CycleFocus(1)
		if !ok || w.ID != want.ID {
			t.Fatalf("шаг %d: фокус = %v, want %q", i, w, want.Title)
		}
	}

	// Обратное направление.
	if w, _ := s.CycleFocus(-1); w.ID != c.ID {
		t.Fatalf("назад: фокус = %v, want C", w)
	}

	// Свернутые окна пропускаются.
	b.Minimized = true
	s.FocusWindow(a.ID)
	if w, _ := s.CycleFocus(1); w.ID != c.ID {
		t.Fatalf("свернутое B не пропущено: фокус = %v", w)
	}
}

// Спринт 2: пустой холст не падает.
func TestCycleFocusEmpty(t *testing.T) {
	s := New(1920, 1080)
	if _, ok := s.CycleFocus(1); ok {
		t.Fatal("на пустом холсте фокуса быть не может")
	}
	if s.Focus != 0 {
		t.Fatalf("Focus = %d, want 0", s.Focus)
	}
}

// Спринт 2: позиция камеры для центрирования окна.
func TestCenterCamPos(t *testing.T) {
	s := New(1920, 1080)
	w := s.AddWindow("W", core.Rect{X: 1000, Y: 500, W: 400, H: 200})
	want := core.Vec2{X: 1000 + 200 - 960, Y: 500 + 100 - 540}
	if got := s.CenterCamPos(w); got != want {
		t.Fatalf("CenterCamPos = %v, want %v", got, want)
	}
}

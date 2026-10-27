package canvas

import (
	"testing"

	"lythrum/internal/core"
)

// RemoveWindow закрывает окно и переводит фокус на живое окно.
func TestRemoveWindowMovesFocus(t *testing.T) {
	s := New(1920, 1080)
	a := s.AddWindow("a", core.Rect{X: 0, Y: 0, W: 100, H: 100})
	b := s.AddWindow("b", core.Rect{X: 200, Y: 0, W: 100, H: 100})
	s.Focus = a.ID

	if !s.RemoveWindow(a.ID) {
		t.Fatal("RemoveWindow вернул false для существующего окна")
	}
	if _, ok := s.Window(a.ID); ok {
		t.Fatal("окно не удалено")
	}
	if s.Focus != b.ID {
		t.Fatalf("фокус: %d, want %d", s.Focus, b.ID)
	}
}

// Повторное закрытие того же окна — no-op, паники нет.
func TestRemoveWindowIdempotent(t *testing.T) {
	s := New(1920, 1080)
	w := s.AddWindow("w", core.Rect{X: 0, Y: 0, W: 100, H: 100})
	if !s.RemoveWindow(w.ID) {
		t.Fatal("первое закрытие должно работать")
	}
	if s.RemoveWindow(w.ID) {
		t.Fatal("второе закрытие должно быть no-op")
	}
	if s.Focus != 0 {
		t.Fatalf("фокус без окон: %d, want 0", s.Focus)
	}
}

// Закрытие фона окна, на котором фокус: фокус остаётся на месте.
func TestRemoveWindowKeepsForeignFocus(t *testing.T) {
	s := New(1920, 1080)
	a := s.AddWindow("a", core.Rect{X: 0, Y: 0, W: 100, H: 100})
	b := s.AddWindow("b", core.Rect{X: 200, Y: 0, W: 100, H: 100})
	s.Focus = a.ID

	s.RemoveWindow(b.ID)
	if s.Focus != a.ID {
		t.Fatalf("фокус чужого окна сбился: %d", s.Focus)
	}
}

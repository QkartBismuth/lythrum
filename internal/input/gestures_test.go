package input

import (
	"math"
	"testing"

	"lythrum/internal/module"
)

// 3.7 — панорама трекпада сглаженная: между соседними событиями нет скачков.
func TestGesturePanSmoothing(t *testing.T) {
	var c catcher
	g := NewGestureProcessor(&c)

	// Серия одинаковых дельт: сглаженные значения обязаны сходиться к ним
	// монотонно, без выбросов.
	for i := 0; i < 20; i++ {
		g.Axis(10, -4)
	}

	var prevX float64
	for i, e := range c.of(module.EvCameraPan) {
		if e.Vec.X < prevX-1e-9 {
			t.Fatalf("событие %d: X пошёл назад (%v < %v)", i, e.Vec.X, prevX)
		}
		if math.Abs(e.Vec.X) > 10*(1+1e-9) {
			t.Fatalf("событие %d: выброс X = %v > дельты", i, e.Vec.X)
		}
		prevX = e.Vec.X
	}
	// Сходится к установившемуся значению.
	if math.Abs(prevX-10) > 0.5 {
		t.Fatalf("сглаженная панорама не сошлась к дельте: %v", prevX)
	}
}

// Пинч: зум-события в точке под пальцами, множитель в разумных пределах.
func TestGesturePinch(t *testing.T) {
	var c catcher
	g := NewGestureProcessor(&c)
	at := module.Event{}.Vec // нулевая точка экрана

	for i := 0; i < 10; i++ {
		g.Pinch(0.05, at)
	}
	events := c.of(module.EvCameraZoomAt)
	if len(events) != 10 {
		t.Fatalf("want 10 zoom-событий, got %d", len(events))
	}
	for i, e := range events {
		if e.Num <= 0 {
			t.Fatalf("событие %d: множитель %v неположительный", i, e.Num)
		}
		if e.Num > 3 {
			t.Fatalf("событие %d: множитель %v вылетел", i, e.Num)
		}
	}
}

// Колесо мыши: без сглаживания.
func TestGestureWheel(t *testing.T) {
	var c catcher
	g := NewGestureProcessor(&c)

	g.Wheel(0, -120)
	got := c.of(module.EvCameraPan)
	if len(got) != 1 || got[0].Vec.Y != -120 {
		t.Fatalf("wheel: %+v", got)
	}
}

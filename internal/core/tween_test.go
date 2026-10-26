package core

import "testing"

// Спринт 2: плавное центрирование камеры доходит до цели монотонно и останавливается.
func TestTweenReachesTarget(t *testing.T) {
	tw := NewTween(Vec2{X: 0, Y: 0}, Vec2{X: 100, Y: 50}, 0.35)

	var last Vec2
	for i := 0; i < 100 && !tw.Done(); i++ {
		last = tw.Tick(0.016)
	}
	if !tw.Done() {
		t.Fatal("твин не завершился за отведённое время")
	}
	vecEq(t, "final pos", last, Vec2{X: 100, Y: 50})
}

func TestTweenMonotonic(t *testing.T) {
	tw := NewTween(Vec2{X: 0, Y: 0}, Vec2{X: 100, Y: 0}, 1)
	prev := 0.0
	for i := 0; i < 60; i++ {
		if tw.Done() {
			break
		}
		p := tw.Tick(1.0 / 60)
		if p.X < prev-1e-9 {
			t.Fatalf("анимация пошла назад: %v < %v", p.X, prev)
		}
		prev = p.X
	}
}

func TestTweenInstant(t *testing.T) {
	tw := NewTween(Vec2{X: 1, Y: 2}, Vec2{X: 9, Y: 9}, 0)
	p := tw.Tick(0)
	vecEq(t, "instant tween", p, Vec2{X: 9, Y: 9})
}

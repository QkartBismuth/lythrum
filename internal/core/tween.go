package core

import "math"

// Tween — плавная анимация точки (камеры) из From в To за D секунд.
//
// Используется для «переключения фокуса с плавным центрированием камеры»
// (Meta/Alt+Tab). Ease in-out cubic — разгон и торможение без рывков.
// Чистая математика: тикается из main loop, состояние — только T.
type Tween struct {
	From, To Vec2
	T        float64 // прошедшее время, секунды
	D        float64 // длительность, секунды
}

// NewTween создаёт анимацию длительностью d секунд (d <= 0 → мгновенно:
// твин считается завершённым, Tick сразу возвращает To).
func NewTween(from, to Vec2, d float64) *Tween {
	if d <= 0 {
		return &Tween{From: from, To: to, T: 1, D: 1}
	}
	return &Tween{From: from, To: to, D: d}
}

// Done сообщает о завершении анимации.
func (t *Tween) Done() bool { return t.T >= t.D }

// Tick сдвигает анимацию на dt секунд и возвращает текущую позицию.
// После завершения всегда возвращает To.
func (t *Tween) Tick(dt float64) Vec2 {
	t.T += dt
	p := t.T / t.D
	if p >= 1 {
		return t.To
	}
	return t.From.Add(t.To.Sub(t.From).Mul(easeInOutCubic(p)))
}

// easeInOutCubic — кубическое замедление на обоих концах [0..1].
func easeInOutCubic(p float64) float64 {
	if p < 0.5 {
		return 4 * p * p * p
	}
	return 1 - math.Pow(-2*p+2, 3)/2
}

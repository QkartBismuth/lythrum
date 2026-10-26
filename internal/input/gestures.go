package input

import (
	"lythrum/internal/core"
	"lythrum/internal/module"
)

// GestureProcessor — жесты трекпада: панорамирование и пинч → события
// камеры со сглаживанием (EMA).
//
// Зачем сглаживание: сырье от libinput приходит дискретными дельтами,
// без усреднения камера «дёргается» (тест 3.7: «инерция без рывков»).
// Сглаживание экспоненциальное — дёшево и без накопления истории.
type GestureProcessor struct {
	// AlphaPan — коэффициент сглаживания панорамирования [0..1]:
	// 1 = без сглаживания, 0.5 = половина новой дельты.
	AlphaPan float64
	// ZoomSensitivity — множитель пинча за единицу жеста.
	ZoomSensitivity float64

	smoothPan core.Vec2
	smoothPin float64
	pub       Publisher
}

// NewGestureProcessor — настройки по умолчанию.
func NewGestureProcessor(pub Publisher) *GestureProcessor {
	return &GestureProcessor{
		AlphaPan:        0.6,
		ZoomSensitivity: 0.005,
		smoothPin:       1,
		pub:             pub,
	}
}

// Axis — дельта панорамирования в экранных пикселях (событие libinput
// pointer axis). Публикует EvCameraPan.
func (g *GestureProcessor) Axis(dx, dy float64) {
	if g.AlphaPan <= 0 || g.AlphaPan > 1 {
		g.AlphaPan = 1
	}
	// EMA: смещаемся к свежей дельте, старая затухает.
	g.smoothPan = g.smoothPan.Mul(1 - g.AlphaPan).Add(core.Vec2{X: dx, Y: dy}.Mul(g.AlphaPan))
	g.pub.Publish(module.Event{Type: module.EvCameraPan, Vec: g.smoothPan})
}

// Pinch — изменение масштаба жестом (pinch, factor ~ 1 + delta).
// at — точка экрана под пальцами: зум «в курсор». Публикует EvCameraZoomAt.
func (g *GestureProcessor) Pinch(scaleDelta float64, at core.Vec2) {
	g.smoothPin = g.smoothPin*0.7 + (1+scaleDelta)*0.3
	factor := 1 + (g.smoothPin-1)*g.ZoomSensitivity*100
	g.pub.Publish(module.Event{Type: module.EvCameraZoomAt, Vec: at, Num: factor})
}

// Wheel — колесо мыши (дискретные тики): панорамирование без сглаживания
// (колесо само дискретно).
func (g *GestureProcessor) Wheel(dx, dy float64) {
	g.pub.Publish(module.Event{Type: module.EvCameraPan, Vec: core.Vec2{X: dx, Y: dy}})
}

// Package render — слой отрисовки: frustum culling и разделение слоёв.
//
// Архитектурное правило Lythrum: рендерится и обновляется (frame-events)
// только то, что реально видно. Пакет предоставляет чистые функции
// видимости; владелец состояния (main loop) применяет их к холсту.
package render

import "lythrum/internal/core"

// VisibleRects возвращает индексы прямоугольников, пересекающих фрустум
// камеры. O(n) по количеству окон — вызывается каждый тик, поэтому без
// аллокаций сверх результирующего среза.
func VisibleRects(cam core.Camera, vpW, vpH float64, rects []core.Rect) []int {
	fr := cam.Frustum(vpW, vpH)
	out := make([]int, 0, len(rects))
	for i, r := range rects {
		if r.Intersects(fr) {
			out = append(out, i)
		}
	}
	return out
}

// ShouldSleep сообщает, нужно ли усыпить поверхность: окно вне фрустума
// перестаёт получать frame-events (экономия CPU приложений-клиентов).
func ShouldSleep(cam core.Camera, vpW, vpH float64, r core.Rect) bool {
	return !r.Intersects(cam.Frustum(vpW, vpH))
}

// Layer — фиксированные слои интерфейса (Layer Shell).
type Layer uint8

const (
	// LayerCanvas — динамический слой: окна, виджеты, карточки, обои.
	// Подвержен зуму и панорамированию камеры.
	LayerCanvas Layer = iota

	// LayerOverlay — фиксированный UI: контекстные меню, поиск, оверлей
	// питания. Рисуется ПОСЛЕ холста всегда в масштабе 1:1 к экрану —
	// текст меню и оверлеев не зумится вместе с холстом.
	LayerOverlay
)

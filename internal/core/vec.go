// Package core — математическое ядро Lythrum: векторы, прямоугольники,
// камера, магнитное выравнивание и геометрия тайлинга.
//
// Все функции чистые (без побочных эффектов) и покрываются юнит-тестами:
// это фундамент Спринта 1, не зависящий ни от Wayland, ни от GPU.
package core

// Vec2 — точка или вектор в 2D-пространстве.
type Vec2 struct{ X, Y float64 }

func (v Vec2) Add(o Vec2) Vec2    { return Vec2{v.X + o.X, v.Y + o.Y} }
func (v Vec2) Sub(o Vec2) Vec2    { return Vec2{v.X - o.X, v.Y - o.Y} }
func (v Vec2) Mul(k float64) Vec2 { return Vec2{v.X * k, v.Y * k} }

// Rect — прямоугольник в глобальных координатах холста.
type Rect struct{ X, Y, W, H float64 }

func (r Rect) Pos() Vec2       { return Vec2{r.X, r.Y} }
func (r Rect) Right() float64  { return r.X + r.W }
func (r Rect) Bottom() float64 { return r.Y + r.H }

// Contains проверяет, лежит ли точка внутри прямоугольника (включая границы).
func (r Rect) Contains(p Vec2) bool {
	return p.X >= r.X && p.X <= r.Right() && p.Y >= r.Y && p.Y <= r.Bottom()
}

// Intersects — проверка пересечения. Касание границ пересечением не считается:
// окно, стоящее вплотную, не «видно» наполовину.
func (r Rect) Intersects(o Rect) bool {
	return r.X < o.Right() && o.X < r.Right() && r.Y < o.Bottom() && o.Y < r.Bottom()
}

// Moved возвращает копию, смещённую на d (приёмник не мутирует — см. SnapRect).
func (r Rect) Moved(d Vec2) Rect { return Rect{r.X + d.X, r.Y + d.Y, r.W, r.H} }

// Guide — магнитная направляющая: вертикальная или горизонтальная линия
// на фиксированной координате холста. Рисуется на слое холста с учётом зума
// цветом palette.SnapGuideLine.
type Guide struct {
	Vertical bool
	At       float64
}

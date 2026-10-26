package core

// Границы зума: ниже 0.1 холст нечитаем, выше 8 — артефакты растеризации
// и потеря точности координат.
const (
	MinZoom = 0.1
	MaxZoom = 8.0
)

// Camera — видовой экран (камера) над бесконечным холстом.
//
// (X, Y) — координаты ЛЕВОГО ВЕРХНЕГО угла вьюпорта в глобальных координатах
// холста; Zoom — отношение экранного пикселя к холст-пикселю.
// Такая модель делает тривиальными и фрустум, и обратное преобразование.
type Camera struct {
	X, Y float64
	Zoom float64
}

// NewCamera — дефолтная камера (действие одиночного Meta): холст (0,0), зум 1:1.
func NewCamera() Camera { return Camera{X: 0, Y: 0, Zoom: 1} }

// ToScreen переводит точку холста в точку экрана: screen = (canvas − cam)·zoom.
func (c Camera) ToScreen(p Vec2) Vec2 {
	return Vec2{(p.X - c.X) * c.Zoom, (p.Y - c.Y) * c.Zoom}
}

// ToCanvas — обратное преобразование: canvas = screen/zoom + cam.
// Вызывается при пробросе ввода в окна/виджеты (Спринт 4) — клик по экрану
// обязан попадать в ту же точку холста на любом зуме.
func (c Camera) ToCanvas(s Vec2) Vec2 {
	return Vec2{s.X/c.Zoom + c.X, s.Y/c.Zoom + c.Y}
}

// Frustum — видимая область холста. Используется для frustum culling
// (render.VisibleRects) и для Meta+Стрелок: тайлинг считается в долях
// видимого экрана, а не холста.
func (c Camera) Frustum(vpW, vpH float64) Rect {
	return Rect{X: c.X, Y: c.Y, W: vpW / c.Zoom, H: vpH / c.Zoom}
}

// Pan смещает камеру на dScreen пикселей ЭКРАНА (жест трекпада, мышь).
// Экранная дельта переводится в холст-единицы делением на зум, чтобы
// скорость панорамирования совпадала с движением пальца на любом масштабе.
func (c *Camera) Pan(dScreen Vec2) {
	d := dScreen.Mul(1 / c.Zoom)
	c.X -= d.X
	c.Y -= d.Y
}

// ZoomAt приближает/отдаляет с ФИКСАЦИЕЙ мировой точки под курсором
// (пинч трекпада, колесо). factor > 1 — приблизить. Зум ограничен
// [MinZoom, MaxZoom], точка под курсором остаётся на месте.
func (c *Camera) ZoomAt(screenPoint Vec2, factor float64) {
	before := c.ToCanvas(screenPoint)

	z := c.Zoom * factor
	if z < MinZoom {
		z = MinZoom
	}
	if z > MaxZoom {
		z = MaxZoom
	}
	c.Zoom = z

	// После смены зума точка под курсором «уехала» — двигаем камеру так,
	// чтобы мировая точка вернулась на исходное экранное место.
	after := c.ToCanvas(screenPoint)
	c.X += before.X - after.X
	c.Y += before.Y - after.Y
}

// IsDefault сообщает, находится ли камера в дефолтном состоянии.
func (c Camera) IsDefault() bool {
	return c.X == 0 && c.Y == 0 && c.Zoom == 1
}

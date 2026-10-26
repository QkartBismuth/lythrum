package core

// Dir — направление тайлинга (Meta+Стрелки).
type Dir uint8

const (
	DirLeft Dir = iota
	DirRight
	DirUp
	DirDown
)

// TileDirection раскладывает окно в долю ВИДИМОГО экрана (фрустума камеры):
//
//	Left  → левая половина;   Right → правая половина;
//	Up    → maximize (весь фрустум);
//	Down  → нижняя половина (Windows 11: восстановить/свернуть в ленту —
//	        решение о minimize принимает вызывающая сторона, геометрия та же).
//
// Возвращает целевой Rect в координатах холста. Чистая функция.
func TileDirection(dir Dir, vp Rect) Rect {
	switch dir {
	case DirLeft:
		return Rect{X: vp.X, Y: vp.Y, W: vp.W / 2, H: vp.H}
	case DirRight:
		return Rect{X: vp.X + vp.W/2, Y: vp.Y, W: vp.W / 2, H: vp.H}
	case DirUp:
		return vp
	case DirDown:
		return Rect{X: vp.X, Y: vp.Y + vp.H/2, W: vp.W, H: vp.H / 2}
	default:
		return vp
	}
}

package core

import "math"

// DefaultSnapThresholdPx — магнитный порог в ЭКРАННЫХ пикселях: если кромки
// ближе этого значения, окно примагничивается встык. Порог задаётся в экранных
// пикселях и переводится в холст-единицы делением на zoom — ощущение
// «магнита» одинаково на любом масштабе.
const DefaultSnapThresholdPx = 8.0

// SnapResult — результат примагничивания: подправленная позиция и
// направляющие для отрисовки магнитных линий.
type SnapResult struct {
	Pos    Vec2
	Guides []Guide
}

// SnapRect примагничивает движущийся прямоугольник к соседям встык.
//
// Алгоритм: для каждой пары кромок по каждой оси (лево/право движущегося
// к лево/право соседа) вычисляется дельта совмещения; из кандидатов в пределах
// порога выбирается минимальная по модулю дельта. Ось независимы: по X может
// примагнититься к одному соседу, по Y — к другому. Чистая функция —
// не мутирует ни moving, ни neighbors.
func SnapRect(moving Rect, neighbors []Rect, thresholdPx, zoom float64) SnapResult {
	if zoom <= 0 {
		zoom = 1
	}
	th := thresholdPx / zoom // экранная чувствительность → холст-единицы

	bestDX, guideX, foundX := 0.0, 0.0, false
	bestDY, guideY, foundY := 0.0, 0.0, false

	for _, n := range neighbors {
		// Кандидаты по X: [дельта совмещения, координата направляющей].
		for _, c := range [][2]float64{
			{n.X - moving.X, n.X},             // моя левая кромка → левая соседа
			{n.X - moving.Right(), n.X},       // моя правая → левая соседа (стык слева)
			{n.Right() - moving.X, n.Right()}, // моя левая → правая соседа (стык справа)
			{n.Right() - moving.Right(), n.Right()},
		} {
			d := c[0]
			// эпсилон отсекает флорат при равных дельтах разных соседей.
			if math.Abs(d) <= th && (!foundX || math.Abs(d) < math.Abs(bestDX)-1e-9) {
				bestDX, guideX, foundX = d, c[1], true
			}
		}
		// Кандидаты по Y — симметрично.
		for _, c := range [][2]float64{
			{n.Y - moving.Y, n.Y},
			{n.Y - moving.Bottom(), n.Y},
			{n.Bottom() - moving.Y, n.Bottom()},
			{n.Bottom() - moving.Bottom(), n.Bottom()},
		} {
			d := c[0]
			if math.Abs(d) <= th && (!foundY || math.Abs(d) < math.Abs(bestDY)-1e-9) {
				bestDY, guideY, foundY = d, c[1], true
			}
		}
	}

	res := SnapResult{Pos: moving.Pos()}
	if foundX {
		res.Pos.X += bestDX
		res.Guides = append(res.Guides, Guide{Vertical: true, At: guideX})
	}
	if foundY {
		res.Pos.Y += bestDY
		res.Guides = append(res.Guides, Guide{Vertical: false, At: guideY})
	}
	return res
}

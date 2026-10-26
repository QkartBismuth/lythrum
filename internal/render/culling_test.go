package render

import (
	"reflect"
	"testing"

	"lythrum/internal/core"
)

// 5.1 — фрустум-куллинг возвращает только видимые индексы.
func TestVisibleRects(t *testing.T) {
	cam := core.NewCamera()
	rects := []core.Rect{
		{X: 100, Y: 100, W: 200, H: 100},   // внутри
		{X: 5000, Y: 5000, W: 100, H: 100}, // далеко за фрустумом
		{X: 1800, Y: 0, W: 400, H: 50},     // частично виден
		{X: -50, Y: -50, W: 20, H: 20},     // за левым верхом
	}
	want := []int{0, 2}
	if got := VisibleRects(cam, 1920, 1080, rects); !reflect.DeepEqual(got, want) {
		t.Fatalf("VisibleRects = %v, want %v", got, want)
	}
}

// 5.2 — окна вне вьюпорта засыпают, внутри — бодрствуют.
func TestShouldSleep(t *testing.T) {
	cam := core.NewCamera()
	awake := core.Rect{X: 0, Y: 0, W: 100, H: 100}
	far := core.Rect{X: 9999, Y: 9999, W: 100, H: 100}

	if ShouldSleep(cam, 1920, 1080, awake) {
		t.Fatal("видимое окно не должно спать")
	}
	if !ShouldSleep(cam, 1920, 1080, far) {
		t.Fatal("невидимое окно должно спать")
	}
}

// 5.1 — 10k окон: возвращаются ровно видимые индексы, обход O(n).
func TestVisibleRects10k(t *testing.T) {
	cam := core.Camera{X: 0, Y: 0, Zoom: 1}
	const n = 10000
	rects := make([]core.Rect, n)

	// Сетка 100×100 окон 100×100 с шагом 120: видима полоса под фрустумом.
	for i, r := range rects {
		r.X = float64(i%100) * 120
		r.Y = float64(i/100) * 120
		r.W, r.H = 100, 100
		rects[i] = r
	}

	// Фрустум 1920×1080 покрывает 17×10 узлов сетки (последний столбец/ряд
	// касаются границ — считаем честным пересечением).
	got := VisibleRects(cam, 1920, 1080, rects)

	// Проверка полноты: каждый видимый вручную индекс есть в ответе.
	seen := make(map[int]bool, len(got))
	for _, i := range got {
		if seen[i] {
			t.Fatalf("индекс %d задвоился", i)
		}
		seen[i] = true
		if !rects[i].Intersects(cam.Frustum(1920, 1080)) {
			t.Fatalf("индекс %d не пересекает фрустум, но возвращён", i)
		}
	}
	manual := 0
	for i, r := range rects {
		if r.Intersects(cam.Frustum(1920, 1080)) {
			manual++
			if !seen[i] {
				t.Fatalf("видимый индекс %d потерян", i)
			}
		}
	}
	if len(got) != manual {
		t.Fatalf("видимых %d, want %d", len(got), manual)
	}

	// Пустая сетка координат — окна в точке (0,0) размером 0: ничего не видно.
	empty := make([]core.Rect, n)
	if got := VisibleRects(cam, 1920, 1080, empty); len(got) != 0 {
		t.Fatalf("вырожденные окна не должны быть видимы: %d", len(got))
	}
}

// Бенчмарк O(n): растёт линейно с числом окон.
func BenchmarkVisibleRects10k(b *testing.B) {
	cam := core.Camera{X: 0, Y: 0, Zoom: 1}
	rects := make([]core.Rect, 10000)
	for i := range rects {
		rects[i] = core.Rect{X: float64(i%100) * 120, Y: float64(i/100) * 120, W: 100, H: 100}
	}
	b.ResetTimer()
	for range b.N {
		VisibleRects(cam, 1920, 1080, rects)
	}
}

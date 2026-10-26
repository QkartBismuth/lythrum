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

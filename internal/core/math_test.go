package core

import (
	"math"
	"testing"
)

func almostEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func vecEq(t *testing.T, name string, got, want Vec2) {
	t.Helper()
	if !almostEq(got.X, want.X) || !almostEq(got.Y, want.Y) {
		t.Fatalf("%s: got %v, want %v", name, got, want)
	}
}

// 1.1 — преобразования Холст→Экран→Холст обязаны быть взаимно обратными.
func TestToScreenToCanvasRoundTrip(t *testing.T) {
	cam := Camera{X: 100, Y: 50, Zoom: 1.25}
	p := Vec2{X: 300, Y: 200}
	back := cam.ToCanvas(cam.ToScreen(p))
	vecEq(t, "round trip", back, p)
}

// 1.2 — зум «в курсор»: мировая точка под screenPoint не смещается.
func TestZoomAtKeepsPointUnderCursor(t *testing.T) {
	cam := NewCamera()
	sp := Vec2{X: 960, Y: 540}
	before := cam.ToCanvas(sp)

	cam.ZoomAt(sp, 2)   // приблизить
	cam.ZoomAt(sp, 0.4) // и отдалить (с клампом)

	vecEq(t, "point under cursor after zoom in/out", cam.ToCanvas(sp), before)
}

func TestZoomAtClamps(t *testing.T) {
	cam := NewCamera()
	cam.ZoomAt(Vec2{0, 0}, 1e6)
	if cam.Zoom != MaxZoom {
		t.Fatalf("zoom не ограничен сверху: %v", cam.Zoom)
	}
	cam.ZoomAt(Vec2{0, 0}, 1e-6)
	if cam.Zoom != MinZoom {
		t.Fatalf("zoom не ограничен снизу: %v", cam.Zoom)
	}
}

// 1.3 — панорама задаётся в ЭКРАННЫХ пикселях и масштабируется зумом:
// при zoom 2 смещение пальца на 100px двигает холст на 50 холст-пикселей.
func TestPanAccountsZoom(t *testing.T) {
	cam := NewCamera()
	cam.Pan(Vec2{X: 100, Y: 0})
	if cam.X != -100 {
		t.Fatalf("pan при zoom 1: X = %v, want -100", cam.X)
	}

	cam = NewCamera()
	cam.Zoom = 2
	cam.Pan(Vec2{X: 100, Y: 0})
	if cam.X != -50 {
		t.Fatalf("pan при zoom 2: X = %v, want -50", cam.X)
	}
}

// 1.4 — фрустум: размер видимой области = viewport / zoom.
func TestFrustum(t *testing.T) {
	cases := []struct {
		cam      Camera
		vpW, vpH float64
		want     Rect
	}{
		{NewCamera(), 1920, 1080, Rect{0, 0, 1920, 1080}},
		{Camera{X: 500, Y: 250, Zoom: 2}, 1920, 1080, Rect{500, 250, 960, 540}},
		{Camera{X: -100, Y: -50, Zoom: 0.5}, 1000, 500, Rect{-100, -50, 2000, 1000}},
	}
	for i, c := range cases {
		got := c.cam.Frustum(c.vpW, c.vpH)
		if got != c.want {
			t.Fatalf("case %d: frustum = %+v, want %+v", i, got, c.want)
		}
	}
}

// 1.5 — примагничивание встык: правая кромка движущегося окна липнет к
// левой кромке соседа, направляющая вертикальная на кромке соседа.
func TestSnapRectDocksFlush(t *testing.T) {
	neighbor := Rect{X: 100, Y: 0, W: 200, H: 200}
	moving := Rect{X: 57, Y: 50, W: 50, H: 50} // правая кромка 107, дельта -7

	res := SnapRect(moving, []Rect{neighbor}, DefaultSnapThresholdPx, 1)

	vecEq(t, "snapped pos", res.Pos, Vec2{X: 50, Y: 50})
	if res.Pos.X+50 != neighbor.X {
		t.Fatalf("кромки не встык: right=%v, neighbor.X=%v", res.Pos.X+50, neighbor.X)
	}
	if len(res.Guides) != 1 || !res.Guides[0].Vertical || res.Guides[0].At != neighbor.X {
		t.Fatalf("направляющие: %+v", res.Guides)
	}
}

// 1.6 — за порогом магнит не срабатывает: позиция и линии не тронуты.
func TestSnapRectIgnoresBeyondThreshold(t *testing.T) {
	neighbor := Rect{X: 100, Y: 0, W: 200, H: 200}
	moving := Rect{X: 20, Y: 300, W: 50, H: 50}

	res := SnapRect(moving, []Rect{neighbor}, DefaultSnapThresholdPx, 1)

	vecEq(t, "pos untouched", res.Pos, moving.Pos())
	if len(res.Guides) != 0 {
		t.Fatalf("лишние направляющие: %+v", res.Guides)
	}
}

// 1.6b — порог в экранных пикселях: при zoom 2 экранная чувствительность 8px
// = 4 холст-пикселя, поэтому дельта 7 уже не липнет.
func TestSnapRectThresholdInScreenPx(t *testing.T) {
	neighbor := Rect{X: 100, Y: 0, W: 200, H: 200}
	moving := Rect{X: 57, Y: 50, W: 50, H: 50} // дельта -7

	res := SnapRect(moving, []Rect{neighbor}, DefaultSnapThresholdPx, 2)

	vecEq(t, "pos untouched at zoom 2", res.Pos, moving.Pos())
}

// 1.7 — геометрия Meta+Стрелок в долях видимого экрана.
func TestTileDirection(t *testing.T) {
	vp := Rect{X: 40, Y: 40, W: 1920, H: 1080}
	cases := []struct {
		dir  Dir
		want Rect
	}{
		{DirLeft, Rect{40, 40, 960, 1080}},
		{DirRight, Rect{1000, 40, 960, 1080}},
		{DirUp, vp},
		{DirDown, Rect{40, 580, 1920, 540}},
	}
	for _, c := range cases {
		if got := TileDirection(c.dir, vp); got != c.want {
			t.Fatalf("dir %d: got %+v, want %+v", c.dir, got, c.want)
		}
	}
}

// 1.8 — касание границ не считается пересечением.
func TestRectIntersects(t *testing.T) {
	a := Rect{X: 0, Y: 0, W: 10, H: 10}
	if !a.Intersects(Rect{X: 5, Y: 5, W: 10, H: 10}) {
		t.Fatal("перекрытие не распознано")
	}
	if a.Intersects(Rect{X: 10, Y: 0, W: 10, H: 10}) {
		t.Fatal("касание границ принято за пересечение")
	}
	if a.Intersects(Rect{X: 100, Y: 100, W: 1, H: 1}) {
		t.Fatal("далёкий прямоугольник принят за пересечение")
	}
}

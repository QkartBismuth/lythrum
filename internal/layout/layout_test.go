package layout

import "testing"

func TestRectSplitHorizontalFillsExactly(t *testing.T) {
	r := Rect{X: 10, Y: 20, W: 300, H: 100}
	a, b := r.Split(Horizontal, 100, 10)
	if a.W != 100 {
		t.Errorf("first child width = %d, want 100", a.W)
	}
	if b.X != 120 {
		t.Errorf("second child x = %d, want 120", b.X)
	}
	if a.Right()+10 != b.X {
		t.Errorf("gap not honoured: %d + 10 != %d", a.Right(), b.X)
	}
	if a.W+b.W+10 != r.W {
		t.Errorf("children do not fill the parent: %d + %d + 10 != %d", a.W, b.W, r.W)
	}
}

func TestRectSplitVertical(t *testing.T) {
	r := Rect{W: 200, H: 300}
	a, b := r.Split(Vertical, 120, 5)
	if a.H != 120 || b.Y != 125 || b.H != 175 {
		t.Errorf("unexpected vertical split: %+v %+v", a, b)
	}
}

func TestRectSplitRatioRounding(t *testing.T) {
	r := Rect{W: 101, H: 50}
	a, b := r.SplitRatio(Horizontal, 0.3333, 0)
	if a.W+b.W != r.W {
		t.Errorf("ratio split lost pixels: %d + %d != %d", a.W, b.W, r.W)
	}
	if a.W < 32 || a.W > 35 {
		t.Errorf("ratio split produced %d, want about a third of 101", a.W)
	}
}

func TestRectSplitClamps(t *testing.T) {
	r := Rect{W: 100, H: 100}
	a, b := r.Split(Horizontal, 500, 0)
	if a.W != 100 || b.W != 0 || !b.Empty() {
		t.Errorf("oversized split not clamped: %+v %+v", a, b)
	}
}

func TestDivideCoversParentExactly(t *testing.T) {
	for _, n := range []int{1, 2, 3, 7, 16} {
		for _, gap := range []int{0, 1, 5} {
			parent := Rect{X: 3, Y: 4, W: 137, H: 91}
			parts := parent.Divide(Vertical, n, gap)
			if len(parts) != n {
				t.Fatalf("Divide(%d) returned %d parts", n, len(parts))
			}
			total := 0
			for _, p := range parts {
				total += p.H
			}
			if total+gap*(n-1) != parent.H {
				t.Errorf("n=%d gap=%d: parts cover %d of %d", n, gap, total, parent.H)
			}
			if parts[0].Y != parent.Y || parts[n-1].Bottom() != parent.Bottom() {
				t.Errorf("n=%d gap=%d: parts do not span the parent", n, gap)
			}
			for i := 1; i < n; i++ {
				if parts[i].Y != parts[i-1].Bottom()+gap {
					t.Errorf("n=%d gap=%d: gap wrong at %d", n, gap, i)
				}
			}
		}
	}
}

func TestDivideHorizontal(t *testing.T) {
	parts := Rect{W: 300, H: 100}.Divide(Horizontal, 3, 4)
	if len(parts) != 3 {
		t.Fatalf("got %d parts", len(parts))
	}
	total := 4 * 2
	for _, p := range parts {
		if p.H != 100 {
			t.Errorf("part lost height: %+v", p)
		}
		total += p.W
	}
	if total != 300 {
		t.Errorf("parts cover %d of 300", total)
	}
}

func TestInsetAndAsymmetricInset(t *testing.T) {
	r := Rect{X: 10, Y: 10, W: 100, H: 100}
	if got := r.Inset(5); got != (Rect{X: 15, Y: 15, W: 90, H: 90}) {
		t.Errorf("Inset = %+v", got)
	}
	grown := r.AsymmetricInset(-2, -2, 4, 0)
	if grown != (Rect{X: 8, Y: 14, W: 104, H: 96}) {
		t.Errorf("AsymmetricInset = %+v", grown)
	}
}

func TestContainsAndCenter(t *testing.T) {
	r := Rect{X: 10, Y: 10, W: 20, H: 20}
	if !r.Contains(10, 10) || !r.Contains(29, 29) {
		t.Error("corners should be inside")
	}
	if r.Contains(30, 30) || r.Contains(9, 15) {
		t.Error("points outside should be rejected")
	}
	c := r.Center(1000, 1000)
	if c != r {
		t.Errorf("Center larger than parent = %+v, want %+v", c, r)
	}
	c = r.Center(10, 10)
	if c != (Rect{X: 15, Y: 15, W: 10, H: 10}) {
		t.Errorf("Center = %+v", c)
	}
}

func TestUnionIgnoresEmpty(t *testing.T) {
	a := Rect{X: 0, Y: 0, W: 10, H: 10}
	if got := Union(a, Rect{}); got != a {
		t.Errorf("Union with empty = %+v", got)
	}
	if got := Union(Rect{}, a); got != a {
		t.Errorf("Union with empty first = %+v", got)
	}
	b := Rect{X: 20, Y: 20, W: 10, H: 10}
	if got := Union(a, b); got != (Rect{X: 0, Y: 0, W: 30, H: 30}) {
		t.Errorf("Union = %+v", got)
	}
}

func TestTiledCoversWorkArea(t *testing.T) {
	work := Rect{W: 1920, H: 1080}
	o := Options{Gap: 8, Border: 2, TitleBar: 20}
	rects := Tiled(work, 3, o)
	if len(rects) != 3 {
		t.Fatalf("got %d rects", len(rects))
	}
	for _, r := range rects {
		if r.Empty() {
			t.Fatalf("empty rect %+v", r)
		}
		if r.X < work.X || r.Y < work.Y || r.Right() > work.Right() || r.Bottom() > work.Bottom() {
			t.Errorf("rect %+v escapes the work area %+v", r, work)
		}
	}
	if rects[0].H != work.H-(2*o.Border+o.TitleBar) {
		t.Errorf("window should span the cell minus borders and title bar, got %d", rects[0].H)
	}
	if rects[0].X != work.X+o.Border || rects[0].Y != work.Y+o.Border+o.TitleBar {
		t.Errorf("window is not inset from the cell: %+v", rects[0])
	}
}

func TestTiledTallAreaSplitsVertically(t *testing.T) {
	rects := Tiled(Rect{W: 100, H: 1000}, 2, DefaultOptions())
	if rects[0].W != 100 {
		t.Errorf("tall area should keep full width: %+v", rects[0])
	}
	if rects[0].Bottom() != rects[1].Y {
		t.Errorf("windows are not adjacent: %+v %+v", rects[0], rects[1])
	}
}

func TestStackAndMonocle(t *testing.T) {
	work := Rect{W: 800, H: 600}
	if got := Stack(work); len(got) != 1 || got[0] != work {
		t.Errorf("Stack = %+v", got)
	}
	if got := Monocle(work); len(got) != 1 || got[0] != work {
		t.Errorf("Monocle = %+v", got)
	}
	if Tiled(work, 0, DefaultOptions()) != nil {
		t.Error("Tiled with no windows should return nothing")
	}
}

func TestMonitorWorkArea(t *testing.T) {
	m := Monitor{X: 1920, Y: 0, W: 1280, H: 1024}
	area := m.WorkArea(0, 24, 0, 0, 8)
	if area != (Rect{X: 1920, Y: 24, W: 1280, H: 992}) {
		t.Errorf("WorkArea = %+v", area)
	}
}

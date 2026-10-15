// Package layout contains pure geometry helpers. Nothing here talks to X, so
// the tiling rules can be unit tested without a display.
package layout

// Rect is a rectangle in root coordinates.
type Rect struct {
	X, Y, W, H int
}

// Empty reports whether the rectangle has no area.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Right returns the x coordinate just past the right edge.
func (r Rect) Right() int { return r.X + r.W }

// Bottom returns the y coordinate just past the bottom edge.
func (r Rect) Bottom() int { return r.Y + r.H }

// Area is the number of pixels the rectangle covers.
func (r Rect) Area() int { return max(r.W, 0) * max(r.H, 0) }

// Intersects reports whether two rectangles share any area.
func (r Rect) Intersects(o Rect) bool {
	return r.X < o.Right() && o.X < r.Right() && r.Y < o.Bottom() && o.Y < r.Bottom()
}

// Contains reports whether the point lies inside the rectangle.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.Right() && y >= r.Y && y < r.Bottom()
}

// Inset shrinks the rectangle by n pixels on every side.
func (r Rect) Inset(n int) Rect {
	return Rect{r.X + n, r.Y + n, r.W - 2*n, r.H - 2*n}
}

// AsymmetricInset shrinks each edge independently. Negative values grow the
// rectangle, which is what an outside border does.
func (r Rect) AsymmetricInset(left, right, top, bottom int) Rect {
	return Rect{
		X: r.X + left,
		Y: r.Y + top,
		W: r.W - left - right,
		H: r.H - top - bottom,
	}
}

// Direction is the axis along which a container splits its children.
type Direction int

// Split directions.
const (
	Horizontal Direction = iota // children side by side, split by x
	Vertical                    // children stacked, split by y
)

// Split cuts the rectangle into two along dir. first is the size of the first
// child in pixels; the second child gets the rest. gap is inserted between
// them. The first child gets the extra pixel when the split is inexact.
func (r Rect) Split(dir Direction, first, gap int) (Rect, Rect) {
	if dir == Horizontal {
		usable := r.W - gap
		if usable < 0 {
			usable = 0
		}
		a := clamp(first, 0, usable)
		return Rect{r.X, r.Y, a, r.H},
			Rect{r.X + a + gap, r.Y, usable - a, r.H}
	}
	usable := r.H - gap
	if usable < 0 {
		usable = 0
	}
	a := clamp(first, 0, usable)
	return Rect{r.X, r.Y, r.W, a},
		Rect{r.X, r.Y + a + gap, r.W, usable - a}
}

// SplitRatio cuts the rectangle in two using a ratio in [0,1] for the first
// child, rounded so the two children exactly fill the space.
func (r Rect) SplitRatio(dir Direction, ratio float64, gap int) (Rect, Rect) {
	usable := r.W
	if dir == Vertical {
		usable = r.H
	}
	usable -= gap
	if usable < 0 {
		usable = 0
	}
	first := int(float64(usable)*ratio + 0.5)
	first = clamp(first, 0, usable)
	return r.Split(dir, first, gap)
}

// Divide splits the rectangle into count equal parts, distributing the
// remainder one pixel at a time so the result exactly covers the rectangle.
func (r Rect) Divide(dir Direction, count, gap int) []Rect {
	if count <= 0 {
		return nil
	}
	extent := r.W
	if dir == Vertical {
		extent = r.H
	}
	if extent <= 0 {
		out := make([]Rect, count)
		for i := range out {
			out[i] = Rect{X: r.X, Y: r.Y}
		}
		return out
	}
	usable := extent - gap*(count-1)
	if usable < count {
		usable = count
	}
	base := usable / count
	extra := usable % count

	out := make([]Rect, 0, count)
	pos := r.X
	if dir == Vertical {
		pos = r.Y
	}
	for i := 0; i < count; i++ {
		size := base
		if i < extra {
			size++
		}
		if dir == Horizontal {
			out = append(out, Rect{X: pos, Y: r.Y, W: size, H: r.H})
		} else {
			out = append(out, Rect{X: r.X, Y: pos, W: r.W, H: size})
		}
		pos += size + gap
	}
	return out
}

// Center shrinks the rectangle to the given size, centred inside it.
func (r Rect) Center(w, h int) Rect {
	w = min(w, r.W)
	h = min(h, r.H)
	return Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}
}

// Union returns the smallest rectangle covering both inputs.
func Union(a, b Rect) Rect {
	if a.Empty() {
		return b
	}
	if b.Empty() {
		return a
	}
	x := min(a.X, b.X)
	y := min(a.Y, b.Y)
	return Rect{x, y, max(a.Right(), b.Right()) - x, max(a.Bottom(), b.Bottom()) - y}
}

// Monitor is one output with its work area.
type Monitor struct {
	Name    string
	X, Y    int
	W, H    int
	Primary bool
}

// WorkArea returns the usable area of the monitor, optionally reserving space
// for bars or gaps along its edges.
func (m Monitor) WorkArea(gap, topGap, leftGap, rightGap, bottomGap int) Rect {
	return Rect{m.X, m.Y, m.W, m.H}.AsymmetricInset(leftGap, rightGap, topGap, bottomGap)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

package wm

import (
	"lythrum/internal/layout"
)

// canvasLimit is how far the desktop may be panned from its origin, in
// pixels. The desktop has no edge in practice, and the bound only keeps the
// numbers small enough for X11 and for the layout maths.
const canvasLimit = 16384

// canvas is the pannable desktop: a surface much larger than the screen whose
// offset says which part of it the screen shows.
//
// Everything the shell draws at "desktop level" lives in canvas coordinates —
// the wallpaper, the icons and the frames of the windows — so panning moves all
// of it at once. The monitor geometry the shell shows stays in root
// coordinates, because that is what the X server and EWMH expect.
type canvas struct {
	wm *WM
	// offset is the canvas point the screen's top left corner shows.
	offset layout.Rect

	// drag holds the state of a pan in progress.
	dragging bool
	moved    bool
	lastX    int
	lastY    int
	anchorX  int
	anchorY  int
	fromX    int
	fromY    int
}

// newCanvas creates the pannable desktop of a shell.
func newCanvas(w *WM) *canvas { return &canvas{wm: w} }

// Offset returns the canvas point shown at the top left corner of the screen.
func (c *canvas) Offset() layout.Rect { return c.offset }

// ToCanvas turns a root coordinate into a canvas point.
func (c *canvas) ToCanvas(x, y int) (int, int) {
	return x + c.offset.X, y + c.offset.Y
}

// ToRoot turns a canvas point into a root coordinate.
func (c *canvas) ToRoot(x, y int) (int, int) {
	return x - c.offset.X, y - c.offset.Y
}

// ToRootRect moves a canvas rectangle onto the screen.
func (c *canvas) ToRootRect(r layout.Rect) layout.Rect {
	r.X -= c.offset.X
	r.Y -= c.offset.Y
	return r
}

// Viewport returns the part of the canvas a monitor shows right now. The
// monitor is given in root coordinates, the answer is a canvas rectangle.
func (c *canvas) Viewport(monitor layout.Rect) layout.Rect {
	monitor.X += c.offset.X
	monitor.Y += c.offset.Y
	return monitor
}

// PanBy moves the view by a number of pixels on the screen. The border of the
// canvas holds it back instead of letting it run off.
func (c *canvas) PanBy(dx, dy int) {
	c.offset.X = clampInt(c.offset.X+dx, -canvasLimit, canvasLimit)
	c.offset.Y = clampInt(c.offset.Y+dy, -canvasLimit, canvasLimit)
}

// PanTo puts the view at an absolute canvas point.
func (c *canvas) PanTo(x, y int) {
	c.offset.X = clampInt(x, -canvasLimit, canvasLimit)
	c.offset.Y = clampInt(y, -canvasLimit, canvasLimit)
}

// BeginDrag starts a pan. The pointer has to travel panThreshold before the
// desktop moves, so a plain click on the empty desktop still only clears the
// selection.
func (c *canvas) BeginDrag(rootX, rootY int) {
	c.dragging = true
	c.moved = false
	c.anchorX, c.anchorY = rootX, rootY
	c.lastX, c.lastY = rootX, rootY
}

// DragTo moves the view with the pointer while a drag is in progress. It
// reports whether the desktop actually moved.
func (c *canvas) DragTo(rootX, rootY int) bool {
	if !c.dragging {
		return false
	}
	if !c.moved {
		dx, dy := rootX-c.anchorX, rootY-c.anchorY
		if abs(dx) < panThreshold && abs(dy) < panThreshold {
			return false
		}
		c.moved = true
	}
	// The desktop follows the pointer: the view slides by the same distance
	// the pointer travelled since the last motion.
	c.PanBy(c.lastX-rootX, c.lastY-rootY)
	c.lastX, c.lastY = rootX, rootY
	return true
}

// EndDrag forgets a finished pan and reports whether the pointer actually
// panned the desktop.
func (c *canvas) EndDrag() bool {
	moved := c.moved
	c.dragging, c.moved = false, false
	return moved
}

// panThreshold is how far the pointer travels before a click turns into a pan.
const panThreshold = 6

// panStep is how far one wheel notch moves the desktop.
const panStep = 120

// PanByNotches moves the view by whole wheel notches along an axis. A notch
// forward goes deeper into the desktop, the way a page scrolls down.
func (c *canvas) PanByNotches(notches int, horizontal bool) {
	if notches == 0 {
		return
	}
	step := panStep * notches
	if horizontal {
		c.PanBy(-step, 0)
		return
	}
	c.PanBy(0, -step)
}

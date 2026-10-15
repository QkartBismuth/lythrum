package wm

import (
	"testing"

	"lythrum/internal/layout"
)

// TestCanvasRoundTrip checks that a point survives the trip between the screen
// and the desktop in both directions.
func TestCanvasRoundTrip(t *testing.T) {
	c := newCanvas(nil)
	c.PanTo(300, -200)
	x, y := c.ToCanvas(10, 20)
	if x != 310 || y != -180 {
		t.Errorf("ToCanvas(10,20) = (%d,%d), want (310,-180)", x, y)
	}
	if rx, ry := c.ToRoot(310, -180); rx != 10 || ry != 20 {
		t.Errorf("ToRoot(310,-180) = (%d,%d), want (10,20)", rx, ry)
	}
	rect := layout.Rect{X: 300, Y: -200, W: 640, H: 400}
	root := c.ToRootRect(rect)
	if root != (layout.Rect{X: 0, Y: 0, W: 640, H: 400}) {
		t.Errorf("ToRootRect = %+v, want the rectangle on the screen", root)
	}
}

// TestCanvasViewport checks the rule the tiling works by: the window area is
// the part of the desktop under the screen, so panning moves the windows.
func TestCanvasViewport(t *testing.T) {
	screen := layout.Rect{X: 0, Y: 0, W: 1280, H: 800}
	c := newCanvas(nil)
	if got := c.Viewport(screen); got != screen {
		t.Errorf("Viewport at the origin = %+v, want %+v", got, screen)
	}
	c.PanBy(400, 250)
	want := layout.Rect{X: 400, Y: 250, W: 1280, H: 800}
	if got := c.Viewport(screen); got != want {
		t.Errorf("Viewport after panning = %+v, want %+v", got, want)
	}
}

// TestCanvasPanClamps checks that the desktop has an edge far away but not an
// overflow.
func TestCanvasPanClamps(t *testing.T) {
	c := newCanvas(nil)
	c.PanBy(-10*canvasLimit, -10*canvasLimit)
	if c.Offset().X != -canvasLimit || c.Offset().Y != -canvasLimit {
		t.Errorf("offset = %+v, want the border", c.Offset())
	}
	c.PanTo(10*canvasLimit, 10*canvasLimit)
	if c.Offset().X != canvasLimit || c.Offset().Y != canvasLimit {
		t.Errorf("offset = %+v, want the border", c.Offset())
	}
}

// TestCanvasDragFollowsThePointer checks the two halves of a pan: a press that
// does not move stays a click, and a drag moves the desktop as far as the
// pointer went.
func TestCanvasDragFollowsThePointer(t *testing.T) {
	c := newCanvas(nil)
	c.BeginDrag(500, 400)
	if c.DragTo(500, 400) {
		t.Error("a press without movement must not pan the desktop")
	}
	if c.DragTo(505, 402) {
		t.Error("a movement below the threshold must not pan the desktop")
	}
	if !c.DragTo(400, 300) {
		t.Fatal("a movement past the threshold must pan the desktop")
	}
	if c.Offset().X != 100 || c.Offset().Y != 100 {
		t.Errorf("offset = %+v, want the desktop to follow the pointer by 100,100", c.Offset())
	}
	// Every further motion keeps moving it, not just the first one.
	if !c.DragTo(350, 260) {
		t.Error("the second motion of a drag must keep panning")
	}
	if c.Offset().X != 150 || c.Offset().Y != 140 {
		t.Errorf("offset = %+v, want 150,140: the pointer moved 50 and 40", c.Offset())
	}
	if !c.EndDrag() {
		t.Error("EndDrag must report that the desktop moved")
	}
	if c.EndDrag() {
		t.Error("a finished drag must not report movement twice")
	}
}

// TestCanvasDragReportsNoPan checks the plain click: the shell uses this to
// tell "the user clicked the empty desktop" from "the user dragged".
func TestCanvasDragReportsNoPan(t *testing.T) {
	c := newCanvas(nil)
	c.BeginDrag(100, 100)
	c.DragTo(101, 101)
	if c.EndDrag() {
		t.Error("a click without movement must not count as a pan")
	}
	if c.Offset() != (layout.Rect{}) {
		t.Errorf("offset = %+v, want the desktop to stay put", c.Offset())
	}
}

// TestCanvasPanSteps checks the wheel: down goes deeper into the desktop, up
// comes back.
func TestCanvasPanByNotches(t *testing.T) {
	c := newCanvas(nil)
	c.PanByNotches(1, false)
	if c.Offset().Y != -panStep {
		t.Errorf("one notch down gave %+v, want -%d", c.Offset(), panStep)
	}
	c.PanByNotches(-1, false)
	if c.Offset().Y != 0 {
		t.Errorf("one notch up gave %+v, want the origin", c.Offset())
	}
	c.PanByNotches(1, true)
	if c.Offset().X != -panStep {
		t.Errorf("one notch to the right gave %+v, want -%d", c.Offset(), panStep)
	}
}

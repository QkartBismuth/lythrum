package main

/*
#cgo pkg-config: x11 xi
#include <X11/Xlib.h>
#include <X11/extensions/XInput2.h>
#include <X11/extensions/XI2.h>

// The board asks for the touch events of the screen: the beginning, the
// movement and the end of a touch. The mask of the events covers the bits
// 16 to 23, the third byte of the mask.
static int canvas_xi_select_touch(Display *dpy, Window w) {
	unsigned char bits[3];
	bits[0] = 0;
	bits[1] = 0;
	bits[2] = (1 << (XI_TouchBegin - 16)) | (1 << (XI_TouchUpdate - 16)) |
	          (1 << (XI_TouchEnd - 16));
	XIEventMask mask;
	mask.deviceid = XIAllMasterDevices;
	mask.mask_len = 3;
	mask.mask = bits;
	return XISelectEvents(dpy, w, &mask, 1);
}

// The touch events arrive as generic events: the helper reads the type, the
// finger and the position out of them.
static int canvas_xi_touch(Display *dpy, XEvent *xev, int *type, int *id,
                           double *x, double *y) {
	XGenericEventCookie *cookie = &xev->xcookie;
	if (!XGetEventData(dpy, cookie)) {
		return 0;
	}
	XIDeviceEvent *de = (XIDeviceEvent *)cookie->data;
	if (de) {
		*type = de->evtype;
		*id = de->detail;
		*x = de->root_x;
		*y = de->root_y;
	}
	XFreeEventData(dpy, cookie);
	return 1;
}
*/
import "C"

import (
	"math"
	"time"
)

// touchPoint is a finger on the screen.
type touchPoint struct {
	x, y float64 // in the coordinates of the board
}

// gesturesInit asks the X server for the touch events of the board. The
// touchpad itself is handled by the wheel events the server sends for the
// two finger scroll; a screen is handled here.
func (v *view) gesturesInit() {
	if !v.cfg.Gestures.Enabled {
		return
	}
	major, eventBase, errorBase := C.int(0), C.int(0), C.int(0)
	if C.XQueryExtension(v.dpy, cstr("XInputExtension"), &major, &eventBase, &errorBase) == 0 {
		return
	}
	// The client asks for the version 2.2, the one that carries the touch
	// events; the server answers with the version it speaks.
	maj, min := C.int(2), C.int(2)
	if C.XIQueryVersion(v.dpy, &maj, &min) == 0 || maj < 2 {
		return
	}
	C.canvas_xi_select_touch(v.dpy, v.win)
	C.XFlush(v.dpy)
	v.gesturesOK = true
}

// onXIEvent handles the events of the touch devices.
func (v *view) onXIEvent(xev *C.XEvent) {
	var kind, id C.int
	var x, y C.double
	if C.canvas_xi_touch(v.dpy, xev, &kind, &id, &x, &y) == 0 {
		return
	}
	switch kind {
	case C.XI_TouchBegin:
		v.touchBegin(int(id), float64(x), float64(y))
	case C.XI_TouchUpdate:
		v.touchUpdate(int(id), float64(x), float64(y))
	case C.XI_TouchEnd:
		v.touchEnd(int(id))
	}
}

// touchPointAt returns the position of a finger in the coordinates of the
// board.
func (v *view) touchPointAt(rx, ry float64) touchPoint {
	x, y := v.rootToBoard(int(math.Round(rx)), int(math.Round(ry)))
	return touchPoint{x: float64(x), y: float64(y)}
}

// touchBegin starts a gesture.
func (v *view) touchBegin(id int, rx, ry float64) {
	if v.touches == nil {
		v.touches = map[int]touchPoint{}
	}
	p := v.touchPointAt(rx, ry)
	v.touches[id] = p
	v.touchAt = time.Now()
	if len(v.touches) == 1 {
		v.touchStart = p
		v.touchStartTime = time.Now()
		v.touchMoved = false
		return
	}
	// A second finger turns the gesture into a pinch.
	v.touchPinch = true
	dist, mx, my := v.touchGeometry()
	v.touchBaseDist = dist
	v.touchBaseMidX, v.touchBaseMidY = mx, my
}

// touchUpdate moves the board with the fingers.
func (v *view) touchUpdate(id int, rx, ry float64) {
	old, ok := v.touches[id]
	if !ok {
		return
	}
	p := v.touchPointAt(rx, ry)
	v.touches[id] = p
	v.touchAt = time.Now()
	if math.Abs(p.x-old.x) > 0.5 || math.Abs(p.y-old.y) > 0.5 {
		v.touchMoved = true
	}
	if len(v.touches) == 1 {
		// One finger pans the board.
		v.ox -= (p.x - old.x) / v.zoom
		v.oy -= (p.y - old.y) / v.zoom
		v.redraw = true
		return
	}
	// Two fingers zoom and pan at the same time.
	dist, mx, my := v.touchGeometry()
	if v.touchBaseDist > 4 && dist > 4 {
		factor := dist / v.touchBaseDist
		sens := v.cfg.Gestures.Sensitivity
		if sens <= 0 {
			sens = 1
		}
		factor = 1 + (factor-1)*sens
		if v.cfg.Gestures.Invert {
			factor = 1 / factor
		}
		v.zoomAt(mx, my, factor)
	}
	v.ox -= (mx - v.touchBaseMidX) / v.zoom
	v.oy -= (my - v.touchBaseMidY) / v.zoom
	v.touchBaseDist = dist
	v.touchBaseMidX, v.touchBaseMidY = mx, my
	v.redraw = true
}

// touchEnd ends the gesture. A short touch without movement is a click.
func (v *view) touchEnd(id int) {
	p, ok := v.touches[id]
	if !ok {
		return
	}
	delete(v.touches, id)
	v.touchAt = time.Now()
	if len(v.touches) > 0 {
		return
	}
	if !v.touchPinch && !v.touchMoved &&
		time.Since(v.touchStartTime) < 400*time.Millisecond {
		v.tapAt(int(p.x), int(p.y))
	}
	v.touchPinch = false
	v.touchMoved = false
}

// touchGeometry returns the distance between the fingers and the position of
// their middle.
func (v *view) touchGeometry() (dist, mx, my float64) {
	if len(v.touches) < 2 {
		for _, p := range v.touches {
			mx, my = p.x, p.y
		}
		return 0, mx, my
	}
	var ax, ay, bx, by float64
	first := true
	for _, p := range v.touches {
		if first {
			ax, ay = p.x, p.y
			first = false
			continue
		}
		bx, by = p.x, p.y
	}
	dx, dy := bx-ax, by-ay
	return math.Hypot(dx, dy), (ax + bx) / 2, (ay + by) / 2
}

// tapAt turns a touch into a click, the way a mouse click would arrive.
func (v *view) tapAt(x, y int) {
	var be C.XButtonEvent
	be._type = C.ButtonPress
	be.display = v.dpy
	be.window = v.win
	be.root = C.XDefaultRootWindow(v.dpy)
	be.time = C.CurrentTime
	be.x = C.int(x)
	be.y = C.int(y)
	be.state = C.Button1Mask
	be.button = 1
	be.same_screen = 1
	v.onPress(&be)
	be._type = C.ButtonRelease
	be.state = 0
	v.onRelease(&be)
}

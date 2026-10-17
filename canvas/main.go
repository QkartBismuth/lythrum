// Command canvas is a small X11 board built on cgo and libX11. The board is an
// infinite grid; the viewport pans and zooms over it with the mouse, wheel or
// keyboard. Nothing is drawn by the user: the point is navigation, not sketching.
package main

/*
#cgo pkg-config: x11
#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/keysym.h>
#include <X11/cursorfont.h>
*/
import "C"

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"unsafe"
)

type color struct{ r, g, b byte }

var (
	bg        = color{0x14, 0x16, 0x1a}
	gridMinor = color{0x22, 0x26, 0x2e}
	gridMajor = color{0x30, 0x36, 0x42}
	axisCol   = color{0x8a, 0x4a, 0x4a}
	hudCol    = color{0xc8, 0xcc, 0xd4}
	textCol   = color{0xe8, 0xec, 0xf2}
)

const (
	panKey   = 40.0 // screen pixels per arrow press
	panWheel = 60.0 // screen pixels per panning wheel notch
	zWheel   = 1.20 // zoom factor per wheel notch
	zKey     = 1.25 // zoom factor per key press
	zMin     = 0.15
	zMax     = 8.0
	gridPx   = 44.0 // target on-screen spacing of the minor grid
)

type marker struct {
	x, y  float64
	label string
	col   color
}

var markers = []marker{
	{0, 0, "origin", color{0x4c, 0x6e, 0xf5}},
	{420, -220, "notes", color{0x2b, 0x6c, 0xb0}},
	{-520, 260, "map", color{0x2f, 0xa0, 0x43}},
	{760, 320, "media", color{0xe6, 0x7e, 0x22}},
	{-360, -460, "settings", color{0x9b, 0x59, 0xb6}},
	{160, 520, "files", color{0x00, 0xa9, 0x9c}},
	{-820, -140, "search", color{0xd3, 0x2f, 0x2f}},
}

type view struct {
	dpy    *C.Display
	screen C.int
	depth  C.uint
	win    C.Window
	gc     C.GC
	pm     C.Pixmap // offscreen buffer; the window is only ever shown via XCopyArea

	w, h   int
	ox, oy float64 // board coordinate shown at the viewport's top-left pixel
	zoom   float64

	dragging     bool
	lastX, lastY int

	redraw   bool // content changed: repaint the buffer, then blit
	needBlit bool // buffer is valid but the window needs it again (expose)

	wmDelete C.Atom
}

func main() {
	runtime.LockOSThread()

	dpy := C.XOpenDisplay(nil)
	if dpy == nil {
		fmt.Fprintln(os.Stderr, "canvas: cannot open display; is DISPLAY set?")
		os.Exit(1)
	}

	screen := C.XDefaultScreen(dpy)
	root := C.XDefaultRootWindow(dpy)

	win := C.XCreateSimpleWindow(
		dpy, root,
		40, 40, 900, 620, 0,
		C.XBlackPixel(dpy, screen),
		C.XWhitePixel(dpy, screen),
	)
	if win == 0 {
		fmt.Fprintln(os.Stderr, "canvas: cannot create window")
		os.Exit(1)
	}

	C.XSelectInput(dpy, win,
		C.ExposureMask|C.KeyPressMask|C.ButtonPressMask|C.ButtonReleaseMask|
			C.PointerMotionMask|C.StructureNotifyMask)

	C.XStoreName(dpy, win, C.CString("Board - Go / X11"))

	v := &view{
		dpy:    dpy,
		screen: screen,
		depth:  C.uint(C.XDefaultDepth(dpy, screen)),
		win:    win,
		gc:     C.XCreateGC(dpy, win, 0, nil),
		zoom:   1,
	}
	if font := C.XLoadQueryFont(dpy, C.CString("fixed")); font != nil {
		C.XSetFont(dpy, v.gc, font.fid)
	}
	if cur := C.XCreateFontCursor(dpy, C.XC_fleur); cur != 0 {
		C.XDefineCursor(dpy, win, cur)
	}

	v.wmDelete = C.XInternAtom(dpy, C.CString("WM_DELETE_WINDOW"), C.False)
	proto := []C.Atom{v.wmDelete}
	C.XSetWMProtocols(dpy, win, (*C.Atom)(unsafe.Pointer(&proto[0])), 1)

	var geom C.XWindowAttributes
	C.XGetWindowAttributes(dpy, win, &geom)
	v.resize(int(geom.width), int(geom.height))
	v.centerOrigin()

	C.XMapWindow(dpy, win)
	C.XFlush(dpy)

	fmt.Println("board: infinite canvas ready")
	fmt.Println("  drag        pan        wheel       zoom at pointer")
	fmt.Println("  shift+wheel pan left/right      ctrl+wheel  pan up/down")
	fmt.Println("  arrows      pan        + / -       zoom")
	fmt.Println("  r / Home    recenter   0           zoom 100%   q / Esc  quit")

	v.paint()
	v.blit()
	v.run()
}

// run drains the whole event queue before painting, so a burst of motion
// events collapses into a single frame instead of one repaint per event.
func (v *view) run() {
	var xev C.XEvent
	for {
		C.XNextEvent(v.dpy, &xev)
		for {
			v.handle(&xev)
			if C.XPending(v.dpy) == 0 {
				break
			}
			C.XNextEvent(v.dpy, &xev)
		}
		switch {
		case v.redraw:
			v.paint()
			v.blit()
		case v.needBlit:
			v.blit()
		}
		v.redraw, v.needBlit = false, false
	}
}

func (v *view) handle(xev *C.XEvent) {
	any := (*C.XAnyEvent)(unsafe.Pointer(xev))
	switch any._type {
	case C.Expose:
		v.needBlit = true
	case C.ConfigureNotify:
		ce := (*C.XConfigureEvent)(unsafe.Pointer(xev))
		w, h := int(ce.width), int(ce.height)
		if w != v.w || h != v.h {
			v.resize(w, h)
			v.redraw = true
		}
	case C.ButtonPress:
		v.onPress((*C.XButtonEvent)(unsafe.Pointer(xev)))
	case C.ButtonRelease:
		be := (*C.XButtonEvent)(unsafe.Pointer(xev))
		if be.button == 1 || be.button == 2 {
			v.stopDrag()
		}
	case C.MotionNotify:
		v.onMotion((*C.XMotionEvent)(unsafe.Pointer(xev)))
	case C.KeyPress:
		v.onKey((*C.XKeyEvent)(unsafe.Pointer(xev)))
	case C.ClientMessage:
		ce := (*C.XClientMessageEvent)(unsafe.Pointer(xev))
		if ce.message_type == v.wmDelete {
			C.XCloseDisplay(v.dpy)
			os.Exit(0)
		}
	}
}

func (v *view) onPress(be *C.XButtonEvent) {
	x, y := float64(be.x), float64(be.y)
	switch be.button {
	case 1, 2:
		v.dragging = true
		v.lastX, v.lastY = int(be.x), int(be.y)
		// Keep the motion stream even when the pointer leaves the window.
		C.XGrabPointer(v.dpy, v.win, C.False,
			C.ButtonPressMask|C.ButtonReleaseMask|C.PointerMotionMask,
			C.GrabModeAsync, C.GrabModeAsync, 0, 0, C.CurrentTime)
	case 4:
		switch {
		case be.state&C.ShiftMask != 0:
			v.ox -= panWheel / v.zoom
		case be.state&C.ControlMask != 0:
			v.oy -= panWheel / v.zoom
		default:
			v.zoomAt(x, y, zWheel)
		}
		v.redraw = true
	case 5:
		switch {
		case be.state&C.ShiftMask != 0:
			v.ox += panWheel / v.zoom
		case be.state&C.ControlMask != 0:
			v.oy += panWheel / v.zoom
		default:
			v.zoomAt(x, y, 1/zWheel)
		}
		v.redraw = true
	}
}

func (v *view) stopDrag() {
	if !v.dragging {
		return
	}
	v.dragging = false
	C.XUngrabPointer(v.dpy, C.CurrentTime)
}

func (v *view) onMotion(me *C.XMotionEvent) {
	if !v.dragging {
		return
	}
	if me.state&C.Button1Mask == 0 && me.state&C.Button2Mask == 0 {
		v.stopDrag()
		return
	}
	x, y := int(me.x), int(me.y)
	dx, dy := float64(x-v.lastX), float64(y-v.lastY)
	// Moving the pointer right drags the board right, so the viewport shifts left.
	v.ox -= dx / v.zoom
	v.oy -= dy / v.zoom
	v.lastX, v.lastY = x, y
	v.redraw = true
}

func (v *view) onKey(ke *C.XKeyEvent) {
	ks := C.XLookupKeysym(ke, 0)
	cx, cy := float64(v.w)/2, float64(v.h)/2
	switch ks {
	case 0xff1b, 'q': // Escape, q
		C.XCloseDisplay(v.dpy)
		os.Exit(0)
	case 0xff50, 'r': // Home, r
		v.centerOrigin()
	case '0':
		v.zoomAt(cx, cy, 1/v.zoom)
	case 0xff51: // Left
		v.ox -= panKey / v.zoom
	case 0xff53: // Right
		v.ox += panKey / v.zoom
	case 0xff52: // Up
		v.oy -= panKey / v.zoom
	case 0xff54: // Down
		v.oy += panKey / v.zoom
	case 0xff55: // Page Up
		v.oy -= float64(v.h) / v.zoom
	case 0xff56: // Page Down
		v.oy += float64(v.h) / v.zoom
	case '+', '=':
		v.zoomAt(cx, cy, zKey)
	case '-', '_':
		v.zoomAt(cx, cy, 1/zKey)
	default:
		return
	}
	v.redraw = true
}

// zoomAt scales by f while keeping the board point under (px, py) fixed.
func (v *view) zoomAt(px, py, f float64) {
	nz := math.Min(zMax, math.Max(zMin, v.zoom*f))
	if nz == v.zoom {
		return
	}
	v.ox += px/v.zoom - px/nz
	v.oy += py/v.zoom - py/nz
	v.zoom = nz
}

// centerOrigin puts board (0, 0) at the middle of the viewport.
func (v *view) centerOrigin() {
	v.ox = -float64(v.w) / (2 * v.zoom)
	v.oy = -float64(v.h) / (2 * v.zoom)
}

func (v *view) resize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if v.pm != 0 {
		C.XFreePixmap(v.dpy, v.pm)
	}
	v.pm = C.XCreatePixmap(v.dpy, v.win, C.uint(w), C.uint(h), v.depth)
	if v.pm == 0 {
		fmt.Fprintln(os.Stderr, "canvas: cannot create pixmap")
		os.Exit(1)
	}
	v.w, v.h = w, h
}

// paint renders the viewport straight into the offscreen pixmap with server
// side primitives. There is no per-pixel client buffer, so a frame is a few
// dozen X requests regardless of window size.
func (v *view) paint() {
	v.fill(0, 0, v.w, v.h, bg)
	v.grid()
	v.axes()
	v.markers()
	v.hud()
}

// gridStep picks a 1/2/5-based spacing so the minor grid stays near gridPx
// screen pixels no matter the zoom.
func (v *view) gridStep() float64 {
	target := gridPx / v.zoom
	p := math.Pow(10, math.Floor(math.Log10(target)))
	for _, m := range []float64{1, 2, 5, 10} {
		if p*m >= target*0.999 {
			return p * m
		}
	}
	return p * 10
}

func (v *view) grid() {
	step := v.gridStep()
	bx1 := v.ox + float64(v.w)/v.zoom
	by1 := v.oy + float64(v.h)/v.zoom
	k0 := int(math.Floor(v.ox / step))
	k1 := int(math.Ceil(bx1 / step))
	l0 := int(math.Floor(v.oy / step))
	l1 := int(math.Ceil(by1 / step))

	v.fg(gridMinor)
	for k := k0; k <= k1; k++ {
		if mod(k, 5) == 0 {
			continue
		}
		x := v.sx(float64(k) * step)
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x), 0, C.int(x), C.int(v.h))
	}
	for k := l0; k <= l1; k++ {
		if mod(k, 5) == 0 {
			continue
		}
		y := v.sy(float64(k) * step)
		C.XDrawLine(v.dpy, v.pm, v.gc, 0, C.int(y), C.int(v.w), C.int(y))
	}
	v.fg(gridMajor)
	for k := k0; k <= k1; k++ {
		if mod(k, 5) != 0 {
			continue
		}
		x := v.sx(float64(k) * step)
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x), 0, C.int(x), C.int(v.h))
	}
	for k := l0; k <= l1; k++ {
		if mod(k, 5) != 0 {
			continue
		}
		y := v.sy(float64(k) * step)
		C.XDrawLine(v.dpy, v.pm, v.gc, 0, C.int(y), C.int(v.w), C.int(y))
	}
}

func (v *view) axes() {
	v.fg(axisCol)
	if x := v.sx(0); x >= 0 && x < v.w {
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x), 0, C.int(x), C.int(v.h))
	}
	if y := v.sy(0); y >= 0 && y < v.h {
		C.XDrawLine(v.dpy, v.pm, v.gc, 0, C.int(y), C.int(v.w), C.int(y))
	}
}

func (v *view) markers() {
	const (
		boxW, boxH = 190.0, 120.0
		barH       = 26.0
	)
	for _, m := range markers {
		w1 := int(math.Round(boxW * v.zoom))
		h1 := int(math.Round(boxH * v.zoom))
		bh := int(math.Round(barH * v.zoom))
		if w1 < 1 {
			w1 = 1
		}
		if h1 < 1 {
			h1 = 1
		}
		if bh < 1 {
			bh = 1
		}
		sx, sy := v.sx(m.x), v.sy(m.y)
		if sx+w1 < 0 || sy+h1 < 0 || sx > v.w || sy > v.h {
			continue
		}
		v.fill(sx, sy, w1, h1, color{0x1c, 0x1f, 0x26})
		v.fill(sx, sy, w1, bh, m.col)
		v.fg(color{0x30, 0x36, 0x42})
		C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(sx), C.int(sy),
			C.uint(w1-1), C.uint(h1-1))
		if w1 < 70 {
			continue
		}
		v.text(sx+12, sy+bh-8, m.label, textCol)
		v.text(sx+12, sy+bh+18, fmt.Sprintf("(%.0f, %.0f)", m.x, m.y), hudCol)
	}
}

func (v *view) hud() {
	v.text(14, 22, fmt.Sprintf("board  x=%.0f  y=%.0f  zoom=%.0f%%",
		v.ox+float64(v.w)/(2*v.zoom), v.oy+float64(v.h)/(2*v.zoom), v.zoom*100), hudCol)
	v.text(14, v.h-12, "drag / wheel / arrows   ctrl+wheel / shift+wheel pan   +/- zoom   r recenter", hudCol)
}

// sx / sy map board coordinates to window pixels.
func (v *view) sx(bx float64) int {
	return int(math.Round((bx - v.ox) * v.zoom))
}

func (v *view) sy(by float64) int {
	return int(math.Round((by - v.oy) * v.zoom))
}

func (v *view) fill(x, y, w, h int, c color) {
	v.fg(c)
	C.XFillRectangle(v.dpy, v.pm, v.gc, C.int(x), C.int(y), C.uint(w), C.uint(h))
}

func (v *view) text(x, y int, s string, c color) {
	v.fg(c)
	cs := C.CString(s)
	C.XDrawString(v.dpy, v.pm, v.gc, C.int(x), C.int(y), cs, C.int(len(s)))
	C.free(unsafe.Pointer(cs))
}

func (v *view) fg(c color) {
	C.XSetForeground(v.dpy, v.gc, C.ulong(pixel(c)))
}

// blit shows the finished buffer in one server-side operation.
func (v *view) blit() {
	C.XCopyArea(v.dpy, v.pm, v.win, v.gc, 0, 0,
		C.uint(v.w), C.uint(v.h), 0, 0)
	C.XFlush(v.dpy)
}

func pixel(c color) int {
	return int(c.r)<<16 | int(c.g)<<8 | int(c.b)
}

func mod(a, b int) int {
	return ((a % b) + b) % b
}

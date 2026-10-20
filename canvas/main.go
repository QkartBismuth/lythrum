// Command canvas is a small X11 board built on cgo and libX11. The board is an
// infinite grid holding shortcuts to the contents of the desktop folder. The
// viewport pans and zooms over it with the mouse, wheel or keyboard.
package main

/*
#cgo pkg-config: x11 xft
#include <stdio.h>
#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/keysym.h>
#include <X11/XKBlib.h>
#include <X11/cursorfont.h>
#include <X11/Xft/Xft.h>

// canvas_xerror keeps the default Xlib handler from terminating the whole
// session on a recoverable protocol error (e.g. a key already grabbed by
// someone else). Real errors are still reported on stderr.
static int canvas_xerror(Display *dpy, XErrorEvent *ev) {
	if (ev->error_code != BadAccess) {
		char buf[256];
		XGetErrorText(dpy, ev->error_code, buf, sizeof buf);
		fprintf(stderr, "canvas: X error: %s (request %d)\n", buf, ev->request_code);
	}
	return 0;
}
static void canvas_ignore_badaccess(void) {
	XSetErrorHandler(canvas_xerror);
}
static void canvas_destroy_image(XImage *img) { XDestroyImage(img); }
*/
import "C"

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
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
	paperCol  = color{0xd8, 0xdc, 0xe4}
	foldCol   = color{0x91, 0x99, 0xa6}
	selBg     = color{0x24, 0x2b, 0x38}
	selBorder = color{0x4c, 0x6e, 0xf5}
	folderCol = color{0xe0, 0xa8, 0x2e}

	winTitleBg = color{0x2a, 0x33, 0x46}
	winTitleOn = color{0x3a, 0x5a, 0x8c} // title bar of the active (top) window
	winTitleFg = color{0xdd, 0xe3, 0xee}
	winGripCol = color{0x4c, 0x6e, 0xf5}
	winCloseBg = color{0x38, 0x2c, 0x38}
	winCloseFg = color{0xe0, 0x6a, 0x6a}

	menuBg     = color{0x20, 0x25, 0x2f}
	menuBorder = color{0x46, 0x4e, 0x5e}
	panelBg    = color{0x16, 0x19, 0x20}
	appGlyph   = color{0x5b, 0x8d, 0xe8}
	badgeBg    = color{0x10, 0x14, 0x1c}
	badgeFg    = color{0xe8, 0xec, 0xf2}
)

const (
	panKey   = 40.0 // screen pixels per arrow press
	panWheel = 60.0 // screen pixels per panning wheel notch
	zWheel   = 1.20 // zoom factor per wheel notch
	zKey     = 1.25 // zoom factor per key press
	zMin     = 0.15
	zMax     = 8.0
	gridPx   = 44.0 // target on-screen spacing of the minor grid

	cellW   = 120.0 // shortcut cell size in board units
	cellH   = 146.0
	tile    = 88.0 // icon glyph size
	tileTop = 10.0
	gapX    = 14.0
	gapY    = 12.0
	perCol  = 5 // rows per column

	winTitleH = 22 // title bar height, screen pixels
	winGrip   = 16 // corner grip size, screen pixels
	winClose  = 22 // close button size, screen pixels (equals the title bar)

	thumbSize = 256 // generated thumbnail edge, screen pixels squared cache
	wallFps   = 12  // target frame rate of animated wallpapers
)

type boardIcon struct {
	name  string
	path  string
	isDir bool
	isApp bool   // a .desktop launcher (Qt or otherwise)
	exec  string // Exec line of a .desktop launcher
	label string // display name (from .desktop Name, else file name)
	up    bool
	col   color

	thumb image.Image // decoded preview (photo, video poster, gif frame)

	x, y, w, h float64 // cell rectangle in board coordinates
}

// menuItem is one row of the context menu; sep turns the row into a divider.
type menuItem struct {
	label string
	sep   bool
	do    func()
}

// managedWin is a real application window reparented into the board. Its
// position is kept in board units so it sticks to the canvas while the viewport
// pans, but its size stays in screen pixels: zooming the board must not resize
// the window, otherwise the application reflows its content (text changes size).
type managedWin struct {
	win   C.Window
	title string
	bx    float64 // board position of the window's top-left corner
	by    float64
	wp    float64 // on-screen size, in pixels: zooming the board must not
	hp    float64 // resize the window or make the application reflow

	lastX, lastY, lastW, lastH int
}

// pendingLaunch remembers which top-level window to adopt once a freshly
// started application maps it.
type pendingLaunch struct {
	cx, cy float64 // board point where the window should be centred
	before map[C.Window]bool
	until  time.Time
}

type view struct {
	dpy    *C.Display
	screen C.int
	depth  C.uint
	win    C.Window
	gc     C.GC
	pm     C.Pixmap
	visual *C.Visual
	cmap   C.Colormap

	xftFont *C.XftFont
	xftDraw *C.XftDraw
	xcolors map[color]*C.XftColor

	w, h   int
	ox, oy float64 // board coordinate shown at the viewport's top-left pixel
	zoom   float64

	root     string // desktop folder, the single source of shortcuts
	icons    []boardIcon
	wins     []managedWin
	pending  *pendingLaunch
	xkb      []string // keyboard layout names, for the HUD indicator
	selset   map[int]bool
	anchor   int // last single-clicked icon, base for Shift+click ranges
	pressed  int
	pressX   int
	pressY   int
	pressAdd bool
	lastIdx  int
	lastTime time.Time

	iconDrag bool // moving selected icons
	orig     map[int][2]float64

	band    bool // rubber-band selection in progress
	bandX0  int
	bandY0  int
	bandX1  int
	bandY1  int
	bandAdd bool

	menuOpen  bool
	menuX     int
	menuY     int
	menuW     int
	menu      []menuItem
	menuHover int

	promptOn    bool
	promptTitle string
	promptText  string
	promptFresh bool
	promptCb    func(string)

	clip    []string // cut/copied paths
	clipCut bool
	status  string

	dragging     bool
	lastX, lastY int

	winDrag   int // window being moved by its title, -1 when none
	winResize int // window being scaled by its grip, -1 when none
	dragSX    int // pointer position, screen pixels, when the drag began
	dragSY    int
	dragSW    int // window screen size when the drag began
	dragSH    int
	dragBX    float64
	dragBY    float64
	dragBW    float64 // window screen size when the drag began
	dragBH    float64

	redraw   bool
	needBlit bool

	// Background work: thumbnail generation and wallpaper frames stream into
	// the event loop through these channels; each message is accompanied by a
	// wake event sent via a secondary X connection.
	thumbJobs    chan thumbJob
	thumbDone    chan thumbResult
	thumbPending map[string]bool
	thumbImg     *C.XImage // shared staging ZPixmap for thumbnails
	thumbCap     int       // staging square edge in pixels

	wall      *wallpaper
	wallGen   int       // bump on every boot so stale frames are dropped
	wallImg   *C.XImage // screen-sized ZPixmap holding the current frame
	wallW     int
	wallH     int
	wallFrame chan wallMsg
	wallEOF   chan int

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

	// --session turns the board into a desktop session a display manager can
	// offer in place of a full desktop environment such as KDE.
	session := false
	for _, a := range os.Args[1:] {
		if a == "--session" || a == "-s" {
			session = true
		}
	}
	if session {
		os.Setenv("XDG_CURRENT_DESKTOP", "Canvas")
		os.Setenv("XDG_SESSION_DESKTOP", "canvas")
		os.Setenv("DESKTOP_SESSION", "canvas")
	}

	// A bare X session has no desktop environment to manage keyboard layouts,
	// so the board installs them itself (setxkbmap) when it runs as a session.
	var xkbLayouts []string
	if session {
		if layout, variant, options := keyboardConfig(); applyKeyboard(layout, variant, options) {
			xkbLayouts = splitLayouts(layout)
		}
	}

	wx, wy, ww, wh := 40, 40, 900, 620
	if session {
		wx, wy = 0, 0
		ww = int(C.XDisplayWidth(dpy, screen))
		wh = int(C.XDisplayHeight(dpy, screen))
	}
	win := C.XCreateSimpleWindow(
		dpy, root,
		C.int(wx), C.int(wy), C.uint(ww), C.uint(wh), 0,
		C.XBlackPixel(dpy, screen),
		C.XWhitePixel(dpy, screen),
	)
	if win == 0 {
		fmt.Fprintln(os.Stderr, "canvas: cannot create window")
		os.Exit(1)
	}

	C.XSelectInput(dpy, win,
		C.ExposureMask|C.KeyPressMask|C.ButtonPressMask|C.ButtonReleaseMask|
			C.PointerMotionMask|C.StructureNotifyMask|C.SubstructureNotifyMask)
	// Watch top-level windows so a freshly launched application can be adopted.
	C.XSelectInput(dpy, root, C.SubstructureNotifyMask)

	C.XStoreName(dpy, win, C.CString("Board - Go / X11"))

	v := &view{
		dpy:       dpy,
		screen:    screen,
		depth:     C.uint(C.XDefaultDepth(dpy, screen)),
		win:       win,
		gc:        C.XCreateGC(dpy, win, 0, nil),
		visual:    C.XDefaultVisual(dpy, screen),
		cmap:      C.XDefaultColormap(dpy, screen),
		zoom:      1,
		selset:    map[int]bool{},
		anchor:    -1,
		pressed:   -1,
		lastIdx:   -1,
		winDrag:   -1,
		winResize: -1,
		xcolors:   map[color]*C.XftColor{},
		xkb:       xkbLayouts,
	}
	v.xftFont = C.XftFontOpenName(dpy, screen, C.CString("sans:size=10"))
	if v.xftFont == nil {
		fmt.Fprintln(os.Stderr, "canvas: xft font unavailable, falling back to core text")
	}
	if cur := C.XCreateFontCursor(dpy, C.XC_fleur); cur != 0 {
		C.XDefineCursor(dpy, win, cur)
	}

	// A second connection lets background goroutines wake the event loop with
	// a minimal event without sharing the main display across threads.
	wakeDpy = C.XOpenDisplay(nil)
	if wakeDpy == nil {
		fmt.Fprintln(os.Stderr, "canvas: no secondary display, background work wakes on the next event only")
	} else {
		wakeWin = win
		wakeAtom = C.XInternAtom(wakeDpy, C.CString("CANVAS_WAKE"), C.False)
	}
	v.startThumbWorkers()

	v.wmDelete = C.XInternAtom(dpy, C.CString("WM_DELETE_WINDOW"), C.False)
	proto := []C.Atom{v.wmDelete}
	C.XSetWMProtocols(dpy, win, (*C.Atom)(unsafe.Pointer(&proto[0])), 1)

	var geom C.XWindowAttributes
	C.XGetWindowAttributes(dpy, win, &geom)
	v.resize(int(geom.width), int(geom.height))

	v.root = os.Getenv("CANVAS_DIR")
	if v.root == "" {
		v.root = desktopDir()
	}
	v.reload(true)

	C.XMapWindow(dpy, win)
	C.XFlush(dpy)

	v.grabSwitcher()
	v.loadWallpaper()

	fmt.Printf("board: showing %s\n", v.root)
	fmt.Println("  dbl-click   open in a real application window")
	fmt.Println("  t / super+r terminal   drag pan   wheel zoom at pointer")
	fmt.Println("  shift+wheel pan left/right      ctrl+wheel  pan up/down")
	fmt.Println("  arrows      pan        + / -       zoom")
	fmt.Println("  f fit       F5 refresh  r recenter 0 zoom 100%  q quit")
	fmt.Println("  ctrl+r      restart the board")
	fmt.Println("  window: drag its title to move, drag the blue grip to scale, click it to raise, × closes")
	fmt.Println("  shift+tab switches between open windows, alt+f4 closes the active one")
	if session {
		fmt.Printf("session: Canvas desktop, apps=%q\n", os.Getenv("CANVAS_SESSION_APPS"))
		v.startSession()
	}

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
		v.drainBackground()
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
		// Child windows report their own size here too; only our window resizes the board.
		if ce.window != v.win {
			v.onChildConfigure(ce)
			return
		}
		w, h := int(ce.width), int(ce.height)
		if w != v.w || h != v.h {
			v.resize(w, h)
			v.redraw = true
		}
	case C.ButtonPress:
		v.onPress((*C.XButtonEvent)(unsafe.Pointer(xev)))
	case C.ButtonRelease:
		v.onRelease((*C.XButtonEvent)(unsafe.Pointer(xev)))
	case C.MotionNotify:
		v.onMotion((*C.XMotionEvent)(unsafe.Pointer(xev)))
	case C.KeyPress:
		v.onKey((*C.XKeyEvent)(unsafe.Pointer(xev)))
	case C.MapNotify:
		me := (*C.XMapEvent)(unsafe.Pointer(xev))
		v.onMap(me.window)
	case C.DestroyNotify:
		de := (*C.XDestroyWindowEvent)(unsafe.Pointer(xev))
		if de.window == v.win {
			C.XCloseDisplay(v.dpy)
			os.Exit(0)
		}
		v.forget(de.window)
	case C.ClientMessage:
		ce := (*C.XClientMessageEvent)(unsafe.Pointer(xev))
		if ce.message_type == v.wmDelete {
			C.XCloseDisplay(v.dpy)
			os.Exit(0)
		}
		if wakeAtom != 0 && ce.message_type == wakeAtom {
			// A background worker queued results; run() drains the channels.
			v.redraw = true
		}
	}
}

func (v *view) onPress(be *C.XButtonEvent) {
	x, y := int(be.x), int(be.y)

	if v.promptOn {
		return
	}
	if v.menuOpen {
		switch be.button {
		case 1:
			if i := v.menuItemAt(x, y); i >= 0 {
				v.runMenuItem(i)
			} else {
				v.menuOpen = false
				v.redraw = true
			}
		case 3:
			v.menuOpen = false
			v.openMenu(x, y)
		}
		return
	}

	switch be.button {
	case 1:
		C.XGrabPointer(v.dpy, v.win, C.False,
			C.ButtonPressMask|C.ButtonReleaseMask|C.PointerMotionMask,
			C.GrabModeAsync, C.GrabModeAsync, 0, 0, C.CurrentTime)
		if i, part := v.winAt(x, y); i >= 0 {
			if i != len(v.wins)-1 {
				v.raiseWindow(v.wins[i].win)
				i = len(v.wins) - 1
			}
			switch part {
			case partClose:
				v.closeWin(i)
				C.XUngrabPointer(v.dpy, C.CurrentTime)
				v.redraw = true
				return
			case partTitle, partGrip:
				v.beginWinDrag(i, part, x, y)
				v.redraw = true
				return
			}
		}
		if i := v.hitTest(x, y); i >= 0 {
			v.pressed = i
			v.pressX, v.pressY = x, y
			ctrl := be.state&C.ControlMask != 0
			shift := be.state&C.ShiftMask != 0
			v.pressAdd = ctrl || shift
			switch {
			case ctrl:
				if v.selset[i] {
					delete(v.selset, i)
				} else {
					v.selset[i] = true
				}
				v.anchor = i
			case shift && v.anchor >= 0:
				v.selectRange(v.anchor, i)
			default:
				if !v.selset[i] || len(v.selset) == 0 {
					v.selset = map[int]bool{i: true}
				}
				v.anchor = i
			}
			v.redraw = true
			return
		}
		// Empty space: start a rubber-band selection.
		v.bandAdd = be.state&(C.ControlMask|C.ShiftMask) != 0
		if !v.bandAdd {
			v.selset = map[int]bool{}
		}
		v.band = true
		v.bandX0, v.bandY0 = x, y
		v.bandX1, v.bandY1 = x, y
		v.redraw = true
	case 2:
		C.XGrabPointer(v.dpy, v.win, C.False,
			C.ButtonPressMask|C.ButtonReleaseMask|C.PointerMotionMask,
			C.GrabModeAsync, C.GrabModeAsync, 0, 0, C.CurrentTime)
		v.dragging = true
		v.lastX, v.lastY = x, y
	case 3:
		v.openMenu(x, y)
	case 4:
		switch {
		case be.state&C.ShiftMask != 0:
			v.ox -= panWheel / v.zoom
		case be.state&C.ControlMask != 0:
			v.oy -= panWheel / v.zoom
		default:
			v.zoomAt(float64(x), float64(y), zWheel)
		}
		v.redraw = true
	case 5:
		switch {
		case be.state&C.ShiftMask != 0:
			v.ox += panWheel / v.zoom
		case be.state&C.ControlMask != 0:
			v.oy += panWheel / v.zoom
		default:
			v.zoomAt(float64(x), float64(y), 1/zWheel)
		}
		v.redraw = true
	}
}

func (v *view) onRelease(be *C.XButtonEvent) {
	if be.button != 1 && be.button != 2 {
		return
	}
	if be.button == 1 {
		if v.iconDrag {
			v.endIconDrag()
			return
		}
		if v.band {
			v.applyBand()
			v.band = false
			C.XUngrabPointer(v.dpy, C.CurrentTime)
			v.redraw = true
			return
		}
		if v.pressed >= 0 {
			i := v.pressed
			v.pressed = -1
			if !v.pressAdd && len(v.selset) > 1 && v.selset[i] {
				v.selset = map[int]bool{i: true}
				v.redraw = true
			}
			now := time.Now()
			if v.lastIdx == i && now.Sub(v.lastTime) < 400*time.Millisecond {
				v.openIcon(i)
				v.lastIdx = -1
			} else {
				v.lastIdx = i
				v.lastTime = now
			}
		}
	}
	v.stopWinDrag()
	v.stopDrag()
}

func (v *view) selectRange(a, b int) {
	if a > b {
		a, b = b, a
	}
	v.selset = map[int]bool{}
	for i := a; i <= b && i < len(v.icons); i++ {
		if i >= 0 {
			v.selset[i] = true
		}
	}
}

func (v *view) applyBand() {
	x0, y0 := minInt(v.bandX0, v.bandX1), minInt(v.bandY0, v.bandY1)
	x1, y1 := maxInt(v.bandX0, v.bandX1), maxInt(v.bandY0, v.bandY1)
	bx0 := v.ox + float64(x0)/v.zoom
	by0 := v.oy + float64(y0)/v.zoom
	bx1 := v.ox + float64(x1)/v.zoom
	by1 := v.oy + float64(y1)/v.zoom
	if !v.bandAdd {
		v.selset = map[int]bool{}
	}
	for i, ic := range v.icons {
		if ic.x+ic.w >= bx0 && ic.x <= bx1 && ic.y+ic.h >= by0 && ic.y <= by1 {
			v.selset[i] = true
			v.anchor = i
		}
	}
}

func (v *view) beginIconDrag() {
	if !v.selset[v.pressed] {
		v.selset = map[int]bool{v.pressed: true}
		v.anchor = v.pressed
	}
	v.iconDrag = true
	v.orig = map[int][2]float64{}
	for i := range v.selset {
		if i >= 0 && i < len(v.icons) {
			v.orig[i] = [2]float64{v.icons[i].x, v.icons[i].y}
		}
	}
	v.dragBX = v.ox + float64(v.pressX)/v.zoom
	v.dragBY = v.oy + float64(v.pressY)/v.zoom
	v.pressed = -1
}

func (v *view) endIconDrag() {
	v.iconDrag = false
	v.orig = nil
	C.XUngrabPointer(v.dpy, C.CurrentTime)
	savePositions(v.root, v.icons)
	v.redraw = true
}

func (v *view) stopDrag() {
	if !v.dragging {
		C.XUngrabPointer(v.dpy, C.CurrentTime)
		return
	}
	v.dragging = false
	C.XUngrabPointer(v.dpy, C.CurrentTime)
}

func (v *view) onMotion(me *C.XMotionEvent) {
	x, y := int(me.x), int(me.y)

	if v.menuOpen {
		if h := v.menuItemAt(x, y); h != v.menuHover {
			v.menuHover = h
			v.redraw = true
		}
		return
	}
	if v.promptOn {
		return
	}

	// Dragging a window's title moves it; dragging its grip scales it.
	if v.winDrag >= 0 || v.winResize >= 0 {
		if me.state&C.Button1Mask == 0 && me.state&C.Button2Mask == 0 {
			v.stopWinDrag()
			return
		}
		bpx := v.ox + float64(x)/v.zoom
		bpy := v.oy + float64(y)/v.zoom
		if v.winDrag >= 0 {
			w := &v.wins[v.winDrag]
			w.bx = v.dragBX + (bpx - (v.ox + float64(v.dragSX)/v.zoom))
			w.by = v.dragBY + (bpy - (v.oy + float64(v.dragSY)/v.zoom))
			v.clampPos(w)
		} else {
			w := &v.wins[v.winResize]
			s := float64(v.dragSW+(x-v.dragSX)) / float64(v.dragSW)
			if s < 0.15 {
				s = 0.15
			}
			if s > 8 {
				s = 8
			}
			w.wp, w.hp = v.dragBW*s, v.dragBH*s
			v.clampWin(w)
		}
		v.placeWindows()
		v.redraw = true
		return
	}

	// A press on an icon becomes a move once the pointer actually moves.
	if v.pressed >= 0 && !v.iconDrag && !v.band {
		if abs(x-v.pressX) > 4 || abs(y-v.pressY) > 4 {
			v.beginIconDrag()
		}
	}
	if v.iconDrag {
		if me.state&C.Button1Mask == 0 {
			v.endIconDrag()
			return
		}
		dx := v.ox + float64(x)/v.zoom - v.dragBX
		dy := v.oy + float64(y)/v.zoom - v.dragBY
		for i, o := range v.orig {
			v.icons[i].x = o[0] + dx
			v.icons[i].y = o[1] + dy
		}
		v.redraw = true
		return
	}
	if v.band {
		v.bandX1, v.bandY1 = x, y
		v.redraw = true
		return
	}
	if !v.dragging {
		return
	}
	if me.state&C.Button2Mask == 0 {
		v.stopDrag()
		return
	}
	dx, dy := float64(x-v.lastX), float64(y-v.lastY)
	// Moving the pointer right drags the board right, so the viewport shifts left.
	v.ox -= dx / v.zoom
	v.oy -= dy / v.zoom
	v.lastX, v.lastY = x, y
	v.redraw = true
}

func (v *view) onKey(ke *C.XKeyEvent) {
	ks := C.XLookupKeysym(ke, 0)
	if v.promptOn {
		v.promptKey(ke, ks)
		return
	}
	if v.menuOpen {
		switch ks {
		case 0xff1b: // Escape
			v.menuOpen = false
		case 0xff52: // Up
			v.moveMenu(-1)
		case 0xff54: // Down
			v.moveMenu(1)
		case 0xff0d: // Return
			if v.menuHover >= 0 {
				v.runMenuItem(v.menuHover)
			}
		default:
			return
		}
		v.redraw = true
		return
	}
	shift := ke.state&C.ShiftMask != 0
	if ke.state&C.ControlMask != 0 {
		switch ks {
		case 'c', 'C':
			v.copySelection()
			return
		case 'x', 'X':
			v.cutSelection()
			return
		case 'v', 'V':
			v.paste()
			return
		}
	}
	if ks == 'r' || ks == 'R' {
		if ke.state&C.Mod4Mask != 0 { // Super+R: open a terminal (kitty first)
			v.launchTerminal()
			v.redraw = true
			return
		}
		if ke.state&C.ControlMask != 0 { // Ctrl+R: restart the board in place
			v.restart()
			return
		}
	}
	cx, cy := float64(v.w)/2, float64(v.h)/2
	switch ks {
	case 0xff1b: // Escape clears the selection, or leaves the board if empty
		if len(v.selset) > 0 {
			v.selset = map[int]bool{}
			v.anchor = -1
			break
		}
		C.XCloseDisplay(v.dpy)
		os.Exit(0)
	case 'q':
		C.XCloseDisplay(v.dpy)
		os.Exit(0)
	case 0xffff: // Delete: move the selection to the trash
		v.deleteSelection()
	case 0xffbf: // F2: rename
		v.renameSelection()
	case 0xff50: // Home
		v.centerOrigin()
	case 'r', 'R':
		if shift {
			v.reload(false)
		} else {
			v.centerOrigin()
		}
	case 0xffc2: // F5
		v.reload(false)
	case 'f':
		v.fit()
	case 't':
		v.launchTerminal()
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
	case 0xff09, 0xfe20: // Tab / Shift+Tab: switch between open windows
		v.cycleWindows(shift)
	case 0xffc1: // F4 with Alt: close the active window
		if ke.state&C.Mod1Mask != 0 {
			v.closeActive()
		}
	default:
		return
	}
	v.redraw = true
}

const (
	menuRow = 24
	menuSep = 8
)

func (v *view) openMenu(x, y int) {
	idx := v.hitTest(x, y)
	if idx >= 0 && !v.selset[idx] {
		v.selset = map[int]bool{idx: true}
		v.anchor = idx
	}
	var items []menuItem
	if idx >= 0 {
		i := idx
		items = append(items, menuItem{label: "Открыть", do: func() { v.openIcon(i) }})
		items = append(items, menuItem{sep: true})
		items = append(items, menuItem{label: "Вырезать", do: v.cutSelection})
		items = append(items, menuItem{label: "Копировать", do: v.copySelection})
		items = append(items, menuItem{label: "Удалить", do: v.deleteSelection})
		items = append(items, menuItem{sep: true})
		items = append(items, menuItem{label: "Сжать в ZIP", do: v.zipSelection})
		items = append(items, v.conversionItems()...)
		if p, e := v.icons[idx].path, strings.ToLower(filepath.Ext(v.icons[idx].path)); !v.icons[idx].isDir && (isImage(e) || isVideo(e) || e == ".gif") {
			items = append(items, menuItem{label: "Сделать обоями", do: func() { v.setWallpaper(p) }})
		}
	} else {
		items = append(items, menuItem{label: "Новая папка…", do: func() {
			v.startPrompt("Новая папка", "Новая папка", func(n string) { v.createPath(n, false) })
		}})
		items = append(items, menuItem{label: "Новый файл…", do: func() {
			v.startPrompt("Новый файл", "Новый файл.txt", func(n string) { v.createPath(n, true) })
		}})
		items = append(items, menuItem{sep: true})
		if len(v.clip) > 0 {
			items = append(items, menuItem{label: "Вставить", do: v.paste})
		}
		items = append(items, menuItem{label: "Открыть терминал", do: v.launchTerminal})
		items = append(items, menuItem{sep: true})
		items = append(items, menuItem{label: "Обновить", do: func() { v.reload(false) }})
		if v.wall != nil {
			items = append(items, menuItem{sep: true})
			items = append(items, menuItem{label: "Убрать обои", do: func() { v.clearWallpaper("Обои убраны") }})
		}
	}
	v.menu = items
	v.menuOpen = true
	v.menuHover = -1
	v.menuX, v.menuY = x, y
	v.menuW = 0
	for _, it := range items {
		if !it.sep {
			if w := v.textWidth(it.label) + 30; w > v.menuW {
				v.menuW = w
			}
		}
	}
	if v.menuW < 170 {
		v.menuW = 170
	}
	_, _, _, h := v.menuGeom()
	if v.menuX+v.menuW > v.w {
		v.menuX = v.w - v.menuW - 4
	}
	if v.menuX < 4 {
		v.menuX = 4
	}
	if v.menuY+h > v.h {
		v.menuY = v.h - h - 4
	}
	if v.menuY < 4 {
		v.menuY = 4
	}
	v.redraw = true
}

func (v *view) menuGeom() (int, int, int, int) {
	h := 6
	for _, it := range v.menu {
		if it.sep {
			h += menuSep
		} else {
			h += menuRow
		}
	}
	return v.menuX, v.menuY, v.menuW, h
}

func (v *view) menuItemAt(mx, my int) int {
	x, y, w, h := v.menuGeom()
	if mx < x || my < y || mx >= x+w || my >= y+h {
		return -1
	}
	cy := y + 3
	for i, it := range v.menu {
		hh := menuRow
		if it.sep {
			hh = menuSep
		}
		if my >= cy && my < cy+hh {
			if it.sep {
				return -1
			}
			return i
		}
		cy += hh
	}
	return -1
}

func (v *view) runMenuItem(i int) {
	if i < 0 || i >= len(v.menu) {
		return
	}
	do := v.menu[i].do
	v.menuOpen = false
	v.menuHover = -1
	v.redraw = true
	if do != nil {
		do()
	}
}

func (v *view) moveMenu(dir int) {
	n := len(v.menu)
	if n == 0 {
		return
	}
	i := v.menuHover
	for k := 0; k < n; k++ {
		i = (i + dir + n) % n
		if !v.menu[i].sep {
			v.menuHover = i
			return
		}
	}
}

func (v *view) drawMenu() {
	if !v.menuOpen {
		return
	}
	x, y, w, h := v.menuGeom()
	v.fill(x, y, w, h, menuBg)
	v.fg(menuBorder)
	C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(x), C.int(y),
		C.uint(w-1), C.uint(h-1))
	cy := y + 3
	for i, it := range v.menu {
		if it.sep {
			v.fg(menuBorder)
			C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x+10), C.int(cy+menuSep/2),
				C.int(x+w-10), C.int(cy+menuSep/2))
			cy += menuSep
			continue
		}
		if i == v.menuHover {
			v.fill(x+2, cy, w-4, menuRow, selBg)
		}
		v.text(x+16, cy+16, it.label, textCol)
		cy += menuRow
	}
}

func (v *view) startPrompt(title, initial string, cb func(string)) {
	v.promptOn = true
	v.promptTitle = title
	v.promptText = initial
	v.promptFresh = true
	v.promptCb = cb
	v.menuOpen = false
	v.redraw = true
}

func (v *view) promptKey(ke *C.XKeyEvent, ks C.KeySym) {
	switch ks {
	case 0xff1b: // Escape
		v.promptOn = false
		v.promptCb = nil
		v.redraw = true
		return
	case 0xff0d: // Return
		text := strings.TrimSpace(v.promptText)
		cb := v.promptCb
		v.promptOn = false
		v.promptCb = nil
		if text != "" && cb != nil {
			cb(text)
		}
		v.redraw = true
		return
	case 0xff08: // Backspace
		r := []rune(v.promptText)
		if len(r) > 0 {
			r = r[:len(r)-1]
		}
		v.promptText = string(r)
		v.promptFresh = false
		v.redraw = true
		return
	}

	var buf [32]C.char
	var ksym C.KeySym
	n := C.XLookupString(ke, &buf[0], 32, &ksym, nil)
	if n <= 0 {
		return
	}
	s := C.GoStringN(&buf[0], n)
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return
		}
	}
	if v.promptFresh {
		v.promptText = ""
		v.promptFresh = false
	}
	v.promptText += s
	v.redraw = true
}

func (v *view) drawPrompt() {
	if !v.promptOn {
		return
	}
	w, h := 460, 118
	if w > v.w-40 {
		w = v.w - 40
	}
	x := (v.w - w) / 2
	y := (v.h - h) / 2
	v.fill(x, y, w, h, menuBg)
	v.fg(menuBorder)
	C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(x), C.int(y),
		C.uint(w-1), C.uint(h-1))
	v.text(x+18, y+28, v.promptTitle, textCol)
	ix, iy, iw, ih := x+18, y+44, w-36, 30
	v.fill(ix, iy, iw, ih, panelBg)
	v.fg(selBorder)
	C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(ix), C.int(iy),
		C.uint(iw-1), C.uint(ih-1))
	v.text(ix+8, iy+20, v.promptText, textCol)
	cx := ix + 8 + v.textWidth(v.promptText) + 1
	v.fg(textCol)
	C.XDrawLine(v.dpy, v.pm, v.gc, C.int(cx), C.int(iy+6), C.int(cx), C.int(iy+ih-6))
	v.text(x+18, y+h-16, "Enter — OK, Esc — отмена", hudCol)
}

func (v *view) drawBand() {
	if !v.band {
		return
	}
	x0, y0 := minInt(v.bandX0, v.bandX1), minInt(v.bandY0, v.bandY1)
	x1, y1 := maxInt(v.bandX0, v.bandX1), maxInt(v.bandY0, v.bandY1)
	v.fg(selBorder)
	C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(x0), C.int(y0),
		C.uint(maxInt(x1-x0, 1)), C.uint(maxInt(y1-y0, 1)))
}

func (v *view) drawStatus() {
	if v.status == "" {
		return
	}
	w := v.textWidth(v.status) + 20
	x := (v.w - w) / 2
	v.fill(x, v.h-56, w, 24, menuBg)
	v.fg(menuBorder)
	C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(x), C.int(v.h-56),
		C.uint(w-1), C.uint(23))
	v.text(x+10, v.h-40, v.status, textCol)
}

func (v *view) selectedIdx() []int {
	out := make([]int, 0, len(v.selset))
	for i := range v.selset {
		if i >= 0 && i < len(v.icons) {
			out = append(out, i)
		}
	}
	sort.Ints(out)
	return out
}

func (v *view) selectedPaths() []string {
	idx := v.selectedIdx()
	out := make([]string, 0, len(idx))
	for _, i := range idx {
		out = append(out, v.icons[i].path)
	}
	return out
}

func (v *view) copySelection() {
	p := v.selectedPaths()
	if len(p) == 0 {
		return
	}
	v.clip = append([]string(nil), p...)
	v.clipCut = false
	v.status = fmt.Sprintf("Скопировано: %d", len(p))
	v.redraw = true
}

func (v *view) cutSelection() {
	p := v.selectedPaths()
	if len(p) == 0 {
		return
	}
	v.clip = append([]string(nil), p...)
	v.clipCut = true
	v.status = fmt.Sprintf("Вырезано: %d", len(p))
	v.redraw = true
}

func (v *view) paste() {
	if len(v.clip) == 0 {
		return
	}
	n := 0
	for _, src := range v.clip {
		dst := filepath.Join(v.root, filepath.Base(src))
		if v.clipCut {
			if filepath.Dir(src) == filepath.Clean(v.root) {
				continue
			}
			if exists(dst) {
				dst = uniquePath(dst)
			}
			if err := os.Rename(src, dst); err != nil {
				if err := copyPath(src, dst); err != nil {
					v.status = err.Error()
					continue
				}
				os.RemoveAll(src)
			}
		} else {
			if exists(dst) {
				dst = uniquePath(dst)
			}
			if err := copyPath(src, dst); err != nil {
				v.status = err.Error()
				continue
			}
		}
		n++
	}
	if v.clipCut && n > 0 {
		v.clip = nil
	}
	v.status = fmt.Sprintf("Вставлено: %d", n)
	v.reload(false)
}

func (v *view) createPath(name string, file bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if strings.ContainsAny(name, "/") {
		name = filepath.Base(name)
	}
	p := filepath.Join(v.root, name)
	if exists(p) {
		v.status = "Уже существует: " + name
		v.redraw = true
		return
	}
	if file {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			v.status = err.Error()
			v.redraw = true
			return
		}
		f.Close()
	} else if err := os.Mkdir(p, 0o755); err != nil {
		v.status = err.Error()
		v.redraw = true
		return
	}
	v.status = "Создано: " + name
	v.reload(false)
}

func (v *view) renameSelection() {
	idx := v.selectedIdx()
	if len(idx) != 1 {
		return
	}
	ic := v.icons[idx[0]]
	v.startPrompt("Переименовать", filepath.Base(ic.path), func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || strings.Contains(name, "/") {
			return
		}
		dst := filepath.Join(v.root, name)
		if dst == ic.path {
			return
		}
		if exists(dst) {
			v.status = "Уже существует: " + name
			v.redraw = true
			return
		}
		if err := os.Rename(ic.path, dst); err != nil {
			v.status = err.Error()
		} else {
			v.status = "Переименовано: " + name
		}
		v.reload(false)
	})
}

func (v *view) deleteSelection() {
	paths := v.selectedPaths()
	if len(paths) == 0 {
		return
	}
	home, _ := os.UserHomeDir()
	trash := filepath.Join(home, ".local", "share", "Trash")
	filesDir := filepath.Join(trash, "files")
	infoDir := filepath.Join(trash, "info")
	os.MkdirAll(filesDir, 0o755)
	os.MkdirAll(infoDir, 0o755)

	n := 0
	for _, p := range paths {
		name := filepath.Base(p)
		target := filepath.Join(filesDir, name)
		for i := 1; exists(target); i++ {
			target = filepath.Join(filesDir, fmt.Sprintf("%s.%d", name, i))
		}
		if err := os.Rename(p, target); err != nil {
			if err := copyPath(p, target); err != nil {
				v.status = err.Error()
				continue
			}
			os.RemoveAll(p)
		}
		info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
			p, time.Now().Format("2006-01-02T15:04:05"))
		os.WriteFile(filepath.Join(infoDir, filepath.Base(target)+".trashinfo"),
			[]byte(info), 0o644)
		n++
	}
	v.selset = map[int]bool{}
	v.anchor = -1
	v.status = fmt.Sprintf("В корзину: %d", n)
	v.reload(false)
}

func (v *view) zipSelection() {
	paths := v.selectedPaths()
	if len(paths) == 0 {
		return
	}
	base := "Архив"
	if len(paths) == 1 {
		base = strings.TrimSuffix(filepath.Base(paths[0]), filepath.Ext(paths[0]))
	}
	archive := uniquePath(filepath.Join(v.root, base+".zip"))
	args := []string{"-r", "-q", archive}
	for _, p := range paths {
		args = append(args, filepath.Base(p))
	}
	cmd := exec.Command("zip", args...)
	cmd.Dir = v.root
	if out, err := cmd.CombinedOutput(); err != nil {
		v.status = "zip: " + strings.TrimSpace(string(out))
	} else {
		v.status = "ZIP: " + filepath.Base(archive)
	}
	v.reload(false)
}

func (v *view) conversionItems() []menuItem {
	img, vid, gif := false, false, false
	for _, p := range v.selectedPaths() {
		switch strings.ToLower(filepath.Ext(p)) {
		case ".jpg", ".jpeg", ".png", ".webp", ".bmp", ".tif", ".tiff":
			img = true
		case ".mp4", ".mkv", ".webm", ".mov", ".avi", ".m4v":
			vid = true
		case ".gif":
			gif = true
		}
	}
	var items []menuItem
	add := func(label, kind string) {
		k := kind
		items = append(items, menuItem{label: label, do: func() { v.convertSelection(k) }})
	}
	if img {
		add("Фото в JPEG", "jpeg")
		add("Фото в PNG", "png")
		add("Фото в WebP", "webp")
	}
	if vid {
		add("Видео в GIF", "gif")
		add("Видео в MP3", "mp3")
	}
	if gif {
		add("GIF в MP4", "mp4")
		add("GIF в WebP", "gifwebp")
	}
	if len(items) > 0 {
		items = append([]menuItem{{sep: true}}, items...)
	}
	return items
}

func (v *view) convertSelection(kind string) {
	n := 0
	for _, p := range v.selectedPaths() {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(p))
		stem := strings.TrimSuffix(p, filepath.Ext(p))
		var out string
		var cmd *exec.Cmd
		switch kind {
		case "jpeg", "png", "webp":
			if !isImage(ext) {
				continue
			}
			targetExt := ".jpg"
			if kind == "png" {
				targetExt = ".png"
			} else if kind == "webp" {
				targetExt = ".webp"
			}
			if ext == targetExt || (kind == "jpeg" && ext == ".jpeg") {
				continue
			}
			out = uniquePath(stem + targetExt)
			cmd = exec.Command("magick", p, "-auto-orient", out)
		case "gif":
			if !isVideo(ext) {
				continue
			}
			out = uniquePath(stem + ".gif")
			cmd = exec.Command("ffmpeg", "-y", "-loglevel", "error", "-i", p,
				"-vf", "fps=12,scale=480:-2:flags=lanczos", out)
		case "mp3":
			if !isVideo(ext) {
				continue
			}
			out = uniquePath(stem + ".mp3")
			cmd = exec.Command("ffmpeg", "-y", "-loglevel", "error", "-i", p,
				"-vn", "-q:a", "2", out)
		case "mp4":
			if ext != ".gif" {
				continue
			}
			out = uniquePath(stem + ".mp4")
			cmd = exec.Command("ffmpeg", "-y", "-loglevel", "error", "-i", p,
				"-movflags", "+faststart", "-pix_fmt", "yuv420p",
				"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", out)
		case "gifwebp":
			if ext != ".gif" {
				continue
			}
			out = uniquePath(stem + ".webp")
			cmd = exec.Command("ffmpeg", "-y", "-loglevel", "error", "-i", p,
				"-loop", "0", "-c:v", "libwebp", "-q:v", "80", out)
		}
		if cmd == nil {
			continue
		}
		if err := cmd.Run(); err != nil {
			v.status = "Ошибка: " + filepath.Base(p)
			continue
		}
		n++
	}
	v.status = fmt.Sprintf("Готово: %d", n)
	v.reload(false)
}

func isImage(ext string) bool {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".bmp", ".tif", ".tiff":
		return true
	}
	return false
}

func isVideo(ext string) bool {
	switch ext {
	case ".mp4", ".mkv", ".webm", ".mov", ".avi", ".m4v":
		return true
	}
	return false
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func uniquePath(p string) string {
	if !exists(p) {
		return p
	}
	ext := filepath.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	for i := 1; ; i++ {
		q := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !exists(q) {
			return q
		}
	}
}

func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst)
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		t := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(t, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(l, t)
		}
		return copyFile(p, t)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (v *view) closeActive() {
	if n := len(v.wins); n > 0 {
		v.closeWin(n - 1)
	}
}

func (v *view) hitTest(sx, sy int) int {
	bx := v.ox + float64(sx)/v.zoom
	by := v.oy + float64(sy)/v.zoom
	for i, ic := range v.icons {
		if bx >= ic.x && bx < ic.x+ic.w && by >= ic.y && by < ic.y+ic.h {
			return i
		}
	}
	return -1
}

func (v *view) openIcon(i int) {
	if i < 0 || i >= len(v.icons) {
		return
	}
	ic := v.icons[i]
	if ic.isApp {
		v.launchDesktop(ic.path, ic.x+ic.w/2, ic.y+ic.h/2)
		return
	}
	v.launch(ic.path, ic.isDir, ic.x+ic.w/2, ic.y+ic.h/2)
}

func (v *view) reload(reset bool) {
	v.selset = map[int]bool{}
	v.anchor = -1
	v.pressed = -1
	v.lastIdx = -1

	ents, err := os.ReadDir(v.root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "canvas: readdir:", err)
	}
	pos := loadPositions(v.root)
	var dirs, files, apps []boardIcon
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(v.root, name)
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(name), ".desktop") {
			if ic, ok := desktopIcon(path, name); ok {
				apps = append(apps, ic)
				continue
			}
		}
		ic := boardIcon{name: name, path: path, isDir: e.IsDir(), label: name}
		if ic.isDir {
			ic.col = folderCol
			dirs = append(dirs, ic)
		} else {
			ic.col = fileColor(name)
			files = append(files, ic)
		}
	}
	less := func(s []boardIcon) {
		sort.SliceStable(s, func(i, j int) bool {
			return strings.ToLower(s[i].label) < strings.ToLower(s[j].label)
		})
	}
	less(dirs)
	less(files)
	less(apps)
	v.icons = append(append(dirs, files...), apps...)
	v.layout()
	for i := range v.icons {
		if p, ok := pos[v.icons[i].path]; ok {
			v.icons[i].x, v.icons[i].y = p[0], p[1]
		}
	}

	if reset {
		v.fit()
	}
	v.requestThumbs()
	v.redraw = true
}

// desktopIcon parses a .desktop launcher into a board icon. Launchers that are
// hidden or not applications are skipped.
func desktopIcon(path, name string) (boardIcon, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return boardIcon{}, false
	}
	inEntry := false
	typ, display, execLine := "", "", ""
	hidden, nodisplay := false, false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry || strings.HasPrefix(line, "#") {
			continue
		}
		k, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Type":
			typ = val
		case "Name":
			if display == "" {
				display = val
			}
		case "Exec":
			execLine = val
		case "Hidden":
			hidden = val == "true"
		case "NoDisplay":
			nodisplay = val == "true"
		}
	}
	if typ != "Application" || execLine == "" || hidden || nodisplay {
		return boardIcon{}, false
	}
	if display == "" {
		display = strings.TrimSuffix(name, ".desktop")
	}
	return boardIcon{
		name:  name,
		path:  path,
		isApp: true,
		exec:  execLine,
		label: display,
		col:   selBorder,
	}, true
}

// launchDesktop runs a .desktop file through gio so its Env, Path and field
// codes are handled the same way a desktop environment would.
func (v *view) launchDesktop(path string, cx, cy float64) {
	if gio, err := exec.LookPath("gio"); err == nil {
		v.spawn([]string{gio, "launch", path}, cx, cy)
		return
	}
	// Fallback: run the Exec line ourselves.
	ic := boardIcon{}
	for _, it := range v.icons {
		if it.path == path {
			ic = it
			break
		}
	}
	cmd := strings.Fields(ic.exec)
	argv := make([]string, 0, len(cmd))
	for _, a := range cmd {
		if strings.HasPrefix(a, "%") {
			continue
		}
		argv = append(argv, a)
	}
	if len(argv) > 0 {
		v.spawn(argv, cx, cy)
	}
}

// positionsPath stores per-folder icon coordinates, keyed by absolute path so
// renaming or moving a file simply forgets its old position.
func positionsPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "canvas", "positions.json")
}

func loadPositions(root string) map[string][2]float64 {
	out := map[string][2]float64{}
	p := positionsPath()
	if p == "" {
		return out
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return out
	}
	all := map[string]map[string][2]float64{}
	if json.Unmarshal(data, &all) != nil {
		return out
	}
	for k, v := range all[root] {
		out[k] = v
	}
	return out
}

func savePositions(root string, icons []boardIcon) {
	p := positionsPath()
	if p == "" {
		return
	}
	all := map[string]map[string][2]float64{}
	if data, err := os.ReadFile(p); err == nil {
		json.Unmarshal(data, &all)
	}
	pos := map[string][2]float64{}
	for _, ic := range icons {
		pos[ic.path] = [2]float64{ic.x, ic.y}
	}
	all[root] = pos
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	os.WriteFile(p, data, 0o644)
}

// launch starts a real application and remembers where on the board its first
// mapped top-level window should be adopted.
func (v *view) launch(target string, isDir bool, cx, cy float64) {
	v.spawn(appCommand(target, isDir), cx, cy)
}

func (v *view) launchTerminal() {
	cx := v.ox + float64(v.w)/(2*v.zoom)
	cy := v.oy + float64(v.h)/(2*v.zoom)
	var argv []string
	if t := os.Getenv("CANVAS_TERMINAL"); t != "" {
		argv = strings.Fields(t)
	} else {
		argv = terminalCommand()
	}
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "canvas: no terminal found")
		return
	}
	v.spawn(argv, cx, cy)
}

// terminalCommand picks the first terminal emulator present on the system.
func terminalCommand() []string {
	for _, n := range []string{"kitty", "foot", "alacritty", "xterm", "konsole", "xfce4-terminal"} {
		if p, err := exec.LookPath(n); err == nil {
			return []string{p}
		}
	}
	return nil
}

// spawn runs an application and remembers the board point where its first
// mapped top-level window should be adopted.
func (v *view) spawn(argv []string, cx, cy float64) {
	if len(argv) == 0 {
		return
	}
	v.pending = &pendingLaunch{
		cx: cx, cy: cy,
		before: v.rootChildren(),
		until:  time.Now().Add(10 * time.Second),
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = v.root
	if err := cmd.Start(); err != nil {
		v.pending = nil
		fmt.Fprintln(os.Stderr, "canvas: launch:", err)
		return
	}
	go func() { _ = cmd.Wait() }()
	fmt.Printf("canvas: launched %s\n", strings.Join(argv, " "))
}

// startSession fills a freshly started desktop session: CANVAS_SESSION_APPS
// lists the programs to open (one per ';'), and with no list a terminal opens
// so the board is immediately usable.
func (v *view) startSession() {
	apps := strings.TrimSpace(os.Getenv("CANVAS_SESSION_APPS"))
	if apps == "" {
		v.launchTerminal()
		return
	}
	for _, line := range strings.Split(apps, ";") {
		argv := strings.Fields(line)
		if len(argv) == 0 {
			continue
		}
		cx := v.ox + float64(v.w)/(2*v.zoom)
		cy := v.oy + float64(v.h)/(2*v.zoom)
		v.spawn(argv, cx, cy)
	}
}

// restart relaunches the board as a fresh process with the same arguments and
// environment. The current connection is closed first, which tears down the
// board window together with any adopted child windows, so the new instance
// starts from a clean slate.
func (v *view) restart() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "canvas: restart:", err)
		return
	}
	C.XCloseDisplay(v.dpy)
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = os.Environ()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "canvas: restart:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// appCommand prefers a real file manager for folders; CANVAS_FILEMANAGER can
// override it (useful for testing without a desktop session).
func appCommand(target string, isDir bool) []string {
	if isDir {
		if fm := os.Getenv("CANVAS_FILEMANAGER"); fm != "" {
			return append(strings.Fields(fm), target)
		}
		for _, n := range []string{"dolphin", "thunar", "nautilus", "pcmanfm", "nemo"} {
			if p, err := exec.LookPath(n); err == nil {
				if n == "dolphin" {
					// Force a fresh window, otherwise Dolphin may forward the
					// URL to an already running instance and no window is mapped.
					return []string{p, "--new-window", target}
				}
				return []string{p, target}
			}
		}
	}
	return []string{"xdg-open", target}
}

// keyboardConfig reads the XKB setup for a board session. An empty
// CANVAS_XKB_LAYOUT disables it; the defaults give US + Russian with two
// common toggle keys.
func keyboardConfig() (layout, variant, options string) {
	layout = os.Getenv("CANVAS_XKB_LAYOUT")
	if layout == "" {
		layout = "us,ru"
	}
	variant = os.Getenv("CANVAS_XKB_VARIANT")
	options = os.Getenv("CANVAS_XKB_OPTIONS")
	if options == "" {
		options = "grp:alt_shift_toggle,grp:win_space_toggle"
	}
	return
}

// applyKeyboard installs the layouts with setxkbmap so the session can switch
// between them. It is best-effort: a missing tool or layout is only reported.
func applyKeyboard(layout, variant, options string) bool {
	if layout == "" {
		return false
	}
	bin, err := exec.LookPath("setxkbmap")
	if err != nil {
		fmt.Fprintln(os.Stderr, "canvas: setxkbmap not found, keyboard layouts left alone")
		return false
	}
	args := []string{"-layout", layout}
	if variant != "" {
		args = append(args, "-variant", variant)
	}
	if options != "" {
		args = append(args, "-option", options)
	}
	if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "canvas: setxkbmap: %v: %s\n", err, strings.TrimSpace(string(out)))
		return false
	}
	fmt.Printf("canvas: keyboard layouts %s (%s)\n", layout, options)
	return true
}

func splitLayouts(layout string) []string {
	var out []string
	for _, s := range strings.Split(layout, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// currentGroup is the active XKB group, i.e. the index of the layout in use.
func (v *view) currentGroup() int {
	var st C.XkbStateRec
	if C.XkbGetState(v.dpy, C.uint(C.XkbUseCoreKbd), &st) != 0 {
		return 0
	}
	return int(st.group)
}

func (v *view) rootChildren() map[C.Window]bool {
	m := map[C.Window]bool{}
	var rr, pr C.Window
	var children *C.Window
	var n C.uint
	if C.XQueryTree(v.dpy, C.XDefaultRootWindow(v.dpy), &rr, &pr, &children, &n) == 0 {
		return m
	}
	if children != nil {
		for _, w := range unsafe.Slice(children, int(n)) {
			m[w] = true
		}
		C.XFree(unsafe.Pointer(children))
	}
	return m
}

func (v *view) onMap(win C.Window) {
	p := v.pending
	if p == nil || win == v.win || p.before[win] || time.Now().After(p.until) {
		return
	}
	v.pending = nil
	v.adopt(win, p.cx, p.cy)
}

// maxWinSize is the largest on-screen size a managed window may take. It is
// deliberately smaller than the viewport: applications without a window
// manager often open "maximised", and a window covering the whole board would
// hide its own title bar and the close button, leaving no free board to pan on.
func (v *view) maxWinSize() (w, h float64) {
	w = float64(v.w) - 2*winGrip - 40
	h = float64(v.h) - 2*(winTitleH+winGrip)
	if w < 80 {
		w = 80
	}
	if h < 60 {
		h = 60
	}
	return
}

// clampWin caps a window's size and keeps it inside the viewport, so its title
// bar and close button always stay reachable.
func (v *view) clampWin(w *managedWin) {
	mw, mh := v.maxWinSize()
	if w.wp > mw {
		w.wp = mw
	}
	if w.hp > mh {
		w.hp = mh
	}
	v.clampPos(w)
}

// clampPos nudges a window back so its frame stays on screen.
func (v *view) clampPos(w *managedWin) {
	sw, sh := int(math.Round(w.wp)), int(math.Round(w.hp))
	x, y := v.sx(w.bx), v.sy(w.by)
	maxX := v.w - sw - winGrip
	if maxX < 8 {
		maxX = 8
	}
	maxY := v.h - sh - winGrip - 8
	if maxY < winTitleH+8 {
		maxY = winTitleH + 8
	}
	if x < 8 {
		x = 8
	}
	if x > maxX {
		x = maxX
	}
	if y < winTitleH+8 {
		y = winTitleH + 8
	}
	if y > maxY {
		y = maxY
	}
	w.bx = v.ox + float64(x)/v.zoom
	w.by = v.oy + float64(y)/v.zoom
}

// onChildConfigure reacts to a managed window resizing itself. Left unchecked,
// an application that asks for the full screen would escape the frame.
func (v *view) onChildConfigure(ce *C.XConfigureEvent) {
	for i := range v.wins {
		w := &v.wins[i]
		if w.win != ce.window {
			continue
		}
		cw, ch := int(ce.width), int(ce.height)
		if cw <= 0 || ch <= 0 || (cw == w.lastW && ch == w.lastH) {
			return // a resize we made ourselves
		}
		w.wp, w.hp = float64(cw), float64(ch)
		v.clampWin(w)
		v.placeWindows()
		v.redraw = true
		return
	}
}

// adopt reparents a freshly launched top-level window into the canvas and
// pins it to a board position.
func (v *view) adopt(win C.Window, cx, cy float64) {
	var attr C.XWindowAttributes
	if C.XGetWindowAttributes(v.dpy, win, &attr) == 0 {
		return
	}
	if attr.override_redirect != 0 || attr.width == 0 || attr.height == 0 {
		return
	}
	bw := float64(attr.width)
	bh := float64(attr.height)
	C.XReparentWindow(v.dpy, win, v.win, 0, 0)
	C.XSetWindowBorderWidth(v.dpy, win, 0)
	C.XMapWindow(v.dpy, win)
	v.wins = append(v.wins, managedWin{
		win:   win,
		title: windowTitle(v.dpy, win),
		bx:    cx - (bw/2)/v.zoom,
		by:    cy - (bh/2)/v.zoom,
		wp:    bw,
		hp:    bh,
		lastX: -1 << 30, lastY: -1 << 30, lastW: -1, lastH: -1,
	})
	v.clampWin(&v.wins[len(v.wins)-1])
	v.placeWindows()
	v.redraw = true
	fmt.Printf("canvas: adopted window 0x%x %dx%d\n", uint64(win), attr.width, attr.height)
}

func (v *view) forget(win C.Window) {
	for i := range v.wins {
		if v.wins[i].win == win {
			v.wins = append(v.wins[:i], v.wins[i+1:]...)
			v.redraw = true
			return
		}
	}
}

// raiseWindow brings a managed window to the front, both in X's stacking order
// and at the end of the slice (the topmost window is the last one).
func (v *view) raiseWindow(win C.Window) {
	for i := range v.wins {
		if v.wins[i].win != win {
			continue
		}
		if i == len(v.wins)-1 {
			return
		}
		w := v.wins[i]
		copy(v.wins[i:], v.wins[i+1:])
		v.wins[len(v.wins)-1] = w
		C.XRaiseWindow(v.dpy, win)
		C.XFlush(v.dpy)
		v.redraw = true
		return
	}
}

// winRect is the on-screen rectangle of a managed window's content, with the
// same minimum size the window itself is given. Frames are built around it so
// the drawn bar, the grip and the real child window never disagree.
func (v *view) winRect(w *managedWin) (x, y, sw, sh int) {
	x = v.sx(w.bx)
	y = v.sy(w.by)
	sw = int(math.Round(w.wp))
	sh = int(math.Round(w.hp))
	if sw < 80 {
		sw = 80
	}
	if sh < 60 {
		sh = 60
	}
	return
}

func (v *view) titlePx() int {
	return winTitleH
}

func (v *view) gripPx() int {
	return winGrip
}

// placeWindows keeps every managed window pinned to its board position. Sizes
// are left alone: the viewport zoom only moves windows, never resizes them.
func (v *view) placeWindows() {
	changed := false
	for i := range v.wins {
		w := &v.wins[i]
		x, y, sw, sh := v.winRect(w)
		if x == w.lastX && y == w.lastY && sw == w.lastW && sh == w.lastH {
			continue
		}
		C.XMoveResizeWindow(v.dpy, w.win, C.int(x), C.int(y), C.uint(sw), C.uint(sh))
		w.lastX, w.lastY, w.lastW, w.lastH = x, y, sw, sh
		changed = true
	}
	if changed {
		C.XFlush(v.dpy)
	}
}

// windowTitle reads a window's WM_NAME, if it has one.
func windowTitle(dpy *C.Display, win C.Window) string {
	var name *C.char
	if C.XFetchName(dpy, win, &name) != 0 && name != nil {
		s := C.GoString(name)
		C.XFree(unsafe.Pointer(name))
		return s
	}
	return ""
}

// Parts of a managed window's frame, in the board's own coordinate space.
const (
	partNone  = 0
	partTitle = 1 // drag here to move the window
	partGrip  = 2 // drag here to scale the window
	partClose = 3 // click here to close the window
)

// winAt reports which frame part of which window lies under a screen point,
// searching from the topmost window down. Only the title bar, the close button
// and the grip can be hit: the content area belongs to the application.
func (v *view) winAt(x, y int) (int, int) {
	th, g := v.titlePx(), v.gripPx()
	for i := len(v.wins) - 1; i >= 0; i-- {
		x0, y0, sw, sh := v.winRect(&v.wins[i])
		if x >= x0 && x < x0+sw && y >= y0-th && y < y0 {
			if x >= x0+sw-th {
				return i, partClose
			}
			return i, partTitle
		}
		if x >= x0+sw-g && x < x0+sw && y >= y0+sh && y < y0+sh+g {
			return i, partGrip
		}
	}
	return -1, partNone
}

// beginWinDrag records the geometry a window interaction starts from.
func (v *view) beginWinDrag(i, part, x, y int) {
	w := &v.wins[i]
	v.dragSX, v.dragSY = x, y
	v.dragBX, v.dragBY = w.bx, w.by
	v.dragBW, v.dragBH = w.wp, w.hp
	_, _, v.dragSW, v.dragSH = v.winRect(w)
	switch part {
	case partTitle:
		v.winDrag = i
	case partGrip:
		v.winResize = i
	}
}

func (v *view) stopWinDrag() {
	v.winDrag, v.winResize = -1, -1
}

// closeWin asks an application to close its window: politely through
// WM_DELETE_WINDOW when it advertises the protocol, forcibly otherwise.
func (v *view) closeWin(i int) {
	w := &v.wins[i]
	del := C.XInternAtom(v.dpy, C.CString("WM_DELETE_WINDOW"), C.False)
	var protos *C.Atom
	var n C.int
	if C.XGetWMProtocols(v.dpy, w.win, &protos, &n) != 0 && protos != nil {
		supported := false
		for _, a := range unsafe.Slice(protos, int(n)) {
			if a == del {
				supported = true
				break
			}
		}
		C.XFree(unsafe.Pointer(protos))
		if supported {
			v.sendDelete(w.win, del)
			return
		}
	}
	C.XKillClient(v.dpy, w.win)
	C.XFlush(v.dpy)
}

// sendDelete posts a WM_DELETE_WINDOW client message to a window.
func (v *view) sendDelete(win C.Window, del C.Atom) {
	var ev C.XClientMessageEvent
	ev._type = C.ClientMessage
	ev.window = win
	ev.message_type = C.XInternAtom(v.dpy, C.CString("WM_PROTOCOLS"), C.False)
	ev.format = 32
	data := (*[5]C.long)(unsafe.Pointer(&ev.data))
	data[0] = C.long(del)
	data[1] = C.long(C.CurrentTime)
	C.XSendEvent(v.dpy, win, C.False, C.NoEventMask, (*C.XEvent)(unsafe.Pointer(&ev)))
	C.XFlush(v.dpy)
}

// drawWins paints each managed window's frame: a title bar above it and a
// scaling grip just below its bottom-right corner.
func (v *view) drawWins() {
	th, g := v.titlePx(), v.gripPx()
	for i := range v.wins {
		w := &v.wins[i]
		x, y, sw, sh := v.winRect(w)

		bar := winTitleBg
		if i == len(v.wins)-1 {
			bar = winTitleOn
		}
		v.fill(x, y-th, sw, th, bar)
		if w.title != "" {
			v.text(x+6, y-th+th-5, v.fitText(w.title, sw-winClose-12), winTitleFg)
		}

		// Close button at the right end of the title bar.
		cbx, cby := x+sw-winClose, y-th
		v.fill(cbx, cby, winClose, th, winCloseBg)
		v.fg(winCloseFg)
		p := winClose / 4
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(cbx+p), C.int(cby+p), C.int(cbx+winClose-p), C.int(cby+th-p))
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(cbx+winClose-p), C.int(cby+p), C.int(cbx+p), C.int(cby+th-p))

		gx := x + sw - g
		gy := y + sh
		v.fill(gx, gy, g, g, winGripCol)
		v.fg(bg)
		for k := 1; k <= 3; k++ {
			o := k * g / 4
			C.XDrawLine(v.dpy, v.pm, v.gc, C.int(gx+o), C.int(gy+g), C.int(gx+g), C.int(gy+o))
		}
	}
}

// cycleWindows switches the active window: forward (Tab) rotates the
// bottom-most window to the top, backward (Shift+Tab) sends the top-most one to
// the bottom. Keyboard focus follows the pointer (PointerRoot), so merely
// raising the window is enough for typing to reach it once the pointer is there.
func (v *view) cycleWindows(back bool) {
	n := len(v.wins)
	if n == 0 {
		return
	}
	if n > 1 {
		if back {
			w := v.wins[n-1]
			copy(v.wins[1:], v.wins[:n-1])
			v.wins[0] = w
			C.XRaiseWindow(v.dpy, v.wins[n-1].win)
		} else {
			w := v.wins[0]
			copy(v.wins, v.wins[1:])
			v.wins[n-1] = w
			C.XRaiseWindow(v.dpy, w.win)
		}
	} else {
		C.XRaiseWindow(v.dpy, v.wins[0].win)
	}
	C.XFlush(v.dpy)
	v.redraw = true
}

// grabSwitcher takes Shift+Tab and Alt+F4 at the root, so they work even while
// the keyboard focus is inside an application (kitty, editors, ...). Alt+F4 is a
// last-resort way to close a window whose frame is off screen.
func (v *view) grabSwitcher() {
	C.canvas_ignore_badaccess()
	root := C.XDefaultRootWindow(v.dpy)
	mods := []C.uint{0, C.LockMask, C.Mod2Mask, C.LockMask | C.Mod2Mask}
	grabs := []struct {
		ks  C.KeySym
		mod C.uint
	}{
		{0xff09, C.ShiftMask}, // Shift+Tab: switch windows
		{0xffc1, C.Mod1Mask},  // Alt+F4: close the active window
	}
	for _, g := range grabs {
		kc := C.XKeysymToKeycode(v.dpy, g.ks)
		if kc == 0 {
			continue
		}
		for _, m := range mods {
			C.XGrabKey(v.dpy, C.int(kc), g.mod|m, root,
				C.True, C.GrabModeAsync, C.GrabModeAsync)
		}
	}
	C.XFlush(v.dpy)
}

func (v *view) layout() {
	n := len(v.icons)
	if n == 0 {
		return
	}
	cols := (n + perCol - 1) / perCol
	rows := n
	if rows > perCol {
		rows = perCol
	}
	blockW := float64(cols)*cellW + float64(cols-1)*gapX
	blockH := float64(rows)*cellH + float64(rows-1)*gapY
	x0 := -blockW / 2
	y0 := -blockH / 2
	for i := range v.icons {
		col := i / perCol
		row := i % perCol
		v.icons[i].x = x0 + float64(col)*(cellW+gapX)
		v.icons[i].y = y0 + float64(row)*(cellH+gapY)
		v.icons[i].w = cellW
		v.icons[i].h = cellH
	}
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

// fit frames the whole icon block, but never zooms in past 100%.
func (v *view) fit() {
	if len(v.icons) == 0 {
		v.zoom = 1
		v.centerOrigin()
		return
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, ic := range v.icons {
		minX = math.Min(minX, ic.x)
		minY = math.Min(minY, ic.y)
		maxX = math.Max(maxX, ic.x+ic.w)
		maxY = math.Max(maxY, ic.y+ic.h)
	}
	z := math.Min(float64(v.w)*0.88/(maxX-minX), float64(v.h)*0.80/(maxY-minY))
	z = math.Min(z, 1)
	z = math.Min(zMax, math.Max(zMin, z))
	v.zoom = z
	cx, cy := (minX+maxX)/2, (minY+maxY)/2
	v.ox = cx - float64(v.w)/(2*z)
	v.oy = cy - float64(v.h)/(2*z)
}

func (v *view) resize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if v.xftDraw != nil {
		C.XftDrawDestroy(v.xftDraw)
		v.xftDraw = nil
	}
	if v.pm != 0 {
		C.XFreePixmap(v.dpy, v.pm)
	}
	v.pm = C.XCreatePixmap(v.dpy, v.win, C.uint(w), C.uint(h), v.depth)
	if v.pm == 0 {
		fmt.Fprintln(os.Stderr, "canvas: cannot create pixmap")
		os.Exit(1)
	}
	v.xftDraw = C.XftDrawCreate(v.dpy, C.Drawable(v.pm), v.visual, v.cmap)
	v.w, v.h = w, h
	v.wallResize()
}

func (v *view) paint() {
	v.fill(0, 0, v.w, v.h, bg)
	if v.wall != nil {
		v.drawWallpaper()
	} else {
		v.grid()
		v.axes()
	}
	v.drawIcons()
	v.drawBand()
	v.drawWins()
	v.hud()
	v.drawMenu()
	v.drawPrompt()
	v.drawStatus()
	v.placeWindows()
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

func (v *view) drawIcons() {
	z := v.zoom
	for i, ic := range v.icons {
		sx, sy := v.sx(ic.x), v.sy(ic.y)
		cw := int(math.Round(ic.w * z))
		ch := int(math.Round(ic.h * z))
		if cw < 1 {
			cw = 1
		}
		if ch < 1 {
			ch = 1
		}
		if sx+cw < 0 || sy+ch < 0 || sx > v.w || sy > v.h {
			continue
		}
		bgc := bg
		if v.selset[i] {
			bgc = selBg
			v.fill(sx, sy, cw, ch, selBg)
			v.fg(selBorder)
			C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(sx), C.int(sy),
				C.uint(cw-1), C.uint(ch-1))
		}

		tw := int(math.Round(tile * z))
		if tw < 2 {
			tw = 2
		}
		tx := sx + int(math.Round((cellW-tile)*z/2))
		ty := sy + int(math.Round(tileTop*z))
		switch {
		case ic.isDir:
			v.folderGlyph(tx, ty, tw, ic.up)
		case ic.isApp:
			v.appGlyph(tx, ty, tw, bgc)
		default:
			if ic.thumb != nil && tw >= 48 && v.depth == 24 {
				v.drawThumb(tx, ty, tw, ic.thumb)
				e := strings.ToLower(filepath.Ext(ic.name))
				if isVideo(e) {
					v.playBadge(tx, ty, tw)
				} else if e == ".gif" {
					v.gifBadge(tx, ty, tw)
				}
			} else {
				v.fileGlyph(tx, ty, tw, ic.col, bgc)
			}
		}

		if cw >= 50 {
			name := ic.label
			if name == "" {
				name = ic.name
			}
			label := v.fitText(name, cw-6)
			lw := v.textWidth(label)
			lx := sx + (cw-lw)/2
			if lx < sx {
				lx = sx
			}
			ly := ty + tw + 14
			col := hudCol
			if v.selset[i] {
				col = textCol
			}
			v.text(lx, ly, label, col)
		}
	}
}

func (v *view) folderGlyph(tx, ty, tw int, up bool) {
	m := tw / 8
	if m < 1 {
		m = 1
	}
	v.fill(tx+m, ty+tw/5, tw/2-m/2, tw/9, folderCol)
	v.fill(tx+m, ty+tw/4, tw-2*m, tw*3/5, folderCol)
	if up {
		v.fg(bg)
		cx := tx + tw/2
		top := ty + tw*38/100
		bot := ty + tw*68/100
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(cx), C.int(top), C.int(cx), C.int(bot))
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(cx), C.int(top),
			C.int(cx-tw/10), C.int(top+tw/10))
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(cx), C.int(top),
			C.int(cx+tw/10), C.int(top+tw/10))
	}
}

func (v *view) appGlyph(tx, ty, tw int, bgc color) {
	m := tw / 8
	if m < 1 {
		m = 1
	}
	v.fill(tx+m, ty+m, tw-2*m, tw-2*m, appGlyph)
	x0 := tx + tw*3/10
	y0 := ty + tw*3/10
	w := tw * 4 / 10
	h := tw * 4 / 10
	v.fill(x0, y0, w, h, bgc)
	v.fill(x0, y0, w, maxInt(h/6, 2), paperCol)
}

func (v *view) fileGlyph(tx, ty, tw int, c, bgc color) {
	m := tw / 6
	if m < 1 {
		m = 1
	}
	x0 := tx + m
	y0 := ty + tw/8
	bw := tw - 2*m
	if bw < 3 {
		bw = 3
	}
	bh := tw * 3 / 4
	if bh < 3 {
		bh = 3
	}
	fold := bw / 4
	if fold < 1 {
		fold = 1
	}
	v.fill(x0, y0, bw, bh, paperCol)
	v.fill(x0+bw-fold, y0, fold, fold, bgc)
	v.fg(foldCol)
	C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x0+bw-fold), C.int(y0),
		C.int(x0+bw-fold), C.int(y0+fold))
	C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x0+bw-fold), C.int(y0+fold),
		C.int(x0+bw), C.int(y0+fold))
	v.fill(x0+bw/8, y0+bh-bh/5, bw-bw/4, maxInt(bh/11, 2), c)
}

func (v *view) hud() {
	path := v.root
	if home, err := os.UserHomeDir(); err == nil {
		path = strings.Replace(path, home, "~", 1)
	}
	v.text(14, 22, path, hudCol)
	layout := ""
	if n := len(v.xkb); n > 0 {
		g := v.currentGroup()
		if g < 0 || g >= n {
			g = 0
		}
		layout = "  layout=" + v.xkb[g]
	}
	v.text(14, 43, fmt.Sprintf("x=%.0f  y=%.0f  zoom=%.0f%%  items=%d  selected=%d  windows=%d%s",
		v.ox+float64(v.w)/(2*v.zoom), v.oy+float64(v.h)/(2*v.zoom),
		v.zoom*100, len(v.icons), len(v.selset), len(v.wins), layout), hudCol)
	v.text(14, v.h-12, "dbl-click open   right-click menu   drag icon move   drag empty select   ctrl/shift select   middle-drag pan   wheel zoom   delete trash   ctrl+c/x/v   F2 rename   f fit   F5 refresh   ctrl+r restart   alt+shift layout   title=move grip=scale ×=close   shift+tab switch   alt+f4 close   q quit", hudCol)
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
	if v.xftDraw != nil && v.xftFont != nil {
		if xc := v.xftColor(c); xc != nil {
			cs := C.CString(s)
			C.XftDrawStringUtf8(v.xftDraw, xc, v.xftFont, C.int(x), C.int(y),
				(*C.FcChar8)(unsafe.Pointer(cs)), C.int(len(s)))
			C.free(unsafe.Pointer(cs))
			return
		}
	}
	v.fg(c)
	cs := C.CString(s)
	C.XDrawString(v.dpy, v.pm, v.gc, C.int(x), C.int(y), cs, C.int(len(s)))
	C.free(unsafe.Pointer(cs))
}

func (v *view) textWidth(s string) int {
	if v.xftFont != nil {
		cs := C.CString(s)
		var ext C.XGlyphInfo
		C.XftTextExtentsUtf8(v.dpy, v.xftFont,
			(*C.FcChar8)(unsafe.Pointer(cs)), C.int(len(s)), &ext)
		C.free(unsafe.Pointer(cs))
		return int(ext.xOff)
	}
	return len([]rune(s)) * 7
}

func (v *view) fitText(s string, maxPx int) string {
	if v.textWidth(s) <= maxPx {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		t := string(r) + "…"
		if v.textWidth(t) <= maxPx {
			return t
		}
		r = r[:len(r)-1]
	}
	return "…"
}

func (v *view) xftColor(c color) *C.XftColor {
	if p, ok := v.xcolors[c]; ok {
		return p
	}
	var rc C.XRenderColor
	rc.red = C.ushort(uint16(c.r) * 257)
	rc.green = C.ushort(uint16(c.g) * 257)
	rc.blue = C.ushort(uint16(c.b) * 257)
	rc.alpha = 0xffff
	p := (*C.XftColor)(C.malloc(C.size_t(unsafe.Sizeof(C.XftColor{}))))
	if C.XftColorAllocValue(v.dpy, v.visual, v.cmap, &rc, p) == 0 {
		C.free(unsafe.Pointer(p))
		return nil
	}
	v.xcolors[c] = p
	return p
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

func desktopDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	if b, err := os.ReadFile(filepath.Join(home, ".config", "user-dirs.dirs")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "XDG_DESKTOP_DIR=") {
				continue
			}
			d := strings.Trim(strings.TrimPrefix(line, "XDG_DESKTOP_DIR="), `"`)
			d = strings.ReplaceAll(d, "$HOME", home)
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				return d
			}
		}
	}
	d := filepath.Join(home, "Desktop")
	if fi, err := os.Stat(d); err == nil && fi.IsDir() {
		return d
	}
	return home
}

func fileColor(name string) color {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg", ".ico":
		return color{0x4c, 0x8e, 0xf5}
	case ".txt", ".md", ".pdf", ".doc", ".docx", ".odt", ".rtf", ".csv":
		return color{0x8a, 0x93, 0xa6}
	case ".go", ".c", ".h", ".cc", ".cpp", ".py", ".js", ".ts", ".rs",
		".sh", ".json", ".yaml", ".yml", ".toml":
		return color{0x2f, 0xa0, 0x43}
	case ".mp3", ".wav", ".flac", ".ogg", ".m4a", ".opus":
		return color{0x9b, 0x59, 0xb6}
	case ".mp4", ".mkv", ".webm", ".avi", ".mov":
		return color{0xe6, 0x7e, 0x22}
	case ".zip", ".tar", ".gz", ".xz", ".7z", ".rar", ".zst":
		return color{0x00, 0xa9, 0x9c}
	default:
		return color{0x6b, 0x73, 0x86}
	}
}

func pixel(c color) int {
	return int(c.r)<<16 | int(c.g)<<8 | int(c.b)
}

func mod(a, b int) int {
	return ((a % b) + b) % b
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// --------------------------------------------------------------------- //
// Background work: thumbnails and desktop wallpaper. Generated frames and
// finished thumbnails flow through Go channels into the event loop; every
// message is followed by a wake event sent over a secondary X connection
// (wakeDpy), so libX itself never leaves the main thread.
// --------------------------------------------------------------------- //

var (
	wakeDpy  *C.Display
	wakeWin  C.Window
	wakeAtom C.Atom
)

// wake unblocks the event loop in the main goroutine. The results themselves
// sit on Go channels, drained after every event burst.
func wake() {
	if wakeDpy == nil || wakeWin == 0 {
		return
	}
	var ev C.XEvent
	cm := (*C.XClientMessageEvent)(unsafe.Pointer(&ev))
	cm._type = C.ClientMessage
	cm.display = wakeDpy
	cm.window = wakeWin
	cm.message_type = wakeAtom
	cm.format = 32
	C.XSendEvent(wakeDpy, wakeWin, C.False, 0, (*C.XEvent)(unsafe.Pointer(&ev)))
	C.XFlush(wakeDpy)
}

// drainBackground collects everything the workers queued since the last
// burst of events. Frames are drained before end-of-stream so a static
// wallpaper cannot race its own single frame into a false failure.
func (v *view) drainBackground() {
	for {
		select {
		case m := <-v.wallFrame:
			v.applyFrame(m)
			continue
		default:
		}
		break
	}
	for {
		select {
		case r := <-v.thumbDone:
			v.applyThumb(r)
			continue
		default:
		}
		break
	}
	for {
		select {
		case g := <-v.wallEOF:
			v.onWallEOF(g)
			continue
		default:
		}
		break
	}
}

// ------------------------------------------------------------ thumbnails -- //

type thumbJob struct {
	path  string
	key   string
	video bool
}

type thumbResult struct {
	path string
	key  string
	img  image.Image
}

func (v *view) startThumbWorkers() {
	if v.thumbJobs != nil {
		return
	}
	v.thumbJobs = make(chan thumbJob, 512)
	v.thumbDone = make(chan thumbResult, 512)
	v.thumbPending = map[string]bool{}
	for i := 0; i < 2; i++ {
		go v.thumbWorker()
	}
}

func (v *view) thumbWorker() {
	for j := range v.thumbJobs {
		v.thumbDone <- thumbResult{j.path, j.key, thumbToImage(j)}
		wake()
	}
}

// requestThumbs queues jobs for every media icon still without a preview.
func (v *view) requestThumbs() {
	for i := range v.icons {
		ic := &v.icons[i]
		if ic.isDir || ic.isApp || ic.thumb != nil {
			continue
		}
		video, ok := thumbKind(ic.path)
		if !ok {
			continue
		}
		st, err := os.Stat(ic.path)
		if err != nil {
			continue
		}
		key := thumbKey(ic.path, st)
		if v.thumbPending[key] {
			continue
		}
		select {
		case v.thumbJobs <- thumbJob{ic.path, key, video}:
			v.thumbPending[key] = true
		default:
			// Queue full: retried on the next board refresh.
		}
	}
}

func (v *view) applyThumb(r thumbResult) {
	delete(v.thumbPending, r.key)
	if r.img == nil {
		return
	}
	st, err := os.Stat(r.path)
	if err != nil || thumbKey(r.path, st) != r.key {
		return // the file changed while the thumbnail was generated
	}
	for i := range v.icons {
		if v.icons[i].path == r.path {
			if v.icons[i].thumb == nil {
				v.icons[i].thumb = r.img
				v.redraw = true
			}
			break
		}
	}
}

func thumbKind(path string) (video, ok bool) {
	switch e := strings.ToLower(filepath.Ext(path)); e {
	case ".jpg", ".jpeg", ".png", ".webp", ".bmp", ".tif", ".tiff", ".gif":
		return false, true
	case ".mp4", ".mkv", ".webm", ".mov", ".avi", ".m4v":
		return true, true
	}
	return false, false
}

func thumbKey(path string, st os.FileInfo) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", path, st.ModTime().UnixNano(), st.Size())))
	return hex.EncodeToString(sum[:]) + ".png"
}

func thumbsCache() string {
	base, err := os.UserCacheDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "canvas", "thumbs")
}

// thumbToImage builds the cached thumbnail with external tools and decodes
// it. A nil result means no tool could process the file this time.
func thumbToImage(j thumbJob) image.Image {
	cache := thumbsCache()
	if err := os.MkdirAll(cache, 0755); err != nil {
		return nil
	}
	dst := filepath.Join(cache, j.key)
	if !exists(dst) {
		// The temp name must keep a media extension: ffmpeg infers the muxer
		// from it (rename the finished file onto the hash key afterwards).
		tmp := dst + ".tmp.png"
		box := fmt.Sprintf("%dx%d", thumbSize, thumbSize)
		if j.video {
			poster := func(ss string) error {
				return exec.Command("ffmpeg", "-nostdin", "-loglevel", "error",
					"-ss", ss, "-i", j.path, "-frames:v", "1",
					"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d",
						thumbSize, thumbSize, thumbSize, thumbSize),
					"-y", tmp).Run()
			}
			if poster("1") != nil {
				poster("0")
			}
		} else {
			src := j.path
			if strings.ToLower(filepath.Ext(j.path)) == ".gif" {
				src = j.path + "[0]" // first frame only
			}
			cmd := exec.Command("magick", src, "-auto-orient", "-thumbnail",
				box+"^", "-gravity", "center", "-extent", box, "-strip", "png:"+tmp)
			if err := cmd.Run(); err != nil {
				// Fallback for formats ImageMagick cannot read (e.g. WebP
				// without its delegate).
				exec.Command("ffmpeg", "-nostdin", "-loglevel", "error",
					"-i", j.path, "-frames:v", "1",
					"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d",
						thumbSize, thumbSize, thumbSize, thumbSize),
					"-y", tmp).Run()
			}
		}
		if err := os.Rename(tmp, dst); err != nil {
			return nil
		}
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

// thumbEnsure keeps one shared staging XImage sized for the largest icon on
// screen; smaller previews reuse its top-left rows.
func (v *view) thumbEnsure(sz int) bool {
	if v.thumbImg != nil && v.thumbCap >= sz {
		return true
	}
	ncap := sz
	if ncap < 128 {
		ncap = 128
	}
	if v.thumbImg != nil {
		C.canvas_destroy_image(v.thumbImg)
		v.thumbImg = nil
	}
	data := C.malloc(C.size_t(ncap) * C.size_t(ncap) * 4)
	if data == nil {
		return false
	}
	img := C.XCreateImage(v.dpy, v.visual, v.depth, C.ZPixmap, 0,
		(*C.char)(unsafe.Pointer(data)), C.uint(ncap), C.uint(ncap), 32, C.int(ncap*4))
	if img == nil {
		C.free(unsafe.Pointer(data))
		return false
	}
	v.thumbImg = img
	v.thumbCap = ncap
	return true
}

// drawThumb covers the source inside a paper plate: the shared staging XImage
// is filled in Go (cover-crop, nearest neighbour) and put to the pixmap once.
func (v *view) drawThumb(tx, ty, tw int, src image.Image) {
	sz := tw - 2
	if sz < 4 || !v.thumbEnsure(sz) {
		return
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(v.thumbImg.data)), v.thumbCap*v.thumbCap*4)
	pitch := v.thumbCap * 4
	b := src.Bounds()
	w0, h0 := b.Dx(), b.Dy()
	s := math.Max(float64(sz)/float64(w0), float64(sz)/float64(h0))
	offX := (float64(w0)*s - float64(sz)) / 2
	offY := (float64(h0)*s - float64(sz)) / 2

	for yy := 0; yy < sz; yy++ {
		sy := int((float64(yy) + offY) / s)
		if sy < 0 {
			sy = 0
		} else if sy >= h0 {
			sy = h0 - 1
		}
		rel := sy + b.Min.Y
		r := dst[yy*pitch : yy*pitch+sz*4]
		switch px := src.(type) {
		case *image.RGBA:
			rel = (rel-b.Min.Y)*px.Stride - b.Min.X*4
			for xx := 0; xx < sz; xx++ {
				sx := int((float64(xx)+offX)/s)*4 + rel
				q := xx * 4
				r[q], r[q+1], r[q+2], r[q+3] = px.Pix[sx+2], px.Pix[sx+1], px.Pix[sx], 0
			}
		case *image.NRGBA:
			rel = (rel-b.Min.Y)*px.Stride - b.Min.X*4
			for xx := 0; xx < sz; xx++ {
				sx := int((float64(xx)+offX)/s)*4 + rel
				q := xx * 4
				r[q], r[q+1], r[q+2], r[q+3] = px.Pix[sx+2], px.Pix[sx+1], px.Pix[sx], 0
			}
		case *image.Paletted:
			rel = (rel-b.Min.Y)*px.Stride - b.Min.X
			for xx := 0; xx < sz; xx++ {
				sx := int((float64(xx)+offX)/s) + rel
				cr, cg, cb, _ := px.Palette[px.Pix[sx]].RGBA()
				q := xx * 4
				r[q], r[q+1], r[q+2], r[q+3] = byte(cb>>8), byte(cg>>8), byte(cr>>8), 0
			}
		default:
			for xx := 0; xx < sz; xx++ {
				sx := int((float64(xx)+offX)/s) + b.Min.X
				cr, cg, cb, _ := src.At(sx, rel).RGBA()
				q := xx * 4
				r[q], r[q+1], r[q+2], r[q+3] = byte(cb>>8), byte(cg>>8), byte(cr>>8), 0
			}
		}
	}
	v.fill(tx, ty, tw, tw, paperCol)
	C.XPutImage(v.dpy, v.pm, v.gc, v.thumbImg, 0, 0,
		C.int(tx+1), C.int(ty+1), C.uint(sz), C.uint(sz))
	v.fg(foldCol)
	C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(tx), C.int(ty),
		C.uint(tw-1), C.uint(tw-1))
}

func (v *view) playBadge(tx, ty, tw int) {
	r := tw / 5
	if r < 10 {
		r = 10
	} else if r > 16 {
		r = 16
	}
	cx := tx + tw - r - 3
	cy := ty + tw - r - 3
	v.fg(badgeBg)
	C.XFillArc(v.dpy, v.pm, v.gc, C.int(cx), C.int(cy), C.uint(r), C.uint(r),
		C.int(0), C.int(360*64))
	var pts [3]C.XPoint
	pts[0].x = C.short(cx + r*35/100)
	pts[0].y = C.short(cy + r*25/100)
	pts[1].x = C.short(cx + r*35/100)
	pts[1].y = C.short(cy + r*75/100)
	pts[2].x = C.short(cx + r*70/100)
	pts[2].y = C.short(cy + r*50/100)
	v.fg(badgeFg)
	C.XSetLineAttributes(v.dpy, v.gc, 2, C.LineSolid, C.CapButt, C.JoinMiter)
	C.XDrawLines(v.dpy, v.pm, v.gc, &pts[0], 3, C.CoordModeOrigin)
	C.XSetLineAttributes(v.dpy, v.gc, 0, C.LineSolid, C.CapButt, C.JoinMiter)
}

func (v *view) gifBadge(tx, ty, tw int) {
	if tw < 56 {
		return
	}
	label := "GIF"
	w := v.textWidth(label) + 8
	v.fill(tx+tw-w-2, ty+2, w, 15, badgeBg)
	v.text(tx+tw-w+2, ty+11, label, badgeFg)
}

// ------------------------------------------------------------- wallpaper -- //

// wallpaper shows a photo or an animated GIF/video behind the board. Animated
// media is streamed frame by frame from ffmpeg (raw rgb24 userinfo over a
// pipe); static photos are rendered once by ImageMagick. The wallpaper is
// screen-fixed: the board pans and zooms over it like icons over a desktop.
type wallpaper struct {
	path     string
	gen      int  // generation counter, stale frames are dropped
	boots    int  // end-of-stream restarts of the animated pipeline
	static   bool // one magick-rendered frame, no endless pipe
	hasFrame bool // at least one frame arrived and was drawn
	w, h     int  // frame size, equals the viewport at boot time
	proc     *exec.Cmd
}

type wallMsg struct {
	gen int
	rgb []byte // one rgb24 frame, w*h*3 bytes
}

func (v *view) startWallpaper(path string) {
	if v.depth != 24 {
		v.status = "Обои: нужен экран глубиной 24 бита"
		v.redraw = true
		return
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		v.status = "Обои: файл недоступен"
		v.redraw = true
		return
	}
	if v.wall != nil && v.wall.proc != nil {
		v.wall.proc.Process.Kill()
	}
	if v.wallFrame == nil {
		v.wallFrame = make(chan wallMsg, 2)
		v.wallEOF = make(chan int, 1)
	}
	ext := strings.ToLower(filepath.Ext(path))
	wp := &wallpaper{
		path:   path,
		gen:    v.wallGen + 1,
		w:      v.w,
		h:      v.h,
		static: !isVideo(ext) && ext != ".gif",
	}
	if err := v.wallBoot(wp, v.w, v.h); err != nil {
		v.status = "Обои: " + err.Error()
		v.redraw = true
		return
	}
	v.wallGen++
	v.wall = wp
}

func (v *view) setWallpaper(path string) {
	v.startWallpaper(path)
	if v.wall == nil {
		return
	}
	v.saveWallpaper()
	v.status = "Обои: " + filepath.Base(path)
	v.redraw = true
}

func (v *view) clearWallpaper(status string) {
	if v.wall != nil && v.wall.proc != nil {
		v.wall.proc.Process.Kill()
	}
	v.wall = nil
	if v.wallImg != nil {
		C.canvas_destroy_image(v.wallImg)
		v.wallImg = nil
		v.wallW, v.wallH = 0, 0
	}
	os.Remove(wallpaperConfigPath())
	if status != "" {
		v.status = status
	}
	v.redraw = true
}

// wallBoot (re)starts the frame pipeline for wp at the given size.
func (v *view) wallBoot(wp *wallpaper, w, h int) error {
	if v.wallImg != nil && (v.wallW != w || v.wallH != h) {
		C.canvas_destroy_image(v.wallImg)
		v.wallImg = nil
		v.wallW, v.wallH = 0, 0
	}
	if v.wallImg == nil {
		data := C.malloc(C.size_t(w) * C.size_t(h) * 4)
		if data == nil {
			return fmt.Errorf("нет памяти")
		}
		img := C.XCreateImage(v.dpy, v.visual, v.depth, C.ZPixmap, 0,
			(*C.char)(unsafe.Pointer(data)), C.uint(w), C.uint(h), 32, C.int(w*4))
		if img == nil {
			C.free(unsafe.Pointer(data))
			return fmt.Errorf("XCreateImage")
		}
		v.wallImg = img
		v.wallW, v.wallH = w, h
	}
	var cmd *exec.Cmd
	video := false
	switch strings.ToLower(filepath.Ext(wp.path)) {
	case ".mp4", ".webm", ".mov", ".mkv", ".avi":
		video = true
	}
	if wp.static && !video {
		cmd = exec.Command("magick", wp.path, "-auto-orient",
			"-resize", fmt.Sprintf("%dx%d^", w, h),
			"-gravity", "center", "-extent", fmt.Sprintf("%dx%d", w, h),
			"rgb:-")
	} else if wp.static {
		// video posters go through ffmpeg; magick cannot read them
		fit := fmt.Sprintf("%d:%d", w, h)
		cmd = exec.Command("ffmpeg", "-nostdin", "-loglevel", "error",
			"-ss", "0", "-i", wp.path, "-frames:v", "1", "-an",
			"-vf", fmt.Sprintf("scale=%s:force_original_aspect_ratio=increase,crop=%s", fit, fit),
			"-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:")
	} else {
		fit := fmt.Sprintf("%d:%d", w, h)
		argv := []string{"ffmpeg", "-nostdin", "-loglevel", "error"}
		if strings.ToLower(filepath.Ext(wp.path)) == ".gif" {
			argv = append(argv, "-ignore_loop", "0")
		}
		argv = append(argv, "-stream_loop", "-1", "-i", wp.path, "-an",
			"-vf", fmt.Sprintf("fps=%d,scale=%s:force_original_aspect_ratio=increase,crop=%s", wallFps, fit, fit),
			"-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:")
		cmd = exec.Command(argv[0], argv[1:]...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	wp.proc = cmd
	wp.w, wp.h = w, h
	wp.hasFrame = false
	gen := wp.gen

	// Reader: streams one rgb24 frame per wake into the main thread. When the
	// stream ends (killed, or a looping format hit its end) the generation
	// number goes back for the main thread to restart or give up.
	go func() {
		buf := make([]byte, w*h*3)
		for {
			if _, err := io.ReadFull(stdout, buf); err != nil {
				cmd.Wait()
				v.wallEOF <- gen
				wake()
				return
			}
			select {
			case v.wallFrame <- wallMsg{gen, append([]byte(nil), buf...)}:
			default: // the main thread is behind; the next frame replaces it
			}
			wake()
		}
	}()
	return nil
}

func (v *view) onWallEOF(gen int) {
	wp := v.wall
	if wp == nil || wp.gen != gen {
		return
	}
	if wp.static {
		// A static render finished; the frame stays on screen. No frame at
		// all means the file could not be converted.
		if !wp.hasFrame {
			v.clearWallpaper("Обои: файл не читается")
		}
		return
	}
	if wp.boots++; wp.boots > 4 {
		v.clearWallpaper("Обои: файл не читается")
		return
	}
	v.wallGen++
	wp.gen = v.wallGen
	if err := v.wallBoot(wp, wp.w, wp.h); err != nil {
		v.status = "Обои: " + err.Error()
		v.redraw = true
	}
}

// wallResize reboots the pipeline when the viewport changes size.
func (v *view) wallResize() {
	if v.wall == nil || (v.wall.w == v.w && v.wall.h == v.h) {
		return
	}
	wp := v.wall
	if wp.proc != nil {
		wp.proc.Process.Kill()
	}
	wp.boots = 0
	v.wallGen++
	wp.gen = v.wallGen
	if err := v.wallBoot(wp, v.w, v.h); err != nil {
		v.status = "Обои: " + err.Error()
	}
	v.redraw = true
}

func (v *view) applyFrame(m wallMsg) {
	wp := v.wall
	if wp == nil || m.gen != wp.gen || v.wallImg == nil || len(m.rgb) != wp.w*wp.h*3 {
		return
	}
	wp.hasFrame = true
	dst := unsafe.Slice((*byte)(unsafe.Pointer(v.wallImg.data)), wp.w*wp.h*4)
	for s, d := 0, 0; s < len(m.rgb); s, d = s+3, d+4 {
		// rgb24 -> ZPixmap BGRX little endian
		dst[d] = m.rgb[s+2]
		dst[d+1] = m.rgb[s+1]
		dst[d+2] = m.rgb[s]
		dst[d+3] = 0
	}
	v.redraw = true
}

func (v *view) drawWallpaper() {
	if v.wall == nil || v.wallImg == nil || !v.wall.hasFrame {
		return
	}
	C.XPutImage(v.dpy, v.pm, v.gc, v.wallImg, 0, 0, 0, 0,
		C.uint(v.wall.w), C.uint(v.wall.h))
}

type wallConfig struct {
	Path string `json:"path"`
}

func wallpaperConfigPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "canvas", "wallpaper.json")
	}
	return filepath.Join(base, "canvas", "wallpaper.json")
}

func (v *view) saveWallpaper() {
	if v.wall == nil {
		return
	}
	p := wallpaperConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return
	}
	data, err := json.Marshal(wallConfig{Path: v.wall.path})
	if err != nil {
		return
	}
	os.WriteFile(p, data, 0644)
}

func (v *view) loadWallpaper() {
	data, err := os.ReadFile(wallpaperConfigPath())
	if err != nil {
		return
	}
	var cfg wallConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	if cfg.Path == "" {
		return
	}
	if _, err := os.Stat(cfg.Path); err != nil {
		return
	}
	v.startWallpaper(cfg.Path)
}

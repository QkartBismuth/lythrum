// Command canvas is a small X11 board built on cgo and libX11. The board is an
// infinite grid holding shortcuts to the contents of the desktop folder. The
// viewport pans and zooms over it with the mouse, wheel or keyboard.
package main

/*
#cgo pkg-config: x11 xft
#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/keysym.h>
#include <X11/cursorfont.h>
#include <X11/Xft/Xft.h>
*/
import "C"

import (
	"fmt"
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
)

type boardIcon struct {
	name  string
	path  string
	isDir bool
	up    bool
	col   color

	x, y, w, h float64 // cell rectangle in board coordinates
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

	root     string // desktop folder; navigation never goes above it
	dir      string // folder currently shown
	icons    []boardIcon
	selected int
	pressed  int
	pressX   int
	pressY   int
	lastIdx  int
	lastTime time.Time

	dragging     bool
	lastX, lastY int

	redraw   bool
	needBlit bool

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
		dpy:      dpy,
		screen:   screen,
		depth:    C.uint(C.XDefaultDepth(dpy, screen)),
		win:      win,
		gc:       C.XCreateGC(dpy, win, 0, nil),
		visual:   C.XDefaultVisual(dpy, screen),
		cmap:     C.XDefaultColormap(dpy, screen),
		zoom:     1,
		selected: -1,
		pressed:  -1,
		lastIdx:  -1,
		xcolors:  map[color]*C.XftColor{},
	}
	v.xftFont = C.XftFontOpenName(dpy, screen, C.CString("sans:size=10"))
	if v.xftFont == nil {
		fmt.Fprintln(os.Stderr, "canvas: xft font unavailable, falling back to core text")
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

	v.root = os.Getenv("CANVAS_DIR")
	if v.root == "" {
		v.root = desktopDir()
	}
	v.dir = v.root
	v.reload(true)

	C.XMapWindow(dpy, win)
	C.XFlush(dpy)

	fmt.Printf("board: showing %s\n", v.dir)
	fmt.Println("  dbl-click   open / enter folder")
	fmt.Println("  drag        pan        wheel       zoom at pointer")
	fmt.Println("  shift+wheel pan left/right      ctrl+wheel  pan up/down")
	fmt.Println("  arrows      pan        + / -       zoom")
	fmt.Println("  f fit       F5 refresh  r recenter 0 zoom 100%  q quit")

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
		v.onRelease((*C.XButtonEvent)(unsafe.Pointer(xev)))
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
	x, y := int(be.x), int(be.y)
	switch be.button {
	case 1, 2:
		C.XGrabPointer(v.dpy, v.win, C.False,
			C.ButtonPressMask|C.ButtonReleaseMask|C.PointerMotionMask,
			C.GrabModeAsync, C.GrabModeAsync, 0, 0, C.CurrentTime)
		if be.button == 1 {
			if i := v.hitTest(x, y); i >= 0 {
				v.pressed = i
				v.pressX, v.pressY = x, y
				v.selected = i
				v.redraw = true
				return
			}
		}
		v.dragging = true
		v.lastX, v.lastY = x, y
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
	if be.button == 1 && v.pressed >= 0 {
		i := v.pressed
		v.pressed = -1
		now := time.Now()
		if v.lastIdx == i && now.Sub(v.lastTime) < 400*time.Millisecond {
			v.openIcon(i)
			v.lastIdx = -1
		} else {
			v.lastIdx = i
			v.lastTime = now
		}
	}
	v.stopDrag()
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

	// A press on an icon becomes a pan once the pointer actually moves.
	if v.pressed >= 0 && !v.dragging {
		if abs(x-v.pressX) > 4 || abs(y-v.pressY) > 4 {
			v.pressed = -1
			v.dragging = true
			v.lastX, v.lastY = v.pressX, v.pressY
		}
	}
	if !v.dragging {
		return
	}
	if me.state&C.Button1Mask == 0 && me.state&C.Button2Mask == 0 {
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
	shift := ke.state&C.ShiftMask != 0
	cx, cy := float64(v.w)/2, float64(v.h)/2
	switch ks {
	case 0xff1b, 'q': // Escape, q
		C.XCloseDisplay(v.dpy)
		os.Exit(0)
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
	if !ic.isDir {
		openPath(ic.path)
		return
	}
	if rel, err := filepath.Rel(v.root, ic.path); err != nil || strings.HasPrefix(rel, "..") {
		return
	}
	v.dir = ic.path
	v.reload(true)
}

func (v *view) reload(reset bool) {
	v.selected = -1
	v.pressed = -1
	v.lastIdx = -1

	var icons []boardIcon
	if v.dir != v.root {
		icons = append(icons, boardIcon{
			name: "..", path: filepath.Dir(v.dir), isDir: true, up: true,
			col: folderCol,
		})
	}

	ents, err := os.ReadDir(v.dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "canvas: readdir:", err)
	}
	var dirs, files []boardIcon
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		ic := boardIcon{name: name, path: filepath.Join(v.dir, name), isDir: e.IsDir()}
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
			return strings.ToLower(s[i].name) < strings.ToLower(s[j].name)
		})
	}
	less(dirs)
	less(files)
	icons = append(icons, dirs...)
	icons = append(icons, files...)
	v.icons = icons
	v.layout()

	if reset {
		v.fit()
	}
	v.redraw = true
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
}

func (v *view) paint() {
	v.fill(0, 0, v.w, v.h, bg)
	v.grid()
	v.axes()
	v.drawIcons()
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
		if i == v.selected {
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
		if ic.isDir {
			v.folderGlyph(tx, ty, tw, ic.up)
		} else {
			v.fileGlyph(tx, ty, tw, ic.col, bgc)
		}

		if cw >= 50 {
			label := v.fitText(ic.name, cw-6)
			lw := v.textWidth(label)
			lx := sx + (cw-lw)/2
			if lx < sx {
				lx = sx
			}
			ly := ty + tw + 14
			col := hudCol
			if i == v.selected {
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
	path := v.dir
	if home, err := os.UserHomeDir(); err == nil {
		path = strings.Replace(path, home, "~", 1)
	}
	v.text(14, 22, path, hudCol)
	v.text(14, 43, fmt.Sprintf("x=%.0f  y=%.0f  zoom=%.0f%%  items=%d",
		v.ox+float64(v.w)/(2*v.zoom), v.oy+float64(v.h)/(2*v.zoom),
		v.zoom*100, len(v.icons)), hudCol)
	v.text(14, v.h-12, "dbl-click open   drag pan   wheel zoom   f fit   F5 refresh   q quit", hudCol)
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

func openPath(p string) {
	cmd := exec.Command("xdg-open", p)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "canvas: open:", err)
		return
	}
	go func() { _ = cmd.Wait() }()
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

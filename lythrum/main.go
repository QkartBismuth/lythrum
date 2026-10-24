// Command lythrum is a small X11 board built on cgo and libX11. The board is an
// infinite grid holding shortcuts to the contents of the desktop folder. The
// viewport pans and zooms over it with the mouse, wheel or keyboard.
package main

/*
#cgo pkg-config: x11 xft xrender xcomposite xdamage
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/keysym.h>
#include <X11/XKBlib.h>
#include <X11/cursorfont.h>
#include <X11/Xft/Xft.h>
#include <X11/extensions/Xrender.h>
#include <X11/extensions/Xcomposite.h>
#include <X11/extensions/Xdamage.h>

// The last protocol error the shell ran into.
static int lythrum_last_error = 0;

// lythrum_expose asks an application to paint its window again.
static void lythrum_expose(Display *dpy, Window w);

// The major opcode of the composite extension, and whether the server refused
// to redirect a window into a pixmap.
static int lythrum_composite_opcode = -1;
static int lythrum_render_opcode = -1;
static int lythrum_no_composite = 0;

// lythrum_xerror keeps the default Xlib handler from terminating the whole
// session on a recoverable protocol error (e.g. a key already grabbed by
// someone else). Real errors are still reported on stderr.
static int lythrum_xerror(Display *dpy, XErrorEvent *ev) {
	lythrum_last_error = ev->error_code;
	// A window that cannot be redirected into a pixmap cannot be shown as a
	// miniature: the board draws it as it is. Under a compositor the server
	// refuses to redirect the windows of the clients a second time.
	if ((lythrum_composite_opcode >= 0 && ev->request_code == lythrum_composite_opcode) ||
	    (lythrum_render_opcode >= 0 && ev->request_code == lythrum_render_opcode)) {
		lythrum_no_composite = 1;
	}
	if (ev->error_code != BadAccess) {
		char buf[256];
		XGetErrorText(dpy, ev->error_code, buf, sizeof buf);
		fprintf(stderr, "lythrum: X error: %s (request %d)\n", buf, ev->request_code);
	}
	return 0;
}
static void lythrum_ignore_badaccess(void) {
	XSetErrorHandler(lythrum_xerror);
}
static void lythrum_destroy_image(XImage *img) { XDestroyImage(img); }

// The rendering of a window can be sent into a pixmap, which the board then
// draws scaled in the frame of the window. Not every server allows it.
// The board needs the number of the composite extension to recognise the
// errors of the redirection.
static void lythrum_set_composite_opcode(Display *dpy) {
	int major = 0, event_base = 0, error_base = 0;
	if (XQueryExtension(dpy, "Composite", &major, &event_base, &error_base)) {
		lythrum_composite_opcode = major;
	}
	if (XQueryExtension(dpy, "RENDER", &major, &event_base, &error_base)) {
		lythrum_render_opcode = major;
	}
}

// The board gives up on the miniature when the server refuses to redirect a
// window into a pixmap.
static int lythrum_composite_broken(void) {
	return lythrum_no_composite;
}

static int lythrum_redirect(Display *dpy, Window win) {
	lythrum_last_error = 0;
	// A single window is redirected: the server refuses to redirect the
	// subwindows of a window automatically. The server does not paint the
	// window on the screen: the board draws the content of the application
	// itself, in the frame of the window on the canvas.
	XCompositeRedirectWindow(dpy, win, False);
	XSync(dpy, False);
	if (lythrum_last_error == 0 && !lythrum_no_composite) {
		// The application has to paint its window again: a redirected
		// window keeps no content of its own.
		lythrum_expose(dpy, win);
	}
	return lythrum_last_error == 0 && !lythrum_no_composite;
}

// lythrum_container creates the container that holds a managed window. The
// window has no background of its own: the miniature the board paints shows
// through it, and the application is clipped by its edges.
static Window lythrum_container(Display *dpy, Window parent, unsigned w,
                               unsigned h) {
	XSetWindowAttributes attr;
	memset(&attr, 0, sizeof(attr));
	attr.border_pixel = 0;
	attr.override_redirect = False;
	// No background of its own: the miniature the board paints shows through
	// the container, and the application is clipped by its edges.
	attr.background_pixmap = None;
	attr.event_mask = SubstructureNotifyMask | ButtonPressMask |
	                  ButtonReleaseMask | PointerMotionMask;
	int screen = XDefaultScreen(dpy);
	return XCreateWindow(dpy, parent, 0, 0, w, h, 0,
	                     XDefaultDepth(dpy, screen), InputOutput,
	                     XDefaultVisual(dpy, screen),
	                     CWBorderPixel | CWEventMask | CWOverrideRedirect |
	                     CWBackPixmap, &attr);
}
// lythrum_expose asks an application to paint its window again. A redirected
// window keeps no content of its own: the application has to draw it once
// more, or the board would show what was on the screen before.
static void lythrum_expose(Display *dpy, Window w) {
	XWindowAttributes attr;
	XEvent ev;
	XExposeEvent *ex = (XExposeEvent *)&ev;
	int width = 0, height = 0;
	if (XGetWindowAttributes(dpy, w, &attr)) {
		width = attr.width;
		height = attr.height;
	}
	ex->type = Expose;
	ex->display = dpy;
	ex->window = w;
	ex->x = 0;
	ex->y = 0;
	ex->width = width;
	ex->height = height;
	ex->count = 0;
	XSendEvent(dpy, w, False, ExposureMask, &ev);
	XFlush(dpy);
}
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
	"syscall"
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

	winGrip  = 16 // corner grip size, screen pixels
	winClose = 22 // close button size, screen pixels (equals the title bar)

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
	anim  *gifAnim    // the frames of an animated gif

	x, y, w, h float64 // cell rectangle in board coordinates
}

// menuItem is one row of the context menu; sep turns the row into a divider.
type menuItem struct {
	label string
	sep   bool
	do    func()
}

// managedWin is a real application window reparented into the board. The frame
// (an X container that clips the application) is a board object: like the
// icons, its position and size live in board units, so it pawns pans and zooms
// with the canvas. The application itself stays at the pixel size it was
// opened with and is never resized by the viewport: when the frame shrinks the
// content is simply clipped by the container, when it grows the extra area
// shows the container background.
type managedWin struct {
	win   C.Window // the real application window
	cont  C.Window // board-owned container: scaled with the viewport
	title string

	bx float64 // board position of the frame's top-left corner
	by float64
	bw float64 // frame size in board units: it scales with the viewport zoom
	bh float64 // just like the size of every other object on the board

	aw float64 // the application's own size, in screen pixels: zooming the
	ah float64 // board never resizes it, so apps never reflow their content

	vis *C.Visual // the application's visual, for its render picture

	// Content: the window is redirected by the composite extension, so its
	// pixels render into a backing pixmap while the real window stays hidden
	// outside the container's clip. drawContent paints the backing pixmap at
	// its own pixel size, and the application itself is never resized: it
	// reflows nothing and keeps its live state.
	virtual    bool      // the shell draws the window itself, it has no X window
	widget     bool      // the window holds a widget of the shell
	widgetKind string    // the kind of the widget: clock, calendar, control…
	widgetXID  C.ulong   // the window of the toolkit behind it
	tmb        bool      // true when the frame no longer covers the whole app
	redir      bool      // the rendering of the app goes into a pixmap
	float      bool      // the window is not inside the board, it floats over it
	cpm        C.Pixmap  // the backing pixmap the miniature is drawn from
	cpic       C.Picture // render picture over the backing pixmap
	cpw        int       // backing pixmap geometry the picture belongs to
	cph        int

	dmg C.Damage // damage tracker of the child, to refresh the miniature

	// selfReq holds the sizes of resizes the board asked for but whose
	// ConfigureNotify echoes have not arrived yet. A stale echo that reports an
	// old size must not be mistaken for the application resizing itself,
	// otherwise the frame flaps between sizes while dragging or zooming.
	selfReq [][2]int

	lastX, lastY, lastW, lastH int
}

// redirWait remembers a window that is redirected a moment after its
// adoption: the application has to paint itself first, or the board would
// keep the content of the screen instead of the content of the application.
type redirWait struct {
	win   C.Window
	until time.Time
}

// pendingLaunch remembers which top-level window to adopt once a freshly
// started application maps it.
type pendingLaunch struct {
	cx, cy float64 // board point where the window should be centred
	before map[C.Window]bool
	until  time.Time
	n      int // how many companion windows this launch has already adopted
}

type view struct {
	dpy    *C.Display
	screen C.int
	depth  C.uint
	win    C.Window
	gc     C.GC
	pm     C.Pixmap
	dpict  C.Picture // render picture over pm, destination of miniatures
	visual *C.Visual
	cmap   C.Colormap

	compOK  bool // the composite extension is present and usable
	dmgOK   bool // the damage extension notifies about app repaints
	adopted map[C.Window]bool
	evTime  C.Time   // last input event timestamp, reused for synthesized ones
	thumbP  int      // window whose miniature is being pressed, -1 by default
	kbFocus C.Window // window holding keyboard focus, initially the board

	xftFont *C.XftFont
	xftDraw *C.XftDraw
	xcolors map[color]*C.XftColor

	w, h   int
	ox, oy float64 // board coordinate shown at the viewport's top-left pixel
	zoom   float64

	root    string // desktop folder, the single source of shortcuts
	icons   []boardIcon
	wins    []managedWin
	pending *pendingLaunch
	xkb     []string // keyboard layout names, for the HUD indicator
	cfg     settings
	keyMap  map[combo]string
	keyList []keyEntry
	grabs   []grabRec

	superDown  bool // the Super key is held down
	superChord bool // another key was pressed while it was held

	// The state of the touch gestures.
	touches        map[int]touchPoint
	touchStart     touchPoint
	touchStartTime time.Time
	touchMoved     bool
	touchPinch     bool
	touchBaseDist  float64
	touchBaseMidX  float64
	touchBaseMidY  float64
	touchAt        time.Time
	gesturesOK     bool
	// The last moment the widgets were fed with the state of the shell.
	widgetTick time.Time
	selset     map[int]bool
	anchor     int // last single-clicked icon, base for Shift+click ranges
	pressed    int
	pressX     int
	pressY     int
	pressAdd   bool
	lastIdx    int
	lastTime   time.Time

	iconDrag   bool // moving selected icons
	orig       map[int][2]float64
	dropTarget int // folder the dragged icons are hovering over

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
	dragSW    int // application size, in pixels, when the resize began
	dragSH    int
	dragBX    float64
	dragBY    float64
	dragBW    float64 // application size, in pixels, when the resize began
	dragBH    float64

	redraw   bool
	needBlit bool

	// Background work: thumbnail generation and wallpaper frames stream into
	// the event loop through these channels; each message is accompanied by a
	// wake event sent via a secondary X connection.
	thumbJobs    chan thumbJob
	thumbDone    chan thumbResult
	thumbPending map[string]bool
	gifCache     map[string]*gifAnim
	thumbImg     *C.XImage // shared staging ZPixmap for thumbnails
	thumbCap     int       // staging square edge in pixels

	dirEvent chan int // desktop folder changed, wake and reload the icons
	// The interface of the board itself (the context menu, the prompt) lives
	// in a window of its own, above the applications: the applications are
	// real windows and would hide the board otherwise.
	pop   C.Window
	popGC C.GC
	popFt *C.XftDraw // the Xft context of the popup window
	popX  int        // the place of the popup, in the coordinates of the board
	popY  int
	popW  int
	popH  int
	// The widgets of the shell: windows of the toolkit on the board.
	widgetUntil time.Time // the toolkit opens helper windows of its own
	widgetXID   C.ulong
	widgetKinds map[C.ulong]string
	// importDone carries the files copied in from a file manager.
	importDone chan []string
	// xdn follows the drag of files over the board.
	xdn xdndState
	// redirWait holds the windows that are redirected a moment after their
	// adoption: the application has to paint itself first.
	redirWait []redirWait
	// xds follows the drag of the files of the board over another
	// application: the board offers them to it.
	xds xdndSource
	// The settings application: a window the shell draws itself.
	set      *settingsApp
	setWin   *managedWin
	setOpen  bool
	setFocus bool
	// dropBX and dropBY remember where the files were dropped, in board
	// coordinates, so the new icons can be placed there.
	dropBX, dropBY float64

	wall      *wallpaper
	wallGen   int       // bump on every boot so stale frames are dropped
	wallImg   *C.XImage // screen-sized ZPixmap holding the current frame
	wallW     int
	wallH     int
	wallFrame chan wallMsg
	wallEOF   chan int

	wmDelete C.Atom
}

// xIgnoreBadAccess installs the error handler that keeps a recoverable
// protocol error (a key already grabbed by someone else) from killing the
// session.
func xIgnoreBadAccess() {
	C.lythrum_ignore_badaccess()
}

func main() {
	runtime.LockOSThread()

	// The error handler is installed before anything else: a recoverable
	// protocol error must not take the whole session down.
	xIgnoreBadAccess()

	dpy := C.XOpenDisplay(nil)
	if dpy == nil {
		fmt.Fprintln(os.Stderr, "lythrum: cannot open display; is DISPLAY set?")
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
		os.Setenv("XDG_CURRENT_DESKTOP", "Lythrum")
		os.Setenv("XDG_SESSION_DESKTOP", "lythrum")
		os.Setenv("DESKTOP_SESSION", "lythrum")
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
		fmt.Fprintln(os.Stderr, "lythrum: cannot create window")
		os.Exit(1)
	}

	C.XSelectInput(dpy, win,
		C.ExposureMask|C.KeyPressMask|C.KeyReleaseMask|C.ButtonPressMask|C.ButtonReleaseMask|
			C.PointerMotionMask|C.StructureNotifyMask|C.SubstructureNotifyMask)
	// Watch top-level windows so a freshly launched application can be adopted.
	C.XSelectInput(dpy, root, C.SubstructureNotifyMask)

	C.XStoreName(dpy, win, C.CString("Board - Go / X11"))

	v := &view{
		dpy:         dpy,
		screen:      screen,
		depth:       C.uint(C.XDefaultDepth(dpy, screen)),
		win:         win,
		gc:          C.XCreateGC(dpy, win, 0, nil),
		visual:      C.XDefaultVisual(dpy, screen),
		cmap:        C.XDefaultColormap(dpy, screen),
		zoom:        1,
		selset:      map[int]bool{},
		anchor:      -1,
		pressed:     -1,
		lastIdx:     -1,
		winDrag:     -1,
		dropTarget:  -1,
		winResize:   -1,
		xcolors:     map[color]*C.XftColor{},
		xkb:         xkbLayouts,
		adopted:     map[C.Window]bool{},
		thumbP:      -1,
		kbFocus:     win,
		dirEvent:    make(chan int, 64),
		widgetKinds: map[C.ulong]string{},
		cfg:         loadSettings(),
	}
	v.rebuildKeyMap()
	applyStyle(styleByName(v.cfg.Style))
	v.reopenFont()
	v.applyCursor()
	v.xdndInit()
	v.gesturesInit()
	v.xftFont = C.XftFontOpenName(dpy, screen, C.CString("sans:size=10"))
	if v.xftFont == nil {
		fmt.Fprintln(os.Stderr, "lythrum: xft font unavailable, falling back to core text")
	}

	// A second connection lets background goroutines wake the event loop with
	// a minimal event without sharing the main display across threads.
	wakeDpy = C.XOpenDisplay(nil)
	if wakeDpy == nil {
		fmt.Fprintln(os.Stderr, "lythrum: no secondary display, background work wakes on the next event only")
	} else {
		wakeWin = win
		wakeAtom = C.XInternAtom(wakeDpy, C.CString("LYTHRUM_WAKE"), C.False)
	}
	v.startThumbWorkers()
	startAnimTicker()
	startWidgetTicker()
	v.initToolkit()

	v.wmDelete = C.XInternAtom(dpy, C.CString("WM_DELETE_WINDOW"), C.False)
	proto := []C.Atom{v.wmDelete}
	C.XSetWMProtocols(dpy, win, (*C.Atom)(unsafe.Pointer(&proto[0])), 1)

	var geom C.XWindowAttributes
	C.XGetWindowAttributes(dpy, win, &geom)
	v.resize(int(geom.width), int(geom.height))

	// Miniatures of managed windows are drawn by the composite extension; the
	// damage extension tells the board when an application redraws and the
	// board must refresh its miniature.
	v.bootComposite()

	v.root = os.Getenv("LYTHRUM_DIR")
	if v.root == "" {
		v.root = desktopDir()
	}
	v.reload(true)
	// The pages of the widgets live in a folder of the desktop, so the user
	// can change them.
	ensureWidgetFolder()
	go watchDir(v.root, v.dirEvent)

	C.XMapWindow(dpy, win)
	C.XFlush(dpy)
	// A bare X server has nowhere else to send keyboard input; take the input
	// focus so the board's shortcuts work right after startup.
	C.XSetInputFocus(dpy, win, C.PointerRoot, C.CurrentTime)
	C.XFlush(dpy)

	v.regrabKeys()
	v.loadWallpaper()

	fmt.Printf("board: showing %s\n", v.root)
	fmt.Println("  dbl-click   open in a real application window")
	fmt.Println("  super      back to the centre of the board")
	fmt.Println("  t / super+r terminal   drag pan   wheel zoom at pointer")
	fmt.Println("  shift+wheel pan left/right      ctrl+wheel  pan up/down")
	fmt.Println("  arrows      pan        + / -       zoom")
	fmt.Println("  f fit       F5 refresh  r recenter 0 zoom 100%  q quit")
	fmt.Println("  ctrl+r      restart the board")
	fmt.Println("  window: drag its title to move, drag the blue grip to scale, click it to raise, × closes")
	fmt.Println("  shift+tab switches between open windows, alt+f4 closes the active one")
	fmt.Println("  drop a file on a folder to move it inside")
	if session {
		fmt.Printf("session: Lythrum desktop, apps=%q\n", os.Getenv("LYTHRUM_SESSION_APPS"))
		v.startSession()
	}

	v.paint()
	v.blit()
	if os.Getenv("LYTHRUM_DEBUG_ICONS") == "1" {
		for i, ic := range v.icons {
			fmt.Printf("icon %d %q at %d,%d size %d,%d\n", i, ic.name,
				v.sx(ic.x)+int(ic.w*v.zoom/2), v.sy(ic.y)+int(ic.h*v.zoom/2),
				int(ic.w*v.zoom), int(ic.h*v.zoom))
		}
	}
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
		v.pumpToolkit()
		v.tickWidgets()
		v.doRedirs()
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

// doRedirs redirects the applications that have had the time to paint
// themselves.
func (v *view) doRedirs() {
	if len(v.redirWait) == 0 {
		return
	}
	now := time.Now()
	kept := v.redirWait[:0]
	for _, r := range v.redirWait {
		if now.Before(r.until) {
			kept = append(kept, r)
			continue
		}
		v.redirectNow(r.win)
	}
	v.redirWait = kept
}

// redirPainted is called when an application has painted itself: its window
// can be redirected now.
func (v *view) redirPainted(d C.Drawable) {
	for i, r := range v.redirWait {
		if C.Drawable(r.win) == d {
			v.redirWait = append(v.redirWait[:i], v.redirWait[i+1:]...)
			v.redirectNow(r.win)
			return
		}
	}
}

// redirectNow redirects a window and hands its content to the board.
func (v *view) redirectNow(win C.Window) {
	if C.lythrum_redirect(v.dpy, win) == 0 {
		return
	}
	for i := range v.wins {
		if v.wins[i].win == win {
			v.wins[i].redir = true
		}
	}
	v.placeWindows()
	v.redraw = true
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
		be := (*C.XButtonEvent)(unsafe.Pointer(xev))
		v.evTime = be.time
		if be.window == v.pop {
			b := *be
			px, py := v.popPoint(int(be.x), int(be.y))
			b.x, b.y = C.int(px), C.int(py)
			v.onPress(&b)
			return
		}
		v.onPress(be)
	case C.ButtonRelease:
		be := (*C.XButtonEvent)(unsafe.Pointer(xev))
		v.evTime = be.time
		if be.window == v.pop {
			b := *be
			px, py := v.popPoint(int(be.x), int(be.y))
			b.x, b.y = C.int(px), C.int(py)
			v.onRelease(&b)
			return
		}
		v.onRelease(be)
	case C.MotionNotify:
		me := (*C.XMotionEvent)(unsafe.Pointer(xev))
		v.evTime = me.time
		if me.window == v.pop {
			m := *me
			px, py := v.popPoint(int(me.x), int(me.y))
			m.x, m.y = C.int(px), C.int(py)
			v.onMotion(&m)
			return
		}
		v.onMotion(me)
	case C.KeyPress:
		ke := (*C.XKeyEvent)(unsafe.Pointer(xev))
		v.evTime = ke.time
		if ke.window == v.pop {
			k := *ke
			k.window = v.win
			v.onKey(&k)
			return
		}
		v.onKey(ke)
	case C.KeyRelease:
		v.onKeyRelease((*C.XKeyEvent)(unsafe.Pointer(xev)))
	case C.XDamageNotify:
		// The application repainted itself: refresh its miniature.
		v.redraw = true
		// A window that paints itself for the first time can be redirected
		// now: the board would keep the content of the screen otherwise.
		if de, ok := (*C.XDamageNotifyEvent)(unsafe.Pointer(xev)), true; ok {
			v.redirPainted(C.Drawable(de.drawable))
		}
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
		if ce.message_type == atomXdndEnter ||
			ce.message_type == atomXdndPosition ||
			ce.message_type == atomXdndLeave ||
			ce.message_type == atomXdndDrop {
			v.onXDND(ce)
		}
	case C.SelectionNotify:
		v.onSelectionNotify((*C.XSelectionEvent)(unsafe.Pointer(xev)))
	case C.SelectionRequest:
		// An application asks for the files the board drags.
		v.onSelectionRequest((*C.XSelectionRequestEvent)(unsafe.Pointer(xev)))
	case C.GenericEvent:
		v.onXIEvent(xev)
	}
}

func (v *view) onPress(be *C.XButtonEvent) {
	x, y := int(be.x), int(be.y)

	// A screen that emulates the clicks sends a button event for every
	// touch. The touch gestures already handle the fingers, so these events
	// are dropped.
	if v.gesturesOK && !v.touchAt.IsZero() && time.Since(v.touchAt) < 120*time.Millisecond {
		return
	}

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

	// A press on a miniature belongs to the application it previews, not to
	// the board: raise it, give it the keyboard and re-send the click at the
	// matching point of the real window behind the miniature.
	if be.button == 1 || be.button == 4 || be.button == 5 {
		if t := v.contentWinAt(x, y); t >= 0 {
			w := &v.wins[t]
			lx, ly := v.thumbLocal(w, x, y)
			if v.kbFocus != w.win {
				v.kbFocus = w.win
				C.XSetInputFocus(v.dpy, w.win, C.PointerRoot, C.CurrentTime)
			}
			v.raiseWindow(w.win)
			switch be.button {
			case 1:
				C.XGrabPointer(v.dpy, v.win, C.False,
					C.ButtonPressMask|C.ButtonReleaseMask|C.PointerMotionMask,
					C.GrabModeAsync, C.GrabModeAsync, 0, 0, C.CurrentTime)
				v.thumbP = t
				v.synthButton(w, int(be.button), true, lx, ly,
					int(be.x_root), int(be.y_root), uint(be.state))
			default:
				v.synthButton(w, int(be.button), true, lx, ly,
					int(be.x_root), int(be.y_root), uint(be.state))
			}
			return
		}
	}
	// A press outside every miniature returns keyboard focus to the board.
	if v.kbFocus != v.win {
		v.kbFocus = v.win
		C.XSetInputFocus(v.dpy, v.win, C.PointerRoot, C.CurrentTime)
	}

	switch be.button {
	case 1:
		if v.setOpen && v.setWin != nil {
			x0, y0, sw, sh := v.winRect(v.setWin)
			if x >= x0 && x < x0+sw && y >= y0 && y < y0+sh {
				v.setFocus = true
				v.settingsClick(x, y)
				v.redraw = true
				return
			}
			v.setFocus = false
		}
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
		if v.setOpen && v.setWin != nil {
			if _, _, ok := v.settingsPoint(x, y); ok {
				v.settingsScroll(-1)
				return
			}
		}
		switch {
		case be.state&C.ShiftMask != 0:
			v.ox -= panWheel * v.wheelSpeed() / v.zoom
		case be.state&C.ControlMask != 0:
			v.oy -= panWheel * v.wheelSpeed() / v.zoom
		default:
			v.zoomWheel(x, y, 1)
		}
		v.redraw = true
	case 5:
		if v.setOpen && v.setWin != nil {
			if _, _, ok := v.settingsPoint(x, y); ok {
				v.settingsScroll(1)
				return
			}
		}
		switch {
		case be.state&C.ShiftMask != 0:
			v.ox += panWheel * v.wheelSpeed() / v.zoom
		case be.state&C.ControlMask != 0:
			v.oy += panWheel * v.wheelSpeed() / v.zoom
		default:
			v.zoomWheel(x, y, -1)
		}
		v.redraw = true
	}
}

func (v *view) onRelease(be *C.XButtonEvent) {
	if v.thumbP >= 0 {
		if be.button == 1 {
			w := &v.wins[v.thumbP]
			lx, ly := v.thumbLocal(w, int(be.x), int(be.y))
			v.synthButton(w, 1, false, lx, ly, int(be.x_root), int(be.y_root),
				uint(be.state))
			v.thumbP = -1
			C.XUngrabPointer(v.dpy, C.CurrentTime)
		}
		return
	}
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
	if os.Getenv("LYTHRUM_DEBUG_DND") == "1" {
		fmt.Printf("dnd: drag begin at %d,%d\n", v.pressX, v.pressY)
	}
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
	// The board offers the dragged files to the application under the
	// pointer: another file manager may take them.
	var paths []string
	for i := range v.selset {
		if i >= 0 && i < len(v.icons) {
			paths = append(paths, v.icons[i].path)
		}
	}
	v.xdndStart(paths)
}

func (v *view) endIconDrag() {
	v.iconDrag = false
	v.orig = nil
	C.XUngrabPointer(v.dpy, C.CurrentTime)
	target := v.dropTarget
	v.dropTarget = -1
	if os.Getenv("LYTHRUM_DEBUG_DND") == "1" {
		fmt.Printf("dnd: drag end, target %d\n", target)
	}
	if target >= 0 {
		// The icons were dropped on a folder: they move inside it.
		v.xdndStop()
		v.dropIntoFolder(target)
		return
	}
	// Another application may have taken the files: the board offers them.
	var rx, ry C.int
	C.XQueryPointer(v.dpy, C.XDefaultRootWindow(v.dpy), nil, nil,
		&rx, &ry, nil, nil, nil)
	v.xdndRelease(int(rx), int(ry))
	if v.xds.active {
		// The target asks for the files: the board keeps the selection
		// until the target is done with it.
		v.redraw = true
		return
	}
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

	// While a miniature is pressed, the motion feeds the application behind
	// it (drag-select, hover), translated to the real window coordinates.
	if v.thumbP >= 0 {
		w := &v.wins[v.thumbP]
		if me.state&C.Button1Mask == 0 {
			lx, ly := v.thumbLocal(w, x, y)
			v.synthButton(w, 1, false, lx, ly, int(me.x_root), int(me.y_root),
				uint(me.state))
			v.thumbP = -1
			C.XUngrabPointer(v.dpy, C.CurrentTime)
			return
		}
		lx, ly := v.thumbLocal(w, x, y)
		v.synthMotion(w, lx, ly, int(me.x_root), int(me.y_root),
			uint(me.state)|C.Button1Mask)
		return
	}

	// Hovering over a miniature previews itself to the application, so hover
	// effects and tooltips keep working while zoomed out.
	if v.winDrag < 0 && v.winResize < 0 && !v.iconDrag && !v.band && !v.dragging {
		if t := v.contentWinAt(x, y); t >= 0 {
			w := &v.wins[t]
			lx, ly := v.thumbLocal(w, x, y)
			v.synthMotion(w, lx, ly, int(me.x_root), int(me.y_root), uint(me.state))
			return
		}
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
			// The grip resizes the application itself; the frame follows it,
			// pinned to cover the new size at the current zoom. The frame is
			// only clamped back into the viewport when the drag ends: reguing
			// the position on every motion would make the title bar jump.
			w.aw, w.ah = v.dragBW*s, v.dragBH*s
			aw, ah := int(math.Round(w.aw)), int(math.Round(w.ah))
			w.bw, w.bh = w.aw, w.ah
			w.selfReq = append(w.selfReq, [2]int{aw, ah})
			if len(w.selfReq) > 8 {
				w.selfReq = w.selfReq[1:]
			}
			C.XMoveResizeWindow(v.dpy, w.win, 0, 0, C.uint(aw), C.uint(ah))
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
		// The folder under the pointer is the target of the drop.
		if t := v.folderAt(x, y); t != v.dropTarget {
			v.dropTarget = t
		}
		// Another application may take the files: the board offers them.
		v.xdndMove(int(me.x_root), int(me.y_root))
		v.redraw = true
		return
	}
	if v.band {
		v.bandX1, v.bandY1 = x, y
		v.redraw = true
		return
	}
	if v.set != nil && v.set.drag >= 0 {
		if ux, _, ok := v.settingsPoint(x, y); ok {
			v.settingsSlider(ux)
		}
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
	if os.Getenv("LYTHRUM_DEBUG_KEYS") == "1" {
		fmt.Printf("key: state=0x%x keysym=0x%x\n", ke.state, uint(ks))
	}
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

	// The settings window takes the keyboard while it is focused.
	if v.setFocus && v.setOpen && C.Window(ke.window) == v.win {
		v.settingsKey(ke)
		return
	}

	// Super alone is the "back to the centre" gesture of the board. The press
	// is only remembered here: the board centres when the key is released
	// without a chord, so that Super+R still opens a terminal without moving
	// the board first.
	if uint(ks) == ksSuperL || uint(ks) == ksSuperR {
		v.superDown = true
		v.superChord = false
		return
	}
	if v.superDown {
		v.superChord = true
	}

	mods := uint32(ke.state) & (maskShift | maskControl | maskMod1 | maskMod4 | maskMod5)
	action := v.actionFor(uint32(ks), mods)
	if action == "" {
		return
	}
	v.runAction(action, mods)
}

// onKeyRelease reacts to a key being let go. Releasing Super without a chord
// brings the board back to its centre.
func (v *view) onKeyRelease(ke *C.XKeyEvent) {
	ks := uint32(C.XLookupKeysym(ke, 0))
	if os.Getenv("LYTHRUM_DEBUG_KEYS") == "1" {
		fmt.Printf("key up: state=0x%x keysym=0x%x\n", ke.state, uint(ks))
	}
	if ks != ksSuperL && ks != ksSuperR {
		return
	}
	if !v.superDown {
		return
	}
	chord := v.superChord
	v.superDown = false
	v.superChord = false
	if chord {
		return
	}
	if v.actionFor(ks, 0) == actCenter {
		if os.Getenv("LYTHRUM_DEBUG_KEYS") == "1" {
			fmt.Println("action: center")
		}
		v.centerOrigin()
		v.redraw = true
	}
}

// runAction performs the action a key combination is bound to.
func (v *view) runAction(action string, mods uint32) {
	cx, cy := float64(v.w)/2, float64(v.h)/2
	speed := v.cfg.Mouse.PanSpeed
	if speed <= 0 {
		speed = 1
	}
	switch action {
	case actCenter:
		v.centerOrigin()
	case actTerminal:
		v.launchTerminal()
	case actSettings:
		v.openSettings()
	case actWidgets:
		v.openWidgetPicker()
	case actFit:
		v.fit()
	case actReload:
		v.reload(false)
	case actRestart:
		v.restart()
	case actZoom100:
		v.zoomAt(cx, cy, 1/v.zoom)
	case actZoomIn:
		v.zoomAt(cx, cy, math.Pow(zKey, v.zoomSpeed()))
	case actZoomOut:
		v.zoomAt(cx, cy, math.Pow(zKey, -v.zoomSpeed()))
	case actQuit:
		C.XCloseDisplay(v.dpy)
		os.Exit(0)
	case actSwitch:
		v.cycleWindows(mods&maskShift != 0)
	case actClose:
		v.closeActive()
	case actDelete:
		v.deleteSelection()
	case actRename:
		v.renameSelection()
	case actCopy:
		v.copySelection()
	case actCut:
		v.cutSelection()
	case actPaste:
		v.paste()
	case actPanLeft:
		v.ox -= panKey * speed / v.zoom
	case actPanRight:
		v.ox += panKey * speed / v.zoom
	case actPanUp:
		v.oy -= panKey * speed / v.zoom
	case actPanDown:
		v.oy += panKey * speed / v.zoom
	case actPageUp:
		v.oy -= float64(v.h) / v.zoom
	case actPageDown:
		v.oy += float64(v.h) / v.zoom
	case actEscape:
		// Escape only clears the selection: the board is never left by
		// accident. The key q closes it.
		v.selset = map[int]bool{}
		v.anchor = -1
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
		items = append(items, menuItem{label: "Файловый менеджер", do: v.openFileManager})
		items = append(items, menuItem{label: "Параметры", do: v.openSettings})
		items = append(items, menuItem{label: "Виджеты", do: v.openWidgetPicker})
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

// boardOrigin returns the place of the board on the screen.
func (v *view) boardOrigin() (int, int) {
	var rx, ry C.int
	var child C.Window
	C.XTranslateCoordinates(v.dpy, C.XDefaultRootWindow(v.dpy), v.win,
		0, 0, &rx, &ry, &child)
	return int(rx), int(ry)
}

// openPopup raises the window of the interface of the board and gives it the
// size of the piece of interface it has to hold. The window is a child of the
// root, so it lies above every application on the board.
func (v *view) openPopup(x, y, w, h int, c color) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	bx, by := v.boardOrigin()
	if v.pop == 0 {
		v.pop = C.XCreateSimpleWindow(v.dpy, C.XDefaultRootWindow(v.dpy),
			C.int(bx+x), C.int(by+y), C.uint(w), C.uint(h), 0, 0,
			C.ulong(pixel(c)))
		v.popGC = C.XCreateGC(v.dpy, v.pop, 0, nil)
		v.popFt = C.XftDrawCreate(v.dpy, C.Drawable(v.pop), v.visual, v.cmap)
		C.XSelectInput(v.dpy, v.pop, C.ExposureMask|C.ButtonPressMask|
			C.ButtonReleaseMask|C.PointerMotionMask|C.KeyPressMask)
	} else {
		if w != v.popW || h != v.popH {
			C.XResizeWindow(v.dpy, v.pop, C.uint(w), C.uint(h))
		}
		if x != v.popX || y != v.popY {
			C.XMoveWindow(v.dpy, v.pop, C.int(bx+x), C.int(by+y))
		}
	}
	v.popX, v.popY, v.popW, v.popH = x, y, w, h
	C.XRaiseWindow(v.dpy, v.pop)
	C.XMapWindow(v.dpy, v.pop)
	C.XFlush(v.dpy)
}

// closePopup hides the window of the interface of the board.
func (v *view) closePopup() {
	if v.pop == 0 {
		return
	}
	C.XUnmapWindow(v.dpy, v.pop)
	C.XFlush(v.dpy)
}

// popFill fills a rectangle of the window of the interface of the board.
func (v *view) popFill(x, y, w, h int, c color) {
	if v.pop == 0 {
		return
	}
	C.XSetForeground(v.dpy, v.popGC, C.ulong(pixel(c)))
	C.XFillRectangle(v.dpy, v.pop, v.popGC, C.int(x), C.int(y),
		C.uint(w), C.uint(h))
}

// popLine draws a line in the window of the interface of the board.
func (v *view) popLine(c color, x0, y0, x1, y1 int) {
	if v.pop == 0 {
		return
	}
	C.XSetForeground(v.dpy, v.popGC, C.ulong(pixel(c)))
	C.XDrawLine(v.dpy, v.pop, v.popGC, C.int(x0), C.int(y0), C.int(x1), C.int(y1))
}

// popRect draws the frame of a rectangle in the window of the interface of
// the board.
func (v *view) popRect(c color, x, y, w, h int) {
	v.popLine(c, x, y, x+w, y)
	v.popLine(c, x, y+h, x+w, y+h)
	v.popLine(c, x, y, x, y+h)
	v.popLine(c, x+w, y, x+w, y+h)
}

// popText writes a line of text in the window of the interface of the board.
func (v *view) popText(x, y int, s string, c color) {
	if v.popFt == nil || v.xftFont == nil {
		return
	}
	xc := v.xftColor(c)
	if xc == nil {
		return
	}
	cs := C.CString(s)
	C.XftDrawStringUtf8(v.popFt, xc, v.xftFont, C.int(x), C.int(y),
		(*C.FcChar8)(unsafe.Pointer(cs)), C.int(len(s)))
	C.free(unsafe.Pointer(cs))
}

// popPoint turns the place of an event of the popup into the place of the
// same event in the coordinates of the board.
func (v *view) popPoint(x, y int) (int, int) {
	bx, by := v.boardOrigin()
	return x + v.popX - bx, y + v.popY - by
}

// raisePopup puts the window of the interface of the board above the
// applications: the board raises the floating windows every frame.
func (v *view) raisePopup() {
	if v.pop == 0 {
		return
	}
	C.XRaiseWindow(v.dpy, v.pop)
}

func (v *view) drawMenu() {
	if !v.menuOpen {
		v.closePopup()
		return
	}
	x, y, w, h := v.menuGeom()
	v.openPopup(x, y, w, h, menuBg)
	v.popFill(0, 0, w, h, menuBg)
	v.popRect(menuBorder, 0, 0, w-1, h-1)
	cy := 3
	for i, it := range v.menu {
		if it.sep {
			v.popLine(menuBorder, 10, cy+menuSep/2, w-10, cy+menuSep/2)
			cy += menuSep
			continue
		}
		if i == v.menuHover {
			v.popFill(2, cy, w-4, menuRow, selBg)
		}
		v.popText(16, cy+16, it.label, textCol)
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
	v.openPopup(x, y, w, h, menuBg)
	v.popFill(0, 0, w, h, menuBg)
	v.popRect(menuBorder, 0, 0, w-1, h-1)
	v.popText(18, 28, v.promptTitle, textCol)
	ix, iy, iw, ih := 18, 44, w-36, 30
	v.popFill(ix, iy, iw, ih, panelBg)
	v.popRect(selBorder, ix, iy, iw-1, ih-1)
	v.popText(ix+8, iy+20, v.promptText, textCol)
	cx := ix + 8 + v.textWidth(v.promptText) + 1
	v.popLine(textCol, cx, iy+6, cx, iy+ih-6)
	v.popText(18, h-16, "Enter — OK, Esc — отмена", hudCol)
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
	if ic.path == settingsPathMarker {
		v.openSettings()
		return
	}
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
		fmt.Fprintln(os.Stderr, "lythrum: readdir:", err)
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
	// The settings application is always on the desktop.
	if ic, ok := settingsIcon(); ok {
		apps = append(apps, ic)
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
	return filepath.Join(dir, "lythrum", "positions.json")
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
	if t := os.Getenv("LYTHRUM_TERMINAL"); t != "" {
		argv = strings.Fields(t)
	} else {
		argv = terminalCommand()
	}
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "lythrum: no terminal found")
		return
	}
	v.spawn(argv, cx, cy)
}

// openFileManager opens the file manager of the shell on the desktop.
func (v *view) openFileManager() {
	cx := v.ox + float64(v.w)/(2*v.zoom)
	cy := v.oy + float64(v.h)/(2*v.zoom)
	argv := filemanCommand(v.root)
	if argv == nil {
		v.setStatus("Файловый менеджер не установлен")
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
		fmt.Fprintln(os.Stderr, "lythrum: launch:", err)
		return
	}
	go func() { _ = cmd.Wait() }()
	fmt.Printf("lythrum: launched %s\n", strings.Join(argv, " "))
}

// startSession fills a freshly started desktop session: LYTHRUM_SESSION_APPS
// lists the programs to open (one per ';'), and with no list a terminal opens
// so the board is immediately usable.
func (v *view) startSession() {
	apps := strings.TrimSpace(os.Getenv("LYTHRUM_SESSION_APPS"))
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
		fmt.Fprintln(os.Stderr, "lythrum: restart:", err)
		return
	}
	C.XCloseDisplay(v.dpy)
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = os.Environ()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "lythrum: restart:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// filemanCommand returns the command of the file manager of the shell, or
// nothing when it is not installed.
func filemanCommand(dir string) []string {
	names := []string{"lythrum-fileman", "fileman"}
	if exe, err := os.Executable(); err == nil {
		home := filepath.Dir(exe)
		names = append([]string{filepath.Join(home, "fileman"),
			filepath.Join(home, "lythrum-fileman")}, names...)
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return []string{p, dir}
		}
	}
	return nil
}

// appCommand prefers a real file manager for folders; LYTHRUM_FILEMANAGER can
// override it (useful for testing without a desktop session).
func appCommand(target string, isDir bool) []string {
	if isDir {
		if fm := os.Getenv("LYTHRUM_FILEMANAGER"); fm != "" {
			return append(strings.Fields(fm), target)
		}
		// The file manager of the shell comes first: it lives on the board
		// like every other application.
		if cmd := filemanCommand(target); cmd != nil {
			return cmd
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
// LYTHRUM_XKB_LAYOUT disables it; the defaults give US + Russian with two
// common toggle keys.
func keyboardConfig() (layout, variant, options string) {
	layout = os.Getenv("LYTHRUM_XKB_LAYOUT")
	if layout == "" {
		layout = "us,ru"
	}
	variant = os.Getenv("LYTHRUM_XKB_VARIANT")
	options = os.Getenv("LYTHRUM_XKB_OPTIONS")
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
		fmt.Fprintln(os.Stderr, "lythrum: setxkbmap not found, keyboard layouts left alone")
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
		fmt.Fprintf(os.Stderr, "lythrum: setxkbmap: %v: %s\n", err, strings.TrimSpace(string(out)))
		return false
	}
	fmt.Printf("lythrum: keyboard layouts %s (%s)\n", layout, options)
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
	if win == v.win || v.adopted[win] {
		return
	}
	// Skip the board's own containers and managed children: their maps are
	// echoed by the SubstructureNotifyMask selection.
	for i := range v.wins {
		if v.wins[i].cont == win || v.wins[i].win == win {
			return
		}
	}
	// The toolkit of the widgets opens helper windows of its own: they are
	// not applications and must not be adopted.
	if !time.Now().After(v.widgetUntil) {
		return
	}
	p := v.pending
	if p != nil && p.before[win] {
		return
	}
	cx := v.ox + float64(v.w)/(2*v.zoom)
	cy := v.oy + float64(v.h)/(2*v.zoom)
	if p != nil && !time.Now().After(p.until) {
		cx, cy = p.cx, p.cy
		// A launch can open several windows (main window plus dialogs); with no
		// window manager they would otherwise pile up unmanaged and unclose at
		// all, so each companion is pinned right under the previous one.
		cy += float64(p.n) * 60
		p.n++
	}
	v.adopt(win, cx, cy)
}

// maxWinSize is the largest on-screen size a managed window may take. It is
// deliberately smaller than the viewport: applications without a window
// manager often open "maximised", and a window covering the whole board would
// hide its own title bar and the close button, leaving no free board to pan on.
func (v *view) maxWinSize() (w, h float64) {
	w = float64(v.w) - 2*winGrip - 40
	h = float64(v.h) - float64(2*(winTitleH+winGrip))
	if w < 80 {
		w = 80
	}
	if h < 60 {
		h = 60
	}
	return
}

// capWin caps a window's board size, so that on screen it never grows past
// maxWinSize, and keeps it inside the viewport, so its title bar and close
// button always stay reachable.
func (v *view) capWin(w *managedWin) {
	mw, mh := v.maxWinSize()
	if w.bw*v.zoom > mw {
		w.bw = mw / v.zoom
	}
	if w.bh*v.zoom > mh {
		w.bh = mh / v.zoom
	}
	v.clampPos(w)
}

// clampPos nudges a window back so its frame stays on screen.
func (v *view) clampPos(w *managedWin) {
	_, _, sw, sh := v.winRect(w)
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
		if cw <= 0 || ch <= 0 {
			return
		}
		// The board moves the application itself (into its frame or out of
		// it): only the place changed, the size is the same. Such a move is
		// not a resize of the application and must not resize its frame.
		if cw == int(math.Round(w.aw)) && ch == int(math.Round(w.ah)) {
			return
		}
		// First, drop the echoes of resizes the board made itself: after a
		// burst of resizes (drag, zoom) an echo can arrive late and report an
		// outdated size, which would otherwise flap the window back and forth.
		for i, r := range w.selfReq {
			if r[0] == cw && r[1] == ch {
				w.selfReq = append(w.selfReq[:i], w.selfReq[i+1:]...)
				return
			}
		}
		// While the grip is resizing the window the application keeps refusing
		// or echoing sizes; the final one wins after the drag ends.
		if v.winResize >= 0 && v.wins[v.winResize].win == w.win {
			return
		}
		// The application resized itself: record its real pixel size and pin
		// the frame over it, so the board shows the new size at this zoom.
		w.aw, w.ah = float64(cw), float64(ch)
		w.bw, w.bh = w.aw, w.ah
		v.capWin(w)
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
	// The frame of the window is a plane of the lythrum: its size is fixed
	// in board units (the pixels of the application) and it scales with the
	// zoom of the board, like every other object of the canvas.
	bw := float64(attr.width)
	bh := float64(attr.height)
	// Containers clip the application to the frame: the app keeps its own
	// pixel size, the viewport only scales the frame around it. The container
	// has no background, so the miniature the board paints shows through it.
	cont := C.lythrum_container(v.dpy, v.win, C.uint(attr.width),
		C.uint(attr.height))
	C.XMapWindow(v.dpy, cont)
	// The application can render into a pixmap, which the board then draws
	// scaled in the frame of the window. Not every server allows it: under a
	// compositor the windows of the clients are already redirected, and the
	// window then has to stay a window of its own, floating over the board.
	redir := false
	if C.lythrum_redirect(v.dpy, win) != 0 && C.lythrum_composite_broken() == 0 {
		// The application is redirected a moment later: it has to paint
		// itself before the redirect, or the board would keep what was on
		// the screen instead of the content of the application.
		v.redirWait = append(v.redirWait, redirWait{win: win, until: time.Now().Add(3 * time.Second)})
		go func() {
			time.Sleep(3 * time.Second)
			wake()
		}()
		redir = true
	}
	if redir {
		C.XReparentWindow(v.dpy, win, cont, 0, 0)
		C.XSetWindowBorderWidth(v.dpy, win, 0)
		C.XMapWindow(v.dpy, win)
	}
	w := managedWin{
		win:   win,
		cont:  cont,
		title: windowTitle(v.dpy, win),
		vis:   attr.visual,
		redir: false, // the application is redirected a moment later
		float: !redir,
		bx:    cx - bw/2,
		by:    cy - bh/2,
		bw:    bw,
		bh:    bh,
		aw:    float64(attr.width),
		ah:    float64(attr.height),
		lastX: -1 << 30, lastY: -1 << 30, lastW: -1, lastH: -1,
	}
	if redir {
		// The application stays live in its frame until the redirect: it
		// paints itself while the board waits.
		C.XMoveWindow(v.dpy, win, 0, 0)
	}
	if v.dmgOK {
		w.dmg = C.XDamageCreate(v.dpy, win, C.XDamageReportNonEmpty)
	}
	if kind, ok := v.widgetKinds[C.ulong(win)]; ok {
		w.widget = true
		w.widgetKind = kind
		w.widgetXID = C.ulong(win)
	}
	v.adopted[win] = true
	v.wins = append(v.wins, w)
	v.capWin(&v.wins[len(v.wins)-1])
	v.placeWindows()
	v.redraw = true
	fmt.Printf("lythrum: adopted window 0x%x %dx%d\n", uint64(win), attr.width, attr.height)
}

func (v *view) forget(win C.Window) {
	for i := range v.wins {
		if v.wins[i].win == win {
			// The container dies with the application: destroying it also
			// removes the child window from the display.
			if v.thumbP == i {
				v.thumbP = -1
			} else if v.thumbP > i {
				v.thumbP--
			}
			delete(v.adopted, win)
			if v.kbFocus == win {
				v.kbFocus = v.win
				C.XSetInputFocus(v.dpy, v.win, C.PointerRoot, C.CurrentTime)
			}
			C.XDestroyWindow(v.dpy, v.wins[i].cont)
			v.wins = append(v.wins[:i], v.wins[i+1:]...)
			v.redraw = true
			return
		}
	}
	delete(v.adopted, win)
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
		C.XRaiseWindow(v.dpy, w.cont)
		C.XFlush(v.dpy)
		v.redraw = true
		return
	}
}

// winRect is the on-screen rectangle of a managed window's frame/container: its
// board size scaled by the viewport zoom, clamped to the limits the frame
// itself is given (never smaller than 80x60, never larger than maxWinSize).
// The title bar, the grip and the container are built around the same rect so
// they never disagree.
func (v *view) winRect(w *managedWin) (x, y, sw, sh int) {
	x = v.sx(w.bx)
	y = v.sy(w.by)
	fw, fh := w.bw*v.zoom, w.bh*v.zoom
	if mw, mh := v.maxWinSize(); fw > mw || fh > mh {
		if fw > mw {
			fw = mw
		}
		if fh > mh {
			fh = mh
		}
	}
	if fw < 80 {
		fw = 80
	}
	if fh < 60 {
		fh = 60
	}
	return x, y, int(math.Round(fw)), int(math.Round(fh))
}

func (v *view) titlePx() int {
	return winTitleH
}

func (v *view) gripPx() int {
	return winGrip
}

// winMini tells whether the content of a window is a picture the board
// paints: the application is redirected, so the server keeps no content of
// its own and the board draws the application in its frame.
func (v *view) winMini(w *managedWin) bool {
	return w.redir && C.lythrum_composite_broken() == 0
}

// placeWindows keeps every managed window frame pinned to its board position
// and scaled to its board size times the viewport zoom: frames follow the
// canvas exactly like the icons do. The content inside the frame does not
// scale with it: the redirected application stays parked in the container's
// top-left corner at its own pixel size, the board paints it one to one, and
// the container clips whatever does not fit.
func (v *view) placeWindows() {
	changed := false
	for i := range v.wins {
		w := &v.wins[i]
		x, y, sw, sh := v.winRect(w)
		if w.float {
			// The window follows the board, right under its title bar. It is
			// a window of its own, so it is placed in the coordinates of the
			// screen, and it is raised above the board: the board paints
			// itself over the whole of its window, and would hide it.
			var rx, ry C.int
			var child C.Window
			C.XTranslateCoordinates(v.dpy, C.XDefaultRootWindow(v.dpy), v.win,
				0, 0, &rx, &ry, &child)
			C.XMoveWindow(v.dpy, w.win, C.int(rx)+C.int(x), C.int(ry)+C.int(y))
			C.XRaiseWindow(v.dpy, w.win)
			w.lastX, w.lastY, w.lastW, w.lastH = x, y, sw, sh
			changed = true
			continue
		}
		// A redirected application renders into a pixmap the board draws
		// in the frame of the window. It is parked inside the container,
		// past its clip: the server keeps the whole content in the pixmap,
		// and the screen shows nothing of it. The board is the only painter
		// of the content: the window is a plane of the canvas.
		mini := v.winMini(w)
		moved := x != w.lastX || y != w.lastY ||
			sw != w.lastW || sh != w.lastH
		if mini != w.tmb {
			w.tmb = mini
			moved = true
			v.redraw = true
		}
		if !moved {
			continue
		}
		C.XMoveResizeWindow(v.dpy, w.cont, C.int(x), C.int(y), C.uint(sw), C.uint(sh))
		// A redirected application is not painted by the server: the board
		// paints it in its frame. It stays in the corner of the container,
		// where the server keeps rendering it into its storage.
		C.XMoveWindow(v.dpy, w.win, 0, 0)
		w.lastX, w.lastY, w.lastW, w.lastH = x, y, sw, sh
		changed = true
	}
	if changed {
		C.XFlush(v.dpy)
	}
}

// bootComposite checks the extensions the miniatures need. Without them the
// board falls back to clipping the live application inside its frame.
func (v *view) bootComposite() {
	var cmajor, cminor C.int
	if C.XCompositeQueryVersion(v.dpy, &cmajor, &cminor) != C.True {
		fmt.Fprintln(os.Stderr, "lythrum: composite extension unavailable, windows clip instead of scaling")
		return
	}
	C.lythrum_set_composite_opcode(v.dpy)
	// The redirect is verified lazily: XCompositeNameWindowPixmap in childBrush
	// skips miniatures whose backing pixmap cannot be fetched.
	v.compOK = true
	var dmajor, dminor C.int
	if C.XDamageQueryVersion(v.dpy, &dmajor, &dminor) != C.True {
		fmt.Fprintln(os.Stderr, "lythrum: damage extension unavailable, miniatures refresh on redraw only")
		return
	}
	v.dmgOK = true
}

// childBrush refreshes the backing pixmap and its render picture after the
// application has resized itself: the composite backing pixmap dies with every
// geometry change.
func (v *view) childBrush(w *managedWin) {
	if !v.compOK {
		return
	}
	awn, ahn := int(math.Round(w.aw)), int(math.Round(w.ah))
	if w.cpic != 0 && w.cpw == awn && w.cph == ahn {
		return
	}
	if w.cpic != 0 {
		C.XRenderFreePicture(v.dpy, w.cpic)
		C.XFreePixmap(v.dpy, w.cpm)
		w.cpic, w.cpm, w.cpw, w.cph = 0, 0, 0, 0
	}
	pm := C.XCompositeNameWindowPixmap(v.dpy, w.win)
	if pm == 0 {
		return
	}
	format := C.XRenderFindVisualFormat(v.dpy, w.vis)
	if format == nil {
		C.XFreePixmap(v.dpy, pm)
		return
	}
	w.cpic = C.XRenderCreatePicture(v.dpy, pm, format, 0, nil)
	w.cpm = pm
	w.cpw, w.cph = awn, ahn
}

// drawContent paints the content of an application in its frame. The frame is
// a plane of the lythrum and scales with the zoom of the board, but the
// content does not: it is painted at the size the application itself renders,
// one screen pixel per application pixel. Whatever falls outside the frame is
// clipped.
func (v *view) drawContent(w *managedWin) {
	if w.virtual {
		return
	}
	v.childBrush(w)
	if w.cpic == 0 || v.dpict == 0 {
		return
	}
	x, y, sw, sh := v.winRect(w)
	awn, ahn := int(math.Round(w.aw)), int(math.Round(w.ah))
	if sw <= 0 || sh <= 0 || awn <= 0 || ahn <= 0 {
		return
	}
	// The content is copied one to one, without a transform: the zoom moves
	// the frame, it never scales the pixels of the application.
	cw, ch := sw, sh
	if cw > awn {
		cw = awn
	}
	if ch > ahn {
		ch = ahn
	}
	C.XRenderComposite(v.dpy, C.PictOpSrc, w.cpic, 0, v.dpict,
		0, 0, 0, 0,
		C.int(x), C.int(y), C.uint(cw), C.uint(ch))
}

// watchDir reports changes of the desktop folder, so files added from outside
// (another file manager, a browser download, a shell redirect) appear on the
// board without a manual refresh.
func watchDir(root string, out chan<- int) {
	fd, err := syscall.InotifyInit1(0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lythrum: inotify:", err)
		return
	}
	mask := syscall.IN_CREATE | syscall.IN_DELETE | syscall.IN_MOVED_TO |
		syscall.IN_MOVED_FROM | syscall.IN_CLOSE_WRITE | syscall.IN_MOVED_TO
	if _, err := syscall.InotifyAddWatch(fd, root, uint32(mask)); err != nil {
		fmt.Fprintln(os.Stderr, "lythrum: inotify add:", err)
		return
	}
	buf := make([]byte, 4096)
	for {
		n, err := syscall.Read(fd, buf)
		if err != nil {
			return
		}
		if n <= 0 {
			continue
		}
		// Coalesce a burst of events into one reload; the channel drops what
		// the board has not consumed yet.
		select {
		case out <- 1:
		default:
		}
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
			if x >= x0+sw-winClose {
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
	// The resize grip scales the application's own pixel size: the frame is
	// re-pinned over it afterwards.
	v.dragSW, v.dragSH = int(math.Round(w.aw)), int(math.Round(w.ah))
	v.dragBW, v.dragBH = w.aw, w.ah
	switch part {
	case partTitle:
		v.winDrag = i
	case partGrip:
		v.winResize = i
	}
}

func (v *view) stopWinDrag() { // Clamp the frame only now: during the resize it jumped around with every
	// motion, which made the title bar twitch above the grip.
	if v.winResize >= 0 && v.winResize < len(v.wins) {
		v.capWin(&v.wins[v.winResize])
	}
	v.winDrag, v.winResize = -1, -1
}

// contentWinAt reports the index of the window whose content lies under the
// screen point, or -1. The content of an application is a picture the board
// paints: the pointer belongs to the application, not to the board. Only the
// part of the content the frame shows counts: the frame may be larger than
// the application when the board is zoomed in.
func (v *view) contentWinAt(x, y int) int {
	for i := len(v.wins) - 1; i >= 0; i-- {
		w := &v.wins[i]
		if !w.redir || C.lythrum_composite_broken() != 0 {
			continue
		}
		fx, fy, fw, fh := v.winRect(w)
		awn, ahn := int(math.Round(w.aw)), int(math.Round(w.ah))
		if fw > awn {
			fw = awn
		}
		if fh > ahn {
			fh = ahn
		}
		if x >= fx && x < fx+fw && y >= fy && y < fy+fh {
			return i
		}
	}
	return -1
}

// thumbLocal maps a point on the content of a window to the corresponding
// point of the application's real window. The content is painted at its own
// pixel size, so the point only moves by the offset of the frame.
func (v *view) thumbLocal(w *managedWin, x, y int) (lx, ly int) {
	fx, fy, _, _ := v.winRect(w)
	awn, ahn := int(math.Round(w.aw)), int(math.Round(w.ah))
	lx = x - fx
	ly = y - fy
	if lx < 0 {
		lx = 0
	}
	if ly < 0 {
		ly = 0
	}
	if lx > awn-1 {
		lx = awn - 1
	}
	if ly > ahn-1 {
		ly = ahn - 1
	}
	return lx, ly
}

// synthButton re-delivers a button event to a miniaturized window at the
// position it holds behind the miniature. XSendEvent marks the events as
// synthetic; toolkits accept them for on-screen keyboards already.
func (v *view) synthButton(w *managedWin, btn int, press bool, lx, ly, rx, ry int, state uint) {
	var ev C.XButtonEvent
	ev._type = C.ButtonPress
	if !press {
		ev._type = C.ButtonRelease
	}
	ev.send_event = C.True
	ev.display = v.dpy
	ev.window = w.win
	ev.root = C.XDefaultRootWindow(v.dpy)
	ev.subwindow = 0
	ev.time = v.evTime
	ev.x, ev.y = C.int(lx), C.int(ly)
	ev.x_root, ev.y_root = C.int(rx), C.int(ry)
	ev.state = C.uint(state)
	ev.button = C.uint(btn)
	ev.same_screen = C.True
	C.XSendEvent(v.dpy, w.win, C.False,
		C.ButtonPressMask|C.ButtonReleaseMask|C.PointerMotionMask,
		(*C.XEvent)(unsafe.Pointer(&ev)))
	C.XFlush(v.dpy)
}

// synthMotion re-delivers pointer motion to a miniaturized window.
func (v *view) synthMotion(w *managedWin, lx, ly, rx, ry int, state uint) {
	var ev C.XMotionEvent
	ev._type = C.MotionNotify
	ev.send_event = C.True
	ev.display = v.dpy
	ev.window = w.win
	ev.root = C.XDefaultRootWindow(v.dpy)
	ev.subwindow = 0
	ev.time = v.evTime
	ev.is_hint = 0
	ev.x, ev.y = C.int(lx), C.int(ly)
	ev.x_root, ev.y_root = C.int(rx), C.int(ry)
	ev.state = C.uint(state)
	ev.same_screen = C.True
	C.XSendEvent(v.dpy, w.win, C.False,
		C.PointerMotionMask|C.ButtonMotionMask,
		(*C.XEvent)(unsafe.Pointer(&ev)))
	C.XFlush(v.dpy)
}

// closeWin asks an application to close its window: politely through
// WM_DELETE_WINDOW when it advertises the protocol, forcibly otherwise.
func (v *view) closeWin(i int) {
	w := &v.wins[i]
	if w.virtual {
		if w.title == "Настройки" {
			v.closeSettings()
		}
		return
	}
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

// drawBed paints the body of a managed window: the panel its frame stands
// on. The container of the window has no background of its own, so the panel
// shows through it wherever the application does not cover it.
func (v *view) drawBed(w *managedWin) {
	x, y, sw, sh := v.winRect(w)
	v.fill(x, y, sw, sh, panelBg)
}

// drawWins paints each managed window's frame: a title bar above it and a
// scaling grip just below its bottom-right corner. The look of the frame
// follows the active style: its corners may be rounded, its bar may carry a
// gradient, a bevel or the soft highlight of the glass styles.
func (v *view) drawWins() {
	th, g := v.titlePx(), v.gripPx()
	for i := range v.wins {
		w := &v.wins[i]
		x, y, sw, sh := v.winRect(w)

		bar := winTitleBg
		if i == len(v.wins)-1 {
			bar = winTitleOn
		}
		v.drawTitleBar(x, y-th, sw, th, bar)
		if w.title != "" {
			v.text(x+8, y-th+th-6, v.fitText(w.title, sw-winClose-16), winTitleFg)
		}

		// Close button at the right end of the title bar.
		cbx, cby := x+sw-winClose, y-th
		v.drawCloseButton(cbx, cby, winClose, th)

		gx := x + sw - g
		gy := y + sh
		v.drawGrip(gx, gy, g)
	}
}

// drawTitleBar paints the bar that carries the title of a window. The style
// decides its shape: the corners are rounded, the colour runs from a light
// shade to a dark one, the classic style raises the bar and the glass styles
// let the board shine through it.
func (v *view) drawTitleBar(x, y, w, h int, c color) {
	if w <= 0 || h <= 0 {
		return
	}
	top, bottom := c, c
	switch {
	case styleGradient:
		top, bottom = lighten(c, 0.35), darken(c, 0.20)
	case styleGlass:
		// The glass styles put a translucent panel over the board.
		base := mix(c, bg, 0.35)
		top, bottom = lighten(base, 0.45), darken(base, 0.10)
	}
	r := styleRadius
	if r > h/2 {
		r = h / 2
	}
	if r > w/2 {
		r = w / 2
	}
	if r <= 0 && top == bottom {
		v.fill(x, y, w, h, c)
	} else {
		v.fillRoundTop(x, y, w, h, r, top, bottom)
	}
	if styleGlass {
		// The soft highlight of the glass: a bright line along the top edge.
		v.fill(x+r, y, w-2*r, 1, lighten(top, 0.45))
	}
	if styleBevel {
		// The classic raised bar: the light comes from the top left.
		v.bevel(x, y, w, h, false)
	}
}

// drawCloseButton paints the button that closes a window.
func (v *view) drawCloseButton(x, y, w, h int) {
	r := styleRadius
	if r > h/2 {
		r = h / 2
	}
	if r > w/2 {
		r = w / 2
	}
	v.fillRound(x, y, w, h, r, winCloseBg)
	if styleBevel {
		v.bevel(x, y, w, h, false)
	}
	v.fg(winCloseFg)
	p := w / 4
	C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x+p), C.int(y+p),
		C.int(x+w-p), C.int(y+h-p))
	C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x+w-p), C.int(y+p),
		C.int(x+p), C.int(y+h-p))
}

// drawGrip paints the corner that scales a window.
func (v *view) drawGrip(x, y, g int) {
	r := styleRadius
	if r > g/2 {
		r = g / 2
	}
	v.fillRound(x, y, g, g, r, winGripCol)
	v.fg(bg)
	for k := 1; k <= 3; k++ {
		o := k * g / 4
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x+o), C.int(y+g),
			C.int(x+g), C.int(y+o))
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
			C.XRaiseWindow(v.dpy, v.wins[n-1].cont)
		} else {
			w := v.wins[0]
			copy(v.wins, v.wins[1:])
			v.wins[n-1] = w
			C.XRaiseWindow(v.dpy, w.cont)
		}
	} else {
		C.XRaiseWindow(v.dpy, v.wins[0].cont)
	}
	C.XFlush(v.dpy)
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
		fmt.Fprintln(os.Stderr, "lythrum: cannot create pixmap")
		os.Exit(1)
	}
	v.xftDraw = C.XftDrawCreate(v.dpy, C.Drawable(v.pm), v.visual, v.cmap)
	v.w, v.h = w, h
	// The destination picture rides on the same pixmap; rebuild it whenever
	// the pixmap is rebuilt.
	if v.dpict != 0 {
		C.XRenderFreePicture(v.dpy, v.dpict)
	}
	v.dpict = 0
	if fmt := C.XRenderFindVisualFormat(v.dpy, v.visual); fmt != nil {
		v.dpict = C.XRenderCreatePicture(v.dpy, v.pm, fmt, 0, nil)
	}
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
	// The settings window is drawn on its own canvas and scaled into its
	// frame, so that it follows the zoom of the board like every other
	// object of the board.
	v.renderSettings()
	// The body of every managed window and, over it, the content of the
	// application. The frames are painted over them.
	for i := range v.wins {
		v.drawBed(&v.wins[i])
		if v.wins[i].redir {
			v.drawContent(&v.wins[i])
		}
	}
	v.compositeSettings()
	v.drawWins()
	v.hud()
	v.drawMenu()
	v.drawPrompt()
	v.drawStatus()
	v.placeWindows()
	// The interface of the board lies above the applications.
	v.raisePopup()
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
	animActive.Store(false)
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
		if i == v.dropTarget {
			// The folder the dragged icons would fall into.
			v.fill(sx, sy, cw, ch, selBg)
			v.fg(selBorder)
			for k := 0; k < 3; k++ {
				C.XDrawRectangle(v.dpy, v.pm, v.gc,
					C.int(sx-k*2), C.int(sy-k*2),
					C.uint(cw+4*k), C.uint(ch+4*k))
			}
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
			img := ic.thumb
			if ic.anim != nil && v.cfg.Anim.GifEnabled {
				// The animation plays in the icon itself.
				img = ic.anim.frameAt(time.Now())
				animActive.Store(true)
			}
			if img != nil && tw >= 48 && v.depth == 24 {
				v.drawThumb(tx, ty, tw, img)
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
			col := hudCol
			if v.selset[i] {
				col = textCol
			}
			// A long name is broken into several lines, the room under the
			// icon decides how many of them fit.
			room := int(math.Round((cellH+gapY)*z)) - tw - 10
			lines := room / 11
			if lines < 1 {
				lines = 1
			}
			if lines > 3 {
				lines = 3
			}
			for k, line := range v.wrapText(name, cw-4, lines) {
				lw := v.textWidth(line)
				lx := sx + (cw-lw)/2
				if lx < sx {
					lx = sx
				}
				v.text(lx, ty+tw+9+k*11, line, col)
			}
		}
	}
}

// glyphVariant is the look of the icons: the style decides, unless the user
// has chosen another one in the settings.
func (v *view) glyphVariant() string {
	if v.cfg.Icons.Style != "" && v.cfg.Icons.Style != "auto" {
		return v.cfg.Icons.Style
	}
	return styleIconShape
}

func (v *view) folderGlyph(tx, ty, tw int, up bool) {
	m := tw / 8
	if m < 1 {
		m = 1
	}
	switch v.glyphVariant() {
	case "outline":
		// Only the contour of the folder.
		v.fg(folderCol)
		C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(tx+m), C.int(ty+tw/4),
			C.uint(tw-2*m-1), C.uint(tw*3/5-1))
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(tx+m), C.int(ty+tw/4),
			C.int(tx+m), C.int(ty+tw/5))
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(tx+m), C.int(ty+tw/5),
			C.int(tx+tw/2-m/2), C.int(ty+tw/5))
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(tx+tw/2-m/2), C.int(ty+tw/5),
			C.int(tx+tw/2-m/2), C.int(ty+tw/4))
	case "classic":
		// The folder of the old style: a light top and a dark bottom.
		v.fill(tx+m, ty+tw/5, tw/2-m/2, tw/9, folderCol)
		v.fill(tx+m, ty+tw/4, tw-2*m, tw*3/5, folderCol)
		v.fill(tx+m+1, ty+tw/4+1, tw-2*m-2, tw/12, lighten(folderCol, 0.45))
		v.fill(tx+m+1, ty+tw*3/5+tw/4-tw/14, tw-2*m-2, tw/14, darken(folderCol, 0.35))
	case "glass":
		v.fill(tx+m, ty+tw/5, tw/2-m/2, tw/9, folderCol)
		v.fill(tx+m, ty+tw/4, tw-2*m, tw*3/5, folderCol)
		v.fill(tx+m+2, ty+tw/4+2, tw-2*m-4, tw/8, lighten(folderCol, 0.55))
	default:
		v.fill(tx+m, ty+tw/5, tw/2-m/2, tw/9, folderCol)
		v.fill(tx+m, ty+tw/4, tw-2*m, tw*3/5, folderCol)
	}
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
	switch v.glyphVariant() {
	case "outline":
		v.fg(appGlyph)
		C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(tx+m), C.int(ty+m),
			C.uint(tw-2*m-1), C.uint(tw-2*m-1))
	case "classic":
		v.fill(tx+m, ty+m, tw-2*m, tw-2*m, appGlyph)
		v.fill(tx+m+1, ty+m+1, tw-2*m-2, tw/10, lighten(appGlyph, 0.5))
		v.fill(tx+m+1, ty+tw-m-tw/10, tw-2*m-2, tw/10, darken(appGlyph, 0.35))
	default:
		v.fill(tx+m, ty+m, tw-2*m, tw-2*m, appGlyph)
	}
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
	switch v.glyphVariant() {
	case "outline":
		v.fg(paperCol)
		C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(x0), C.int(y0),
			C.uint(bw-1), C.uint(bh-1))
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x0+bw-fold), C.int(y0),
			C.int(x0+bw-fold), C.int(y0+fold))
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x0+bw-fold), C.int(y0+fold),
			C.int(x0+bw), C.int(y0+fold))
		v.fill(x0+bw/8, y0+bh-bh/5, bw-bw/4, maxInt(bh/11, 2), c)
	case "classic":
		v.fill(x0, y0, bw, bh, paperCol)
		v.fill(x0+bw-fold, y0, fold, fold, bgc)
		v.fg(foldCol)
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x0+bw-fold), C.int(y0),
			C.int(x0+bw-fold), C.int(y0+fold))
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x0+bw-fold), C.int(y0+fold),
			C.int(x0+bw), C.int(y0+fold))
		v.fill(x0+1, y0+1, bw-2, maxInt(bh/12, 2), lighten(paperCol, 0.4))
		v.fill(x0+bw/8, y0+bh-bh/5, bw-bw/4, maxInt(bh/11, 2), c)
		v.fg(darken(foldCol, 0.4))
		C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(x0), C.int(y0),
			C.uint(bw-1), C.uint(bh-1))
	case "glass":
		v.fill(x0, y0, bw, bh, paperCol)
		v.fill(x0+bw-fold, y0, fold, fold, bgc)
		v.fill(x0+2, y0+2, bw-4, bh/4, lighten(paperCol, 0.5))
		v.fill(x0+bw/8, y0+bh-bh/5, bw-bw/4, maxInt(bh/11, 2), c)
	default:
		v.fill(x0, y0, bw, bh, paperCol)
		v.fill(x0+bw-fold, y0, fold, fold, bgc)
		v.fg(foldCol)
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x0+bw-fold), C.int(y0),
			C.int(x0+bw-fold), C.int(y0+fold))
		C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x0+bw-fold), C.int(y0+fold),
			C.int(x0+bw), C.int(y0+fold))
		v.fill(x0+bw/8, y0+bh-bh/5, bw-bw/4, maxInt(bh/11, 2), c)
	}
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

// wrapText breaks a text into the lines that fit into a width. A word that
// is longer than the width is cut in two.
func (v *view) wrapText(s string, maxPx int, maxLines int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	if maxLines < 1 {
		maxLines = 1
	}
	var lines []string
	cur := ""
	for _, w := range words {
		candidate := w
		if cur != "" {
			candidate = cur + " " + w
		}
		if v.textWidth(candidate) <= maxPx {
			cur = candidate
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
		}
		runes := []rune(w)
		// A word that is a little too long keeps its line and sticks out a
		// little; only a very long word is cut in two.
		if v.textWidth(w) <= maxPx*3/2 {
			cur = w
			continue
		}
		for v.textWidth(string(runes)) > maxPx && len(runes) > 1 {
			cut := len(runes)
			for cut > 1 && v.textWidth(string(runes[:cut])) > maxPx {
				cut--
			}
			lines = append(lines, string(runes[:cut]))
			runes = runes[cut:]
		}
		cur = string(runes)
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		last := []rune(lines[maxLines-1])
		for v.textWidth(string(last)+"…") > maxPx && len(last) > 1 {
			last = last[:len(last)-1]
		}
		lines[maxLines-1] = string(last) + "…"
	}
	return lines
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
	for {
		select {
		case <-v.dirEvent:
			// The desktop folder changed outside the board: re-scan it, so new
			// files and folders appear and removed ones disappear at once.
			v.reload(false)
			continue
		default:
		}
		break
	}
	for {
		select {
		case files := <-v.importDone:
			v.applyImported(files)
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
	gif   bool // the file is an animated gif
}

type thumbResult struct {
	path string
	key  string
	img  image.Image
	anim *gifAnim
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
		res := thumbResult{path: j.path, key: j.key}
		if j.gif {
			res.anim = v.gifAnimation(j)
			if res.anim != nil {
				res.img = res.anim.frames[0]
			}
		} else {
			res.img = thumbToImage(j)
		}
		v.thumbDone <- res
		wake()
	}
}

// gifAnimation returns the animation of a GIF, decoding it the first time and
// keeping it in memory afterwards.
func (v *view) gifAnimation(j thumbJob) *gifAnim {
	if v.gifCache == nil {
		v.gifCache = map[string]*gifAnim{}
	}
	if a, ok := v.gifCache[j.key]; ok {
		return a
	}
	a := decodeGIF(j.path)
	if a == nil {
		return nil
	}
	v.gifCache[j.key] = a
	return a
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
		// An animated gif is decoded in the shell itself: the frames have to
		// be played, not only shown.
		if strings.EqualFold(filepath.Ext(ic.path), ".gif") && v.cfg.Anim.GifEnabled {
			if a, cached := v.gifCache[key]; cached {
				ic.anim = a
				ic.thumb = a.frames[0]
				continue
			}
		}
		select {
		case v.thumbJobs <- thumbJob{ic.path, key, video, strings.EqualFold(filepath.Ext(ic.path), ".gif")}:
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
			if r.anim != nil {
				v.icons[i].anim = r.anim
				v.icons[i].thumb = r.img
				v.redraw = true
				break
			}
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
	return filepath.Join(base, "lythrum", "thumbs")
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
		C.lythrum_destroy_image(v.thumbImg)
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
		C.lythrum_destroy_image(v.wallImg)
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
		C.lythrum_destroy_image(v.wallImg)
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
		return filepath.Join(home, ".config", "lythrum", "wallpaper.json")
	}
	return filepath.Join(base, "lythrum", "wallpaper.json")
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

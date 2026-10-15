package x11

/*
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>

typedef struct {
	int x, y;
	unsigned int width, height, border_width;
	unsigned int depth;
	Window root;
	Visual *visual;
	int map_state;
} ly_geom;

static Window ly_create_window(Display *dpy, Window parent, int x, int y,
                               unsigned int w, unsigned int h, unsigned int border,
                               unsigned int depth, Visual *visual, unsigned long valuemask,
                               void *attrs) {
	return XCreateWindow(dpy, parent, x, y, w, h, border, depth,
		InputOutput, visual, valuemask, attrs);
}

static ly_geom *ly_get_geometry(Display *dpy, Window w) {
	ly_geom *g = (ly_geom *)calloc(1, sizeof(ly_geom));
	if (!g)
		return 0;
	if (!XGetGeometry(dpy, w, &g->root, &g->x, &g->y, &g->width, &g->height, &g->border_width, &g->depth)) {
		free(g);
		return 0;
	}
	XWindowAttributes a;
	if (XGetWindowAttributes(dpy, w, &a))
		g->visual = a.visual;
	g->map_state = 0;
	return g;
}

static int screen_of_display(Screen *s) { return XScreenNumberOfScreen(s); }

static Window ly_translate(Display *dpy, Window src, Window dst, int sx, int sy,
                           int *ox, int *oy, Window *child) {
	return XTranslateCoordinates(dpy, src, dst, sx, sy, ox, oy, child);
}

// ly_grab_input grabs the pointer inside a window and the keyboard outright,
// which is what a modal overlay needs. Xlib no longer offers XGrabInput.
static int ly_grab_input(Display *dpy, Window win, unsigned int event_mask) {
	int rc = XGrabPointer(dpy, win, True, event_mask, GrabModeAsync, GrabModeAsync,
	                      None, None, CurrentTime);
	if (rc != GrabSuccess)
		return rc;
	rc = XGrabKeyboard(dpy, win, True, GrabModeAsync, GrabModeAsync, CurrentTime);
	if (rc != GrabSuccess) {
		XUngrabPointer(dpy, CurrentTime);
		return rc;
	}
	return GrabSuccess;
}

static void ly_ungrab_input(Display *dpy) {
	XUngrabKeyboard(dpy, CurrentTime);
	XUngrabPointer(dpy, CurrentTime);
}
*/
import "C"

import (
	"unsafe"
)

// Event mask bits (X.h EventMask).
const (
	NoEventMask              uint32 = 0
	KeyPressMask             uint32 = 1 << 0
	KeyReleaseMask           uint32 = 1 << 1
	ButtonPressMask          uint32 = 1 << 2
	ButtonReleaseMask        uint32 = 1 << 3
	EnterWindowMask          uint32 = 1 << 4
	LeaveWindowMask          uint32 = 1 << 5
	PointerMotionMask        uint32 = 1 << 6
	Button1MotionMask        uint32 = 1 << 8
	Button2MotionMask        uint32 = 1 << 9
	ExposureMask             uint32 = 1 << 15
	VisibilityChangeMask     uint32 = 1 << 16
	StructureNotifyMask      uint32 = 1 << 17
	ResizeRedirectMask       uint32 = 1 << 18
	SubstructureNotifyMask   uint32 = 1 << 19
	SubstructureRedirectMask uint32 = 1 << 20
	FocusChangeMask          uint32 = 1 << 21
	PropertyChangeMask       uint32 = 1 << 22
	ColormapChangeMask       uint32 = 1 << 23
	OwnerGrabButtonMask      uint32 = 1 << 24
)

// Window classes.
const (
	ClassInputOutput = 1
	ClassInputOnly   = 2
)

// Map state values returned by XGetWindowAttributes.
const (
	StateUnmapped   = 0
	StateUnviewable = 1
	StateViewable   = 2
)

// Window configuration value mask bits.
const (
	CWBackPixmap       uint = 1 << 0
	CWBackPixel        uint = 1 << 1
	CWBorderPixmap     uint = 1 << 2
	CWBorderPixel      uint = 1 << 3
	CWBitGravity       uint = 1 << 4
	CWWinGravity       uint = 1 << 5
	CWBackingStore     uint = 1 << 6
	CWBackingPlanes    uint = 1 << 7
	CWBackingPixel     uint = 1 << 8
	CWOverrideRedirect uint = 1 << 9
	CWSaveUnder        uint = 1 << 10
	CWEventMask        uint = 1 << 11
	CWDontPropagate    uint = 1 << 12
	CWColormap         uint = 1 << 13
	CWCursor           uint = 1 << 14
)

// ConfigureWindow value mask bits.
const (
	ConfigX           uint = 1 << 0
	ConfigY           uint = 1 << 1
	ConfigWidth       uint = 1 << 2
	ConfigHeight      uint = 1 << 3
	ConfigBorderWidth uint = 1 << 4
	ConfigSibling     uint = 1 << 5
	ConfigStackMode   uint = 1 << 6
)

// Geometry describes the position and size of a window.
type Geometry struct {
	X, Y          int16
	Width, Height uint16
	BorderWidth   uint16
	Depth         uint8
	Root          Window
	MapState      int
}

// WindowAttributes is the subset of XWindowAttributes a WM cares about.
type WindowAttributes struct {
	MapState           int
	OverrideRedirect   bool
	YourEventMask      uint32
	AllEventMasks      uint32
	DoNotPropagateMask uint32
	Colormap           Colormap
	BitGravity         uint8
	WinGravity         uint8
	BackingStore       int
	SaveUnder          bool
	Subwindow          bool
	Screen             int
}

// Geometry queries the window's geometry.
func (d *Display) Geometry(w Window) (*Geometry, error) {
	if d.dpy == nil {
		return nil, errClosed
	}
	g := C.ly_get_geometry(d.dpy, C.Window(w))
	if g == nil {
		return nil, ErrBadWindow
	}
	defer C.free(unsafe.Pointer(g))
	return &Geometry{
		X: int16(g.x), Y: int16(g.y),
		Width: uint16(g.width), Height: uint16(g.height),
		BorderWidth: uint16(g.border_width), Depth: uint8(g.depth),
		Root: Window(g.root), MapState: int(g.map_state),
	}, nil
}

// Attributes reads the window attributes.
func (d *Display) Attributes(w Window) (*WindowAttributes, error) {
	if d.dpy == nil {
		return nil, errClosed
	}
	var a C.XWindowAttributes
	if C.XGetWindowAttributes(d.dpy, C.Window(w), &a) == 0 {
		return nil, ErrBadWindow
	}
	return &WindowAttributes{
		MapState:           int(a.map_state),
		OverrideRedirect:   a.override_redirect != 0,
		YourEventMask:      uint32(a.your_event_mask),
		AllEventMasks:      uint32(a.all_event_masks),
		DoNotPropagateMask: uint32(a.do_not_propagate_mask),
		Colormap:           Colormap(a.colormap),
		BitGravity:         uint8(a.bit_gravity),
		WinGravity:         uint8(a.win_gravity),
		BackingStore:       int(a.backing_store),
		SaveUnder:          a.save_under != 0,
		Screen:             int(C.screen_of_display(a.screen)),
	}, nil
}

// CreateWindowParams describes a window to create.
type CreateWindowParams struct {
	Parent           Window
	X, Y             int
	Width, Height    uint
	BorderWidth      uint
	Class            uint
	Depth            uint
	BackPixel        uint32
	BorderPixel      uint32
	BitGravity       uint
	WinGravity       uint
	BackingStore     uint
	BackingPlanes    uint
	BackingPixel     uint
	OverrideRedirect bool
	SaveUnder        bool
	EventMask        uint32
	DontPropagate    bool
	Colormap         Colormap
	Cursor           uint32
}

// CreateWindow creates an InputOutput window on the default visual.
func (d *Display) CreateWindow(p CreateWindowParams) (Window, error) {
	if d.dpy == nil {
		return 0, errClosed
	}
	if p.Class == 0 {
		p.Class = ClassInputOutput
	}
	if p.Depth == 0 {
		p.Depth = uint(d.Depth())
	}
	if p.BackingStore == 0 {
		p.BackingStore = 1 // NotUseful
	}

	mask := uint(C.CWBackPixel) | uint(C.CWBorderPixel) | uint(C.CWBitGravity) |
		uint(C.CWWinGravity) | uint(C.CWBackingStore) | uint(C.CWBackingPlanes) |
		uint(C.CWBackingPixel) | uint(C.CWOverrideRedirect) | uint(C.CWSaveUnder) |
		uint(C.CWEventMask) | uint(C.CWDontPropagate) | uint(C.CWColormap) |
		uint(C.CWCursor)

	var attrs [14]C.ulong
	idx := 0
	attrs[idx] = C.ulong(p.BackPixel)
	idx++
	attrs[idx] = C.ulong(p.BorderPixel)
	idx++
	attrs[idx] = C.ulong(p.BitGravity)
	idx++
	attrs[idx] = C.ulong(p.WinGravity)
	idx++
	attrs[idx] = C.ulong(p.BackingStore)
	idx++
	attrs[idx] = C.ulong(p.BackingPlanes)
	idx++
	attrs[idx] = C.ulong(p.BackingPixel)
	idx++
	attrs[idx] = C.ulong(toCBool(p.OverrideRedirect))
	idx++
	attrs[idx] = C.ulong(toCBool(p.SaveUnder))
	idx++
	attrs[idx] = C.ulong(p.EventMask)
	idx++
	attrs[idx] = C.ulong(toCBool(p.DontPropagate))
	idx++
	attrs[idx] = C.ulong(p.Colormap)
	idx++
	attrs[idx] = C.ulong(p.Cursor)
	idx++

	w := C.ly_create_window(d.dpy, C.Window(p.Parent),
		C.int(p.X), C.int(p.Y), C.uint(p.Width), C.uint(p.Height), C.uint(p.BorderWidth),
		C.uint(p.Depth), d.visual, C.ulong(mask), unsafe.Pointer(&attrs[0]))
	if w == 0 {
		return 0, ErrBadWindow
	}
	d.Flush()
	return Window(w), nil
}

// MapWindow maps a window.
func (d *Display) MapWindow(w Window) {
	if d.dpy != nil {
		C.XMapWindow(d.dpy, C.Window(w))
	}
}

// MapRaised maps a window and raises it to the top of the stack.
func (d *Display) MapRaised(w Window) {
	if d.dpy != nil {
		C.XMapRaised(d.dpy, C.Window(w))
	}
}

// UnmapWindow unmaps a window.
func (d *Display) UnmapWindow(w Window) {
	if d.dpy != nil {
		C.XUnmapWindow(d.dpy, C.Window(w))
	}
}

// DestroyWindow destroys a window.
func (d *Display) DestroyWindow(w Window) {
	if d.dpy != nil {
		C.XDestroyWindow(d.dpy, C.Window(w))
	}
}

// ConfigureWindow moves, resizes or restacks a window. Fields not present in
// mask are ignored.
func (d *Display) ConfigureWindow(w Window, mask uint, x, y int, width, height, border, sibling uint32, stackMode int) {
	if d.dpy == nil {
		return
	}
	var val C.XWindowChanges
	if mask != 0 {
		val.x = C.int(x)
		val.y = C.int(y)
		val.width = C.int(width)
		val.height = C.int(height)
		val.border_width = C.int(border)
		val.sibling = C.Window(sibling)
		val.stack_mode = C.int(stackMode)
		C.XConfigureWindow(d.dpy, C.Window(w), C.uint(mask), &val)
		return
	}
	C.XConfigureWindow(d.dpy, C.Window(w), C.uint(0), nil)
}

// MoveWindow moves a window.
func (d *Display) MoveWindow(w Window, x, y int) {
	d.ConfigureWindow(w, ConfigX|ConfigY, x, y, 0, 0, 0, 0, 0)
}

// ResizeWindow resizes a window without moving it.
func (d *Display) ResizeWindow(w Window, width, height uint32) {
	d.ConfigureWindow(w, ConfigWidth|ConfigHeight, 0, 0, width, height, 0, 0, 0)
}

// Restack raises or lowers a window in the stacking order.
func (d *Display) Restack(w Window, sibling Window, mode int) {
	d.ConfigureWindow(w, ConfigStackMode|ConfigSibling, 0, 0, 0, 0, 0, uint32(sibling), mode)
}

// Stacking mode values for Restack.
const (
	Above    = 0
	Below    = 1
	TopIf    = 2
	BottomIf = 3
	Opposite = 4
)

// ReparentWindow moves a window into a new parent.
func (d *Display) ReparentWindow(w, parent Window, x, y int) {
	if d.dpy != nil {
		C.XReparentWindow(d.dpy, C.Window(w), C.Window(parent), C.int(x), C.int(y))
	}
}

// SelectInput asks for events on a window.
func (d *Display) SelectInput(w Window, mask uint32) {
	if d.dpy != nil {
		C.XSelectInput(d.dpy, C.Window(w), C.long(mask))
	}
}

// ChangeSaveSet adds a window to the client's save-set, so it is reparented
// back to its original parent when the client dies.
func (d *Display) ChangeSaveSet(w Window) {
	if d.dpy != nil {
		C.XChangeSaveSet(d.dpy, C.Window(w), InsertFromSaveSet)
	}
}

// ChangeSaveSet modes.
const (
	InsertFromSaveSet = 0
	DeleteFromSaveSet = 1
)

// TranslateCoordinates maps coordinates between two windows.
func (d *Display) TranslateCoordinates(src, dst Window, sx, sy int) (dx, dy int, child Window, err error) {
	if d.dpy == nil {
		return 0, 0, 0, errClosed
	}
	var (
		ox, oy C.int
		ch     C.Window
	)
	C.ly_translate(d.dpy, C.Window(src), C.Window(dst), C.int(sx), C.int(sy), &ox, &oy, &ch)
	return int(ox), int(oy), Window(ch), nil
}

// FocusAction selects the focus transfer semantics.
type FocusAction int

// Focus transfer modes.
const (
	RevertToNone        FocusAction = 0
	RevertToPointerRoot FocusAction = 1
	RevertToParent      FocusAction = 2
)

// Focus sets the input focus to w.
func (d *Display) Focus(w Window, revert FocusAction, timestamp uint32) {
	if d.dpy != nil {
		C.XSetInputFocus(d.dpy, C.Window(w), C.int(revert), C.Time(timestamp))
	}
}

// FocusPointerRoot moves the focus back to the root window.
func (d *Display) FocusPointerRoot(revert FocusAction) {
	d.Focus(PointerRoot, revert, 0)
}

// PointerRoot is the root window constant used for focus fallback.
const PointerRoot = 1

// RaiseWindow raises a window to the top of the stacking order.
func (d *Display) RaiseWindow(w Window) {
	if d.dpy != nil {
		C.XRaiseWindow(d.dpy, C.Window(w))
	}
}

// GrabInput grabs the pointer inside a window and the keyboard, so that a
// modal overlay such as the start screen receives every event.
func (d *Display) GrabInput(win Window, pointerMask, keyboardMask uint32) error {
	if d.dpy == nil {
		return errClosed
	}
	// The mask lists the events the grab owner keeps; the keyboard grab makes
	// every key press arrive here as well.
	mask := pointerMask | keyboardMask
	rc := C.ly_grab_input(d.dpy, C.Window(win), C.uint(mask))
	if rc != C.GrabSuccess {
		return ErrBadAccess
	}
	return nil
}

// UngrabInput releases a grab taken with GrabInput.
func (d *Display) UngrabInput() {
	if d.dpy != nil {
		C.ly_ungrab_input(d.dpy)
		d.Flush()
	}
}

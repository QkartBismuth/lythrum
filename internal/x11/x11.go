// Package x11 is a thin, safe wrapper around libX11 (Xlib) built with cgo.
//
// The package deliberately keeps a single Display per process and is not
// safe for concurrent use: all calls must happen from the goroutine that owns
// the event loop. Values crossing the cgo boundary are only plain integers,
// so no C pointer is ever retained in Go-managed memory.
package x11

/*
#cgo pkg-config: x11 xrandr xinerama xext
#cgo LDFLAGS: -lXext
#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/Xatom.h>
#include <X11/extensions/Xrandr.h>
#include <X11/extensions/Xinerama.h>

typedef struct {
	unsigned long id;
	unsigned long root;
	Visual *visual;
	unsigned long cmap;
	int depth;
	int screen;
	int width;
	int height;
} ly_screen;

static ly_screen *ly_screen_info(Display *d, int scr) {
	XWindowAttributes a;
	Screen *s = ScreenOfDisplay(d, scr);
	if (XGetWindowAttributes(d, RootWindow(d, scr), &a) == 0)
		return 0;
	ly_screen *out = (ly_screen *)calloc(1, sizeof(ly_screen));
	if (!out)
		return 0;
	out->id = (unsigned long)a.root;
	out->root = (unsigned long)a.root;
	out->visual = a.visual;
	out->cmap = (unsigned long)a.colormap;
	out->depth = a.depth;
	out->screen = scr;
	out->width = (int)s->width;
	out->height = (int)s->height;
	return out;
}

static void ly_screen_free(ly_screen *s) { free(s); }

static unsigned long ly_max_request_bytes(Display *dpy) {
	long words = XExtendedMaxRequestSize(dpy);
	if (words <= 0)
		words = 65535;
	return (unsigned long)words * 4;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// Display is a connection to an X server.
type Display struct {
	dpy    *C.Display
	screen int
	root   Window
	visual *C.Visual
	depth  int
	cmap   Colormap
	w, h   int
	atoms  atomCache
}

// Core X11 identifiers.
type (
	Window   uint32
	Colormap uint32
	Atom     uint32
	KeySym   uint32
	KeyCode  uint8
)

// Open connects to the X server named by display ("" means $DISPLAY).
var threadOnce sync.Once

func Open(display string) (*Display, error) {
	// Several threads open and use connections, and the GTK side of the shell
	// has one of its own, so Xlib has to be told it is thread safe before the
	// first call.
	threadOnce.Do(func() { C.XInitThreads() })

	var name *C.char
	if display != "" {
		name = C.CString(display)
		defer C.free(unsafe.Pointer(name))
	}
	dpy := C.XOpenDisplay(name)
	if dpy == nil {
		if display == "" {
			return nil, errors.New("x11: cannot open display (is $DISPLAY set?)")
		}
		return nil, fmt.Errorf("x11: cannot open display %q", display)
	}

	screen := int(C.XDefaultScreen(dpy))
	si := C.ly_screen_info(dpy, C.int(screen))
	if si == nil {
		C.XCloseDisplay(dpy)
		return nil, errors.New("x11: cannot query screen info")
	}
	defer C.ly_screen_free(si)

	visual := si.visual
	if visual == nil {
		C.ly_screen_free(si)
		C.XCloseDisplay(dpy)
		return nil, errors.New("x11: cannot resolve default visual")
	}

	d := &Display{
		dpy:    dpy,
		screen: screen,
		root:   Window(si.root),
		visual: visual,
		depth:  int(si.depth),
		cmap:   Colormap(si.cmap),
		w:      int(si.width),
		h:      int(si.height),
	}
	d.atoms.init(d)
	loadStandardAtoms(d)
	activeDisplay = dpy
	return d, nil
}

// Close releases the connection.
func (d *Display) Close() {
	if d.dpy != nil {
		C.XCloseDisplay(d.dpy)
		d.dpy = nil
	}
}

// Flush sends buffered requests to the server.
func (d *Display) Flush() { C.XFlush(d.dpy) }

// Sync round-trips to the server, processing all events and errors first.
func (d *Display) Sync() { C.XSync(d.dpy, 0) }

// ConnFD returns the file descriptor of the X connection. Useful to wait on
// together with WaitEvent in a select loop.
func (d *Display) ConnFD() int { return int(C.XConnectionNumber(d.dpy)) }

// MaxRequestBytes is the largest single request the server accepts, which
// bounds how large one PutImage may be.
func (d *Display) MaxRequestBytes() int {
	if d.dpy == nil {
		return 0
	}
	return int(C.ly_max_request_bytes(d.dpy))
}

// MaxImageSide is the largest square image that still fits into one request.
func (d *Display) MaxImageSide() int {
	limit := d.MaxRequestBytes() - 4096
	side := 1
	for (side+1)*(side+1)*4 <= limit {
		side++
	}
	return side
}

// Screen reports the index of the default screen.
func (d *Display) Screen() int { return d.screen }

// Root is the root window of the default screen.
func (d *Display) Root() Window { return d.root }

// Depth is the colour depth of the default screen.
func (d *Display) Depth() int { return d.depth }

// Colormap is the default colormap of the default screen.
func (d *Display) Colormap() Colormap { return d.cmap }

// Size returns the pixel dimensions of the default screen.
func (d *Display) Size() (w, h int) { return d.w, d.h }

// pending reports whether the event queue is non-empty without blocking.
func (d *Display) pending() bool { return C.XPending(d.dpy) > 0 }

var errClosed = errors.New("x11: display connection closed")

package x11

/*
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/Xlib.h>

static void ly_build_key_event(XEvent *ev, Display *dpy, KeyCode code, unsigned int state,
                               Window target, Time t, int rx, int ry, int ex, int ey) {
	memset(ev, 0, sizeof(*ev));
	ev->type = KeyPress;
	ev->xkey.display = dpy;
	ev->xkey.window = target;
	ev->xkey.root = DefaultRootWindow(dpy);
	ev->xkey.subwindow = None;
	ev->xkey.time = t;
	ev->xkey.same_screen = True;
	ev->xkey.state = state;
	ev->xkey.keycode = code;
	ev->xkey.x_root = rx;
	ev->xkey.y_root = ry;
	ev->xkey.x = ex;
	ev->xkey.y = ey;
}

static void ly_query_tree(Display *dpy, Window root, Window *out, int max, int *n) {
	Window dummy_root, dummy_parent, *children = 0;
	unsigned int count = 0;
	int i;

	*n = 0;
	if (!XQueryTree(dpy, root, &dummy_root, &dummy_parent, &children, &count))
		return;
	for (i = 0; i < (int)count && i < max; i++)
		out[i] = children[i];
	*n = (int)count < max ? (int)count : max;
	if (children)
		XFree(children);
}
*/
import "C"

import (
	"strconv"
	"strings"
)

// QueryTree lists the children of a window, topmost first as the server
// reports them.
func (d *Display) QueryTree(w Window) ([]Window, error) {
	if d.dpy == nil {
		return nil, errClosed
	}
	buf := make([]C.Window, 256)
	var n C.int
	C.ly_query_tree(d.dpy, C.Window(w), &buf[0], C.int(len(buf)), &n)
	if n <= 0 {
		return nil, nil
	}
	out := make([]Window, 0, int(n))
	for _, c := range buf[:n] {
		out = append(out, Window(c))
	}
	return out, nil
}

// IconifyWindow unmaps a window and withdraws it to the icon.
func (d *Display) IconifyWindow(w Window) {
	if d.dpy != nil {
		C.XIconifyWindow(d.dpy, C.Window(w), 0)
		d.Flush()
	}
}

// SetWindowBackground paints a window in a solid colour.
func (d *Display) SetWindowBackground(w Window, pixel uint32) {
	if d.dpy != nil {
		C.XSetWindowBackground(d.dpy, C.Window(w), C.ulong(pixel))
		d.Flush()
	}
}

// WarpPointer moves the pointer to absolute root coordinates.
func (d *Display) WarpPointer(x, y int) {
	if d.dpy != nil {
		C.XWarpPointer(d.dpy, 0, C.Window(d.Root()), 0, 0, 0, 0, C.int(x), C.int(y))
		d.Flush()
	}
}

// QueryPointer returns the pointer position in root coordinates.
func (d *Display) QueryPointer() (x, y int, child Window, ok bool) {
	if d.dpy == nil {
		return 0, 0, 0, false
	}
	var (
		root, sub      C.Window
		rx, ry, wx, wy C.int
		mask           C.uint
	)
	ok = C.XQueryPointer(d.dpy, C.Window(d.Root()), &root, &sub, &rx, &ry, &wx, &wy, &mask) != 0
	return int(rx), int(ry), Window(sub), ok
}

// ParseColor resolves an "#rrggbb", "#rgb" or "rgb:rr/gg/bb" colour spec
// against the screen's default visual and colormap.
func (d *Display) ParseColor(spec string) (uint32, error) {
	if d.dpy == nil {
		return 0, errClosed
	}
	if strings.TrimSpace(spec) == "" {
		return 0, ErrBadValue
	}
	cs := C.CString(spec)
	defer freeCString(cs)
	var col C.XColor
	if C.XParseColor(d.dpy, C.Colormap(d.cmap), cs, &col) == 0 {
		return 0, ErrBadValue
	}
	var exact C.XColor
	if C.XAllocNamedColor(d.dpy, C.Colormap(d.cmap), cs, &exact, nil) == 0 {
		return 0, ErrBadValue
	}
	return uint32(exact.pixel), nil
}

// PixelFromRGB packs an RGB triple for a TrueColor visual. It is a fast path
// for the hex colours used in the configuration.
func (d *Display) PixelFromRGB(r, g, b uint32) uint32 {
	return (r&0xff)<<16 | (g&0xff)<<8 | (b & 0xff)
}

// HexColor parses "#rgb" or "#rrggbb" into RGB components.
func HexColor(spec string) (r, g, b uint32, err error) {
	s := strings.TrimPrefix(strings.TrimSpace(spec), "#")
	switch len(s) {
	case 3:
		v, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return 0, 0, 0, ErrBadValue
		}
		r, g, b = uint32(v>>8&0xf)<<4|uint32(v>>8&0xf), uint32(v>>4&0xf)<<4|uint32(v>>4&0xf), uint32(v&0xf)<<4|uint32(v&0xf)
		return r, g, b, nil
	case 6:
		v, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return 0, 0, 0, ErrBadValue
		}
		return uint32(v >> 16 & 0xff), uint32(v >> 8 & 0xff), uint32(v & 0xff), nil
	}
	return 0, 0, 0, ErrBadValue
}

// ClearArea exposes a region of a window so the server repaints it.
func (d *Display) ClearArea(w Window, x, y, width, height int) {
	if d.dpy != nil {
		C.XClearArea(d.dpy, C.Window(w), C.int(x), C.int(y), C.uint(width), C.uint(height), C.False)
	}
}

// ForwardKey re-delivers a key press to a client. A window manager that
// selects KeyPress on the root window sees every key and must pass on the ones
// it does not handle.
func (d *Display) ForwardKey(e KeyPress, target Window) {
	if d.dpy == nil {
		return
	}
	var ev C.XEvent
	C.ly_build_key_event(&ev, d.dpy, C.KeyCode(e.KeyCode), C.uint(e.State), C.Window(target),
		C.Time(e.Time()), C.int(e.RootX), C.int(e.RootY), C.int(e.EventX), C.int(e.EventY))
	C.XSendEvent(d.dpy, C.Window(target), C.True, C.long(KeyPressMask), &ev)
	d.Flush()
}

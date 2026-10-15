package x11

/*
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/extensions/Xrandr.h>
#include <X11/extensions/Xinerama.h>

typedef struct {
	int x, y, width, height;
	int primary;
} ly_monitor;

static int ly_query_monitors(Display *dpy, int screen, ly_monitor *out, int max) {
	int i, n = 0;
	int event_base = 0, error_base = 0;

	if (XRRQueryVersion(dpy, &event_base, &error_base) &&
	    XRRQueryExtension(dpy, &event_base, &error_base)) {
		int count = 0;
		XRRMonitorInfo *mons = XRRGetMonitors(dpy, (Drawable)RootWindow(dpy, screen), True, &count);
		if (mons) {
			for (i = 0; i < count && n < max; i++, n++) {
				out[n].x = mons[i].x;
				out[n].y = mons[i].y;
				out[n].width = mons[i].width;
				out[n].height = mons[i].height;
				out[n].primary = mons[i].primary ? 1 : 0;
			}
			XRRFreeMonitors(mons);
			if (n > 0)
				return n;
		}
	}

	if (XineramaQueryExtension(dpy, &event_base, &error_base) &&
	    XineramaIsActive(dpy)) {
		XineramaScreenInfo *screens = XineramaQueryScreens(dpy, &event_base);
		if (screens) {
			for (i = 0; i < event_base && n < max; i++, n++) {
				out[n].x = screens[i].x_org;
				out[n].y = screens[i].y_org;
				out[n].width = screens[i].width;
				out[n].height = screens[i].height;
				out[n].primary = (i == 0);
			}
			XFree(screens);
			if (n > 0)
				return n;
		}
	}

	// Last resort: the whole screen is one monitor.
	{
		Screen *s = ScreenOfDisplay(dpy, screen);
		out[0].x = 0;
		out[0].y = 0;
		out[0].width = s->width;
		out[0].height = s->height;
		out[0].primary = 1;
		return 1;
	}
}

static int ly_monitor_count(Display *dpy, int screen) {
	ly_monitor buf[64];
	return ly_query_monitors(dpy, screen, buf, 64);
}

static int ly_select_monitor_events(Display *dpy, int screen, Window root) {
	int event_base = 0, error_base = 0;

	if (XRRQueryExtension(dpy, &event_base, &error_base) &&
	    XRRQueryVersion(dpy, &event_base, &error_base)) {
		XRRSelectInput(dpy, root,
			RRScreenChangeNotifyMask | RRCrtcChangeNotifyMask | RROutputChangeNotifyMask);
		return event_base;
	}
	return 0;
}
*/
import "C"

import "fmt"

// Monitor is one physical output or logical screen area.
type Monitor struct {
	X, Y    int
	Width   int
	Height  int
	Primary bool
}

// Rect returns the monitor as a rectangle.
func (m Monitor) Rect() (x, y, w, h int) { return m.X, m.Y, m.Width, m.Height }

// Contains reports whether a point lies on the monitor.
func (m Monitor) Contains(x, y int) bool {
	return x >= m.X && x < m.X+m.Width && y >= m.Y && y < m.Y+m.Height
}

const maxMonitors = 64

// Monitors lists the monitors of the default screen, trying XRandR first and
// falling back to Xinerama and then to the whole screen.
func (d *Display) Monitors() ([]Monitor, error) {
	if d.dpy == nil {
		return nil, errClosed
	}
	buf := make([]C.ly_monitor, maxMonitors)
	n := C.ly_query_monitors(d.dpy, C.int(d.screen), &buf[0], C.int(len(buf)))
	if n <= 0 {
		return nil, fmt.Errorf("x11: cannot query monitors")
	}
	out := make([]Monitor, 0, int(n))
	for i := 0; i < int(n); i++ {
		out = append(out, Monitor{
			X:       int(buf[i].x),
			Y:       int(buf[i].y),
			Width:   int(buf[i].width),
			Height:  int(buf[i].height),
			Primary: buf[i].primary != 0,
		})
	}
	return out, nil
}

// MonitorCount returns how many monitors the screen has.
func (d *Display) MonitorCount() int {
	if d.dpy == nil {
		return 0
	}
	return int(C.ly_monitor_count(d.dpy, C.int(d.screen)))
}

// monitorEventMask is the bitmask of extension events that signal a monitor
// configuration change.
var monitorEventMask uint32

// SelectMonitorEvents subscribes to display-layout change notifications on the
// root window and returns the extension event bitmask to watch for.
func (d *Display) SelectMonitorEvents(root Window) uint32 {
	if d.dpy == nil {
		return 0
	}
	m := uint32(C.ly_select_monitor_events(d.dpy, C.int(d.screen), C.Window(root)))
	monitorEventMask = m
	return m
}

// MonitorChanged reports whether a generic event is a display-layout change
// from XRandR or Xinerama.
func MonitorChanged(e Generic) bool {
	return monitorEventMask != 0 && uint32(e.ExtensionOpcode)&monitorEventMask != 0
}

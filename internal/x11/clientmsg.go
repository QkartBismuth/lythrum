package x11

/*
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <stdlib.h>

static void ly_send_client_message(Display *dpy, Window dst, Atom type, int format,
                                   unsigned long d0, unsigned long d1, unsigned long d2,
                                   unsigned long d3, unsigned long d4) {
	XEvent ev;
	memset(&ev, 0, sizeof(ev));
	ev.xclient.type = ClientMessage;
	ev.xclient.display = dpy;
	ev.xclient.window = dst;
	ev.xclient.message_type = type;
	ev.xclient.format = format;
	ev.xclient.data.l[0] = (long)d0;
	ev.xclient.data.l[1] = (long)d1;
	ev.xclient.data.l[2] = (long)d2;
	ev.xclient.data.l[3] = (long)d3;
	ev.xclient.data.l[4] = (long)d4;
	XSendEvent(dpy, dst, False, 0L, &ev);
	XFlush(dpy);
}
*/
import "C"

// _NET_WM_STATE actions carried in the first data slot of the message.
const (
	stateAdd    uint32 = 0
	stateRemove uint32 = 1
	stateToggle uint32 = 2

	// sourceIndication tells the client the change came from the window
	// manager, as required by the EWMH.
	sourceIndication uint32 = 2
)

// Event masks used when sending synthetic events to a window.
const (
	NoEvent                 uint32 = 0
	KeyPressMaskSend        uint32 = 1 << 0
	ButtonPressMaskSend     uint32 = 1 << 2
	EnterWindowMaskSend     uint32 = 1 << 4
	FocusChangeMaskSend     uint32 = 1 << 21
	PropertyChangeMaskSend  uint32 = 1 << 22
	StructureNotifyMaskSend uint32 = 1 << 17
)

// SendClientMessage delivers a ClientMessage to a window. Both the EWMH
// requests and replies use it.
func (d *Display) SendClientMessage(dst Window, t Atom, format int, data [5]uint32) {
	if d.dpy == nil {
		return
	}
	C.ly_send_client_message(d.dpy, C.Window(dst), C.Atom(t), C.int(format),
		C.ulong(data[0]), C.ulong(data[1]), C.ulong(data[2]), C.ulong(data[3]), C.ulong(data[4]))
}

// GrabServer serialises the client against the rest of the session; it is
// required around _NET_MOVERESIZE_WINDOW and property changes that must not be
// seen half-applied.
func (d *Display) GrabServer() {
	if d.dpy != nil {
		C.XGrabServer(d.dpy)
	}
}

// UngrabServer releases a GrabServer.
func (d *Display) UngrabServer() {
	if d.dpy != nil {
		C.XUngrabServer(d.dpy)
		d.Flush()
	}
}

// ActiveWindow reports the value of _NET_ACTIVE_WINDOW on the root window.
func (d *Display) ActiveWindow() Window {
	w, ok := d.GetAtomValue(d.Root(), A.NETActiveWindow)
	if !ok {
		return 0
	}
	return Window(w)
}

// SetActiveWindow publishes the focused window to the rest of the session.
func (d *Display) SetActiveWindow(w Window) error {
	return d.SetCardinal(d.Root(), A.NETActiveWindow, A.Cardinal, uint32(w))
}

// SupportedAtoms returns the value of _NET_SUPPORTED on the root window.
func (d *Display) SupportedAtoms() []Atom {
	vals, err := d.GetCardinals(d.Root(), A.NETSupported)
	if err != nil {
		return nil
	}
	out := make([]Atom, 0, len(vals))
	for _, v := range vals {
		out = append(out, Atom(v))
	}
	return out
}

// SetSupportedAtoms publishes the atom list a window manager implements.
func (d *Display) SetSupportedAtoms(list []Atom) error {
	return d.SetCardinals(d.Root(), A.NETSupported, A.Atom, atomsFromUint32(list))
}

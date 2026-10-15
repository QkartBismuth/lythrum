package x11

/*
#include "xevent.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <unistd.h>
#include <poll.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>

// Xlib's error handler may not call back into Xlib, so it only copies the
// error into a slot armed by ly_collect_errors around a blocking call.
static ly_event *ly_error_slot = 0;
static int ly_error_armed = 0;

static int ly_error_handler(Display *dpy, XErrorEvent *err) {
	if (ly_error_armed && ly_error_slot) {
		memset(ly_error_slot, 0, sizeof(ly_event));
		ly_error_slot->type = -(int)err->error_code;
		ly_error_slot->serial = (int)err->serial;
		ly_error_slot->request = (unsigned long)err->request_code;
		ly_error_slot->resource = err->resourceid;
		ly_error_slot->minor = (unsigned long)err->minor_code;
	}
	return 0;
}

// The IO error handler must not return: Xlib terminates the process after it
// returns, so report the reason and leave on our own terms instead.
static int ly_io_error_handler(Display *dpy) {
	fprintf(stderr, "lythrum: lost connection to the X server\n");
	_exit(1);
	return 0;
}

static void ly_install_handlers(void) {
	XSetErrorHandler(ly_error_handler);
	XSetIOErrorHandler(ly_io_error_handler);
}

static void ly_collect_errors(ly_event *slot, int armed) {
	ly_error_slot = slot;
	ly_error_armed = armed;
}

static int ly_wait_for_event(Display *dpy, int timeout_ms) {
	struct pollfd p;
	p.fd = ConnectionNumber(dpy);
	p.events = POLLIN;
	if (XPending(dpy) > 0)
		return 1;
	if (timeout_ms < 0)
		return poll(&p, 1, -1) > 0 ? 1 : 0;
	return poll(&p, 1, timeout_ms) > 0 ? 1 : 0;
}

static void ly_decode_event(const XEvent *e, ly_event *o) {
	memset(o, 0, sizeof(ly_event));
	o->type = e->type;
	o->send_event = e->xany.send_event ? 1 : 0;
	switch (e->type) {
	case KeyPress:
	case KeyRelease: {
		const XKeyEvent *k = &e->xkey;
		o->serial = (int)k->serial;
		o->time = k->time;
		o->root = k->root;
		o->window = k->window;
		o->child = k->subwindow;
		o->x = k->x; o->y = k->y;
		o->root_x = k->x_root; o->root_y = k->y_root;
		o->state = k->state;
		o->same_screen = k->same_screen ? 1 : 0;
		o->detail = (int)k->keycode;
		break;
	}
	case ButtonPress:
	case ButtonRelease: {
		const XButtonEvent *b = &e->xbutton;
		o->serial = (int)b->serial;
		o->time = b->time;
		o->root = b->root;
		o->window = b->window;
		o->child = b->subwindow;
		o->x = b->x; o->y = b->y;
		o->root_x = b->x_root; o->root_y = b->y_root;
		o->state = b->state;
		o->same_screen = b->same_screen ? 1 : 0;
		o->detail = (int)b->button;
		break;
	}
	case MotionNotify: {
		const XMotionEvent *m = &e->xmotion;
		o->serial = (int)m->serial;
		o->time = m->time;
		o->root = m->root;
		o->window = m->window;
		o->child = m->subwindow;
		o->x = m->x; o->y = m->y;
		o->root_x = m->x_root; o->root_y = m->y_root;
		o->state = m->state;
		o->same_screen = m->same_screen ? 1 : 0;
		o->detail = m->is_hint ? 1 : 0;
		break;
	}
	case EnterNotify:
	case LeaveNotify: {
		const XCrossingEvent *c = &e->xcrossing;
		o->serial = (int)c->serial;
		o->time = c->time;
		o->root = c->root;
		o->window = c->window;
		o->child = c->subwindow;
		o->x = c->x; o->y = c->y;
		o->root_x = c->x_root; o->root_y = c->y_root;
		o->state = c->state;
		o->same_screen = c->same_screen ? 1 : 0;
		o->detail = c->detail;
		o->mode = c->mode;
		o->focus = c->focus;
		break;
	}
	case FocusIn:
	case FocusOut: {
		const XFocusChangeEvent *f = &e->xfocus;
		o->serial = (int)f->serial;
		o->mode = f->mode;
		o->window = f->window;
		break;
	}
	case Expose: {
		const XExposeEvent *x = &e->xexpose;
		o->serial = (int)x->serial;
		o->window = x->window;
		o->x = x->x; o->y = x->y;
		o->width = (int)x->width; o->height = (int)x->height;
		o->count = (unsigned long)x->count;
		break;
	}
	case VisibilityNotify: {
		const XVisibilityEvent *v = &e->xvisibility;
		o->serial = (int)v->serial;
		o->window = v->window;
		o->state = v->state;
		break;
	}
	case CreateNotify: {
		const XCreateWindowEvent *c = &e->xcreatewindow;
		o->serial = (int)c->serial;
		o->parent = c->parent;
		o->window = c->window;
		o->x = c->x; o->y = c->y;
		o->width = (int)c->width; o->height = (int)c->height;
		o->border_width = c->border_width;
		o->override_redirect = c->override_redirect ? 1 : 0;
		break;
	}
	case DestroyNotify: {
		const XDestroyWindowEvent *x = &e->xdestroywindow;
		o->serial = (int)x->serial;
		o->window = x->window;
		break;
	}
	case UnmapNotify: {
		const XUnmapEvent *u = &e->xunmap;
		o->serial = (int)u->serial;
		o->window = u->window;
		o->from_configure = u->from_configure ? 1 : 0;
		break;
	}
	case MapNotify: {
		const XMapEvent *m = &e->xmap;
		o->serial = (int)m->serial;
		o->window = m->window;
		o->override_redirect = m->override_redirect ? 1 : 0;
		break;
	}
	case MapRequest: {
		const XMapRequestEvent *m = &e->xmaprequest;
		o->serial = (int)m->serial;
		o->parent = m->parent;
		o->window = m->window;
		break;
	}
	case ReparentNotify: {
		const XReparentEvent *r = &e->xreparent;
		o->serial = (int)r->serial;
		o->window = r->window;
		o->parent = r->parent;
		o->x = r->x; o->y = r->y;
		o->override_redirect = r->override_redirect ? 1 : 0;
		break;
	}
	case ConfigureNotify: {
		const XConfigureEvent *c = &e->xconfigure;
		o->serial = (int)c->serial;
		o->window = c->event;
		o->x = c->x; o->y = c->y;
		o->width = c->width; o->height = c->height;
		o->border_width = c->border_width;
		o->above_sibling = c->above;
		o->override_redirect = c->override_redirect ? 1 : 0;
		break;
	}
	case ConfigureRequest: {
		const XConfigureRequestEvent *c = &e->xconfigurerequest;
		o->serial = (int)c->serial;
		o->parent = c->parent;
		o->window = c->window;
		o->value_mask = (unsigned long)c->value_mask;
		o->x = c->x; o->y = c->y;
		o->width = c->width; o->height = c->height;
		o->border_width = c->border_width;
		o->above_sibling = c->above;
		o->detail = c->detail;
		break;
	}
	case PropertyNotify: {
		const XPropertyEvent *p = &e->xproperty;
		o->serial = (int)p->serial;
		o->window = p->window;
		o->atom = p->atom;
		o->time = p->time;
		o->state = p->state; // 0 = NewValue, 1 = Delete
		break;
	}
	case SelectionClear: {
		const XSelectionClearEvent *s = &e->xselectionclear;
		o->serial = (int)s->serial;
		o->time = s->time;
		o->window = s->window;
		break;
	}
	case SelectionRequest: {
		const XSelectionRequestEvent *s = &e->xselectionrequest;
		o->serial = (int)s->serial;
		o->time = s->time;
		o->owner = s->owner;
		o->requestor = s->requestor;
		o->selection = s->selection;
		o->target = s->target;
		o->property = s->property;
		break;
	}
	case SelectionNotify: {
		const XSelectionEvent *s = &e->xselection;
		o->serial = (int)s->serial;
		o->time = s->time;
		o->requestor = s->requestor;
		o->selection = s->selection;
		o->target = s->target;
		o->property = s->property;
		break;
	}
	case ClientMessage: {
		int i;
		const XClientMessageEvent *c = &e->xclient;
		o->serial = (int)c->serial;
		o->window = c->window;
		o->msg_type = c->message_type;
		o->format = c->format;
		for (i = 0; i < 5; i++)
			o->data[i] = (unsigned long)c->data.l[i];
		break;
	}
	case MappingNotify: {
		const XMappingEvent *m = &e->xmapping;
		o->serial = (int)m->serial;
		o->window = m->window;
		o->request = (unsigned long)m->request;
		o->detail = m->first_keycode;
		o->count = (unsigned long)m->count;
		break;
	}
	case GenericEvent: {
		o->serial = (int)e->xgeneric.serial;
		o->extension = (unsigned long)e->xgeneric.extension;
		o->detail = e->xgeneric.evtype;
		break;
	}
	default:
		break;
	}
}
*/
import "C"

import (
	"errors"
	"sync"
	"time"
)

// Event type codes from the core X11 protocol.
const (
	KeyPressEvent         uint8 = 2
	KeyReleaseEvent       uint8 = 3
	ButtonPressEvent      uint8 = 4
	ButtonReleaseEvent    uint8 = 5
	MotionNotifyEvent     uint8 = 6
	EnterNotifyEvent      uint8 = 7
	LeaveNotifyEvent      uint8 = 8
	FocusInEvent          uint8 = 9
	FocusOutEvent         uint8 = 10
	ExposeEvent           uint8 = 12
	VisibilityEvent       uint8 = 15
	CreateNotifyEvent     uint8 = 16
	DestroyNotifyEvent    uint8 = 17
	UnmapNotifyEvent      uint8 = 18
	MapNotifyEvent        uint8 = 19
	MapRequestEvent       uint8 = 20
	ReparentNotifyEvent   uint8 = 21
	ConfigureNotifyEvent  uint8 = 22
	ConfigureRequestEvent uint8 = 23
	PropertyNotifyEvent   uint8 = 28
	SelectionClearEvent   uint8 = 29
	SelectionRequestEvent uint8 = 30
	SelectionNotifyEvent  uint8 = 31
	ClientMessageEvent    uint8 = 33
	MappingNotifyEvent    uint8 = 34

	// GenericEvent is delivered for extension events (XRandR, XInput...).
	GenericEvent uint8 = 35
)

// Event is a decoded X protocol event.
type Event interface {
	// TypeCode returns the protocol event code.
	TypeCode() uint8
	// SentByClient reports whether the event was produced by XSendEvent.
	SentByClient() bool
	// Sequence returns the sequence number of the originating request.
	Sequence() uint16
}

// header carries the fields shared by every core X event.
type header struct {
	Type       uint8
	Serial     uint16
	SendEvent  bool
	Timestamp  uint32
	Root       Window
	Window     Window
	Child      Window
	RootX      int16
	RootY      int16
	EventX     int16
	EventY     int16
	State      uint16
	SameScreen bool
}

func (h header) TypeCode() uint8    { return h.Type }
func (h header) SentByClient() bool { return h.SendEvent }
func (h header) Sequence() uint16   { return h.Serial }
func (h header) Time() uint32       { return h.Timestamp }

func newHeader(e *C.ly_event) header {
	return header{
		Type:       uint8(e._type),
		Serial:     uint16(e.serial),
		SendEvent:  e.send_event != 0,
		Timestamp:  uint32(e.time),
		Root:       Window(e.root),
		Window:     Window(e.window),
		Child:      Window(e.child),
		RootX:      int16(e.root_x),
		RootY:      int16(e.root_y),
		EventX:     int16(e.x),
		EventY:     int16(e.y),
		State:      uint16(e.state),
		SameScreen: e.same_screen != 0,
	}
}

// KeyEvent is a KeyPress or KeyRelease.
type KeyEvent struct {
	header
	KeyCode KeyCode
	State16 uint16
}

// KeyPress reports a key press.
type KeyPress struct{ KeyEvent }

// KeyRelease reports a key release.
type KeyRelease struct{ KeyEvent }

// ButtonEvent is a ButtonPress or ButtonRelease.
type ButtonEvent struct {
	header
	Button uint8
}

// ButtonPress reports a button press.
type ButtonPress struct{ ButtonEvent }

// ButtonRelease reports a button release.
type ButtonRelease struct{ ButtonEvent }

// MotionNotify reports pointer motion.
type MotionNotify struct{ header }

// CrossEvent is EnterNotify or LeaveNotify.
type CrossEvent struct {
	header
	Mode   uint8
	Focus  Window
	Detail uint8
}

// EnterNotify reports the pointer entering a window.
type EnterNotify struct{ CrossEvent }

// LeaveNotify reports the pointer leaving a window.
type LeaveNotify struct{ CrossEvent }

// FocusEvent is FocusIn or FocusOut.
type FocusEvent struct {
	header
	Mode uint8
}

// FocusIn reports focus arriving at a window.
type FocusIn struct{ FocusEvent }

// FocusOut reports focus leaving a window.
type FocusOut struct{ FocusEvent }

// Expose reports a region of a window that needs repainting.
type Expose struct {
	header
	X, Y, Width, Height int
	Count               int
}

// VisibilityNotify reports a change in a window's visibility.
type VisibilityNotify struct{ header }

// CreateNotify reports a child being created.
type CreateNotify struct{ header }

// DestroyNotify reports a window being destroyed.
type DestroyNotify struct{ header }

// UnmapNotify reports a window being unmapped.
type UnmapNotify struct {
	header
	FromConfigure bool
}

// MapNotify reports a window being mapped.
type MapNotify struct {
	header
	OverrideRedirect bool
}

// MapRequest is a client asking to be mapped.
type MapRequest struct{ header }

// ReparentNotify reports a window gaining a new parent.
type ReparentNotify struct {
	header
	Parent           Window
	OverrideRedirect bool
}

// ConfigureNotify reports a change of a window's geometry.
type ConfigureNotify struct {
	header
	X, Y, Width, Height int
	BorderWidth         int
	AboveSibling        Window
	OverrideRedirect    bool
}

// ConfigureRequest is a client asking for a geometry or stacking change.
type ConfigureRequest struct {
	header
	ValueMask    uint16
	X, Y         int
	Width        int
	Height       int
	BorderWidth  int
	AboveSibling Window
	Detail       int
}

// PropertyNotify reports a property being changed or deleted.
type PropertyNotify struct {
	header
	Atom     Atom
	NewValue bool
}

// SelectionClear reports another client claiming a selection.
type SelectionClear struct{ header }

// SelectionRequest is a client asking to receive a selection.
type SelectionRequest struct {
	header
	Owner     Window
	Requestor Window
	Selection Atom
	Target    Atom
	Property  Atom
}

// SelectionNotify is the reply to a SelectionRequest.
type SelectionNotify struct {
	header
	Selection Atom
	Target    Atom
	Property  Atom
}

// ClientMessage is a synthetic event exchanged between clients.
type ClientMessage struct {
	header
	Format int
	Type   Atom
	Data   [5]uint32
}

// MappingNotify reports a change in the keyboard mapping.
type MappingNotify struct {
	header
	FirstKeyCode KeyCode
	Count        int
}

// Generic is an extension event; ExtensionOpcode identifies the extension.
type Generic struct {
	header
	ExtensionOpcode uint8
	Data            [5]uint32
}

// decodeEvent converts the flat C struct into a typed Go event.
func decodeEvent(e *C.ly_event) Event {
	h := newHeader(e)
	switch h.Type {
	case KeyPressEvent:
		return KeyPress{KeyEvent{header: h, KeyCode: KeyCode(e.detail), State16: uint16(e.state)}}
	case KeyReleaseEvent:
		return KeyRelease{KeyEvent{header: h, KeyCode: KeyCode(e.detail), State16: uint16(e.state)}}
	case ButtonPressEvent:
		return ButtonPress{ButtonEvent{header: h, Button: uint8(e.detail)}}
	case ButtonReleaseEvent:
		return ButtonRelease{ButtonEvent{header: h, Button: uint8(e.detail)}}
	case MotionNotifyEvent:
		return MotionNotify{h}
	case EnterNotifyEvent:
		return EnterNotify{CrossEvent{header: h, Mode: uint8(e.mode), Focus: Window(e.focus), Detail: uint8(e.detail)}}
	case LeaveNotifyEvent:
		return LeaveNotify{CrossEvent{header: h, Mode: uint8(e.mode), Focus: Window(e.focus), Detail: uint8(e.detail)}}

	case FocusInEvent:
		return FocusIn{FocusEvent{header: h, Mode: uint8(e.mode)}}
	case FocusOutEvent:
		return FocusOut{FocusEvent{header: h, Mode: uint8(e.mode)}}
	case ExposeEvent:
		return Expose{header: h, X: int(e.x), Y: int(e.y), Width: int(e.width), Height: int(e.height), Count: int(e.count)}
	case VisibilityEvent:
		return VisibilityNotify{h}
	case CreateNotifyEvent:
		return CreateNotify{h}
	case DestroyNotifyEvent:
		return DestroyNotify{h}
	case UnmapNotifyEvent:
		return UnmapNotify{header: h, FromConfigure: e.from_configure != 0}
	case MapNotifyEvent:
		return MapNotify{header: h, OverrideRedirect: e.override_redirect != 0}
	case MapRequestEvent:
		return MapRequest{h}
	case ReparentNotifyEvent:
		return ReparentNotify{header: h, Parent: Window(e.parent), OverrideRedirect: e.override_redirect != 0}
	case ConfigureNotifyEvent:
		return ConfigureNotify{
			header: h, X: int(e.x), Y: int(e.y),
			Width: int(e.width), Height: int(e.height), BorderWidth: int(e.border_width),
			AboveSibling: Window(e.above_sibling), OverrideRedirect: e.override_redirect != 0,
		}
	case ConfigureRequestEvent:
		return ConfigureRequest{
			header: h, ValueMask: uint16(e.value_mask), X: int(e.x), Y: int(e.y),
			Width: int(e.width), Height: int(e.height), BorderWidth: int(e.border_width),
			AboveSibling: Window(e.above_sibling), Detail: int(e.detail),
		}
	case PropertyNotifyEvent:
		return PropertyNotify{header: h, Atom: Atom(e.atom), NewValue: e.state == 0}
	case SelectionClearEvent:
		return SelectionClear{h}
	case SelectionRequestEvent:
		return SelectionRequest{
			header: h, Owner: Window(e.owner), Requestor: Window(e.requestor),
			Selection: Atom(e.selection), Target: Atom(e.target), Property: Atom(e.property),
		}
	case SelectionNotifyEvent:
		return SelectionNotify{header: h, Selection: Atom(e.selection), Target: Atom(e.target), Property: Atom(e.property)}
	case ClientMessageEvent:
		return ClientMessage{
			header: h, Format: int(e.format), Type: Atom(e.msg_type),
			Data: [5]uint32{uint32(e.data[0]), uint32(e.data[1]), uint32(e.data[2]), uint32(e.data[3]), uint32(e.data[4])},
		}
	case MappingNotifyEvent:
		return MappingNotify{header: h, FirstKeyCode: KeyCode(e.detail), Count: int(e.count)}
	case GenericEvent:
		return Generic{header: h, ExtensionOpcode: uint8(e.extension)}
	}
	return nil
}

var (
	errorMu     sync.Mutex
	errorSlot   C.ly_event
	errorEvents []ServerError
	errorHook   func(ServerError)
)

// ErrTimeout is returned when an event wait expires without an event.
var ErrTimeout = errors.New("x11: timeout waiting for event")

// ErrConnectionLost is returned once the X connection has been torn down.
var ErrConnectionLost = errors.New("x11: connection lost")

// SetErrorHandler installs a callback invoked for every X protocol error.
// Protocol errors are asynchronous and usually harmless (a window vanished, a
// misbehaving client referenced a bad id), so they are reported rather than
// treated as fatal.
func SetErrorHandler(fn func(ServerError)) {
	errorMu.Lock()
	errorHook = fn
	errorMu.Unlock()
}

// drainErrors performs a round-trip so pending errors land in errorSlot and
// hands them to the registered hook.
func drainErrors() {
	errorMu.Lock()
	armed := errorSlot
	errorMu.Unlock()

	C.ly_collect_errors(&armed, C.int(1))
	C.XSync(activeDisplay, 0)
	C.ly_collect_errors(&armed, C.int(0))

	errorMu.Lock()
	if armed._type != 0 {
		code := byte(-armed._type)
		name := errorNames[code]
		if name == "" {
			name = "UnknownError"
		}
		e := ServerError{
			Code:        code,
			CodeName:    name,
			Sequence:    uint16(armed.serial),
			ResourceID:  uint32(armed.resource),
			MinorOpcode: uint16(armed.minor),
			MajorOpcode: uint8(armed.request),
			Message:     name,
		}
		errorEvents = append(errorEvents, e)
		hook := errorHook
		errorMu.Unlock()
		if hook != nil {
			hook(e)
		}
		return
	}
	errorMu.Unlock()
}

// activeDisplay is the connection used for error round-trips.
var activeDisplay *C.Display

// NextEvent blocks until the next event is available and decodes it.
func (d *Display) NextEvent() (Event, error) {
	if d.dpy == nil {
		return nil, ErrConnectionLost
	}
	var (
		raw C.XEvent
		ev  C.ly_event
	)
	C.ly_collect_errors(&errorSlot, C.int(1))
	C.XNextEvent(d.dpy, &raw)
	C.ly_collect_errors(&errorSlot, C.int(0))
	C.ly_decode_event(&raw, &ev)
	drainErrors()
	return decodeEvent(&ev), nil
}

// PollEvent waits up to timeout for an event and returns ErrTimeout when
// nothing arrives.
func (d *Display) PollEvent(timeout time.Duration) (Event, error) {
	if d.dpy == nil {
		return nil, ErrConnectionLost
	}
	ms := -1
	if timeout >= 0 {
		ms = int(timeout.Milliseconds())
	}
	if C.ly_wait_for_event(d.dpy, C.int(ms)) == 0 {
		drainErrors()
		return nil, ErrTimeout
	}
	var (
		raw C.XEvent
		ev  C.ly_event
	)
	C.ly_collect_errors(&errorSlot, C.int(1))
	C.XNextEvent(d.dpy, &raw)
	C.ly_collect_errors(&errorSlot, C.int(0))
	C.ly_decode_event(&raw, &ev)
	drainErrors()
	return decodeEvent(&ev), nil
}

// Errors returns and clears the protocol errors collected so far.
func (d *Display) Errors() []ServerError {
	errorMu.Lock()
	defer errorMu.Unlock()
	out := errorEvents
	errorEvents = nil
	return out
}

func init() { C.ly_install_handlers() }

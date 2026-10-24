package main

/*
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <stdlib.h>

// The data of a client message is a union of five longs. cgo does not expose
// the members of the union, so the shell reads and writes them through these
// helpers.
static long lythrum_cmsg_get(XClientMessageEvent *e, int i) {
	return e->data.l[i];
}

static void lythrum_cmsg_send(Display *dpy, Window w, Atom type,
                             long l0, long l1, long l2, long l3, long l4) {
	XEvent ev;
	XClientMessageEvent *ce = (XClientMessageEvent *)&ev;
	ce->type = ClientMessage;
	ce->display = dpy;
	ce->window = w;
	ce->message_type = type;
	ce->format = 32;
	ce->data.l[0] = l0;
	ce->data.l[1] = l1;
	ce->data.l[2] = l2;
	ce->data.l[3] = l3;
	ce->data.l[4] = l4;
	XSendEvent(dpy, w, False, 0, &ev);
}

// lythrum_selection_notify tells a client that its request for the selection
// is answered.
static void lythrum_selection_notify(Display *dpy, Window requestor,
                                    Atom selection, Atom target,
                                    Atom property, Time time) {
	XEvent ev;
	XSelectionEvent *se = (XSelectionEvent *)&ev;
	se->type = SelectionNotify;
	se->display = dpy;
	se->requestor = requestor;
	se->selection = selection;
	se->target = target;
	se->property = property;
	se->time = time;
	XSendEvent(dpy, requestor, False, 0, &ev);
}
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

// The atoms of the XDND protocol: the board is a drop target for the files
// the user drags out of a file manager.
var (
	atomXdndAware      C.Atom
	atomXdndEnter      C.Atom
	atomXdndPosition   C.Atom
	atomXdndStatus     C.Atom
	atomXdndLeave      C.Atom
	atomXdndDrop       C.Atom
	atomXdndFinished   C.Atom
	atomXdndSelection  C.Atom
	atomXdndTypeList   C.Atom
	atomXdndActionCopy C.Atom
	atomUriList        C.Atom
	atomTextPlain      C.Atom
)

// xaAtom is the X11 ATOM type, used for the XdndAware property.
const xaAtom = 4

// xdndState follows the drag of a file over the board.
type xdndState struct {
	active   bool
	source   C.Window
	version  int
	typ      C.Atom
	accepted bool
	rootX    int
	rootY    int
	waiting  bool
	dropTime C.Time
	prop     C.Atom
}

// xdndSource follows the drag of the icons of the board over another
// application: the board offers the files it drags to it.
type xdndSource struct {
	active   bool
	target   C.Window
	accepted bool
	paths    []string
	selTime  C.Time
}

// xdndInit makes the board a drop target and interns the atoms of the
// protocol.
func (v *view) xdndInit() {
	dpy := v.dpy
	atomXdndAware = C.XInternAtom(dpy, cstr("XdndAware"), C.False)
	atomXdndEnter = C.XInternAtom(dpy, cstr("XdndEnter"), C.False)
	atomXdndPosition = C.XInternAtom(dpy, cstr("XdndPosition"), C.False)
	atomXdndStatus = C.XInternAtom(dpy, cstr("XdndStatus"), C.False)
	atomXdndLeave = C.XInternAtom(dpy, cstr("XdndLeave"), C.False)
	atomXdndDrop = C.XInternAtom(dpy, cstr("XdndDrop"), C.False)
	atomXdndFinished = C.XInternAtom(dpy, cstr("XdndFinished"), C.False)
	atomXdndSelection = C.XInternAtom(dpy, cstr("XdndSelection"), C.False)
	atomXdndTypeList = C.XInternAtom(dpy, cstr("XdndTypeList"), C.False)
	atomXdndActionCopy = C.XInternAtom(dpy, cstr("XdndActionCopy"), C.False)
	atomUriList = C.XInternAtom(dpy, cstr("text/uri-list"), C.False)
	atomTextPlain = C.XInternAtom(dpy, cstr("text/plain;charset=utf-8"), C.False)

	// The XdndAware property tells the world that the board accepts drops.
	val := C.long(5)
	C.XChangeProperty(dpy, v.win, atomXdndAware, C.Atom(xaAtom), 32,
		C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&val)), 1)
	C.XFlush(dpy)
}

// cstr makes a C string out of a Go one.
func cstr(s string) *C.char {
	return C.CString(s)
}

// xdndTypeList reads the list of formats a source offers.
func (v *view) xdndTypeList(source C.Window) C.Atom {
	var actualType C.Atom
	var actualFormat C.int
	var nitems, bytesAfter C.ulong
	var prop *C.uchar
	rc := C.XGetWindowProperty(v.dpy, source, atomXdndTypeList, 0, 64, C.False,
		C.AnyPropertyType, &actualType, &actualFormat, &nitems, &bytesAfter, &prop)
	if rc != C.Success || prop == nil {
		return 0
	}
	defer C.XFree(unsafe.Pointer(prop))
	list := C.GoBytes(unsafe.Pointer(prop), C.int(nitems*4))
	var found C.Atom
	for i := 0; i+4 <= len(list); i += 4 {
		a := C.Atom(uint32(list[i]) | uint32(list[i+1])<<8 |
			uint32(list[i+2])<<16 | uint32(list[i+3])<<24)
		if a == atomUriList {
			return a
		}
		if a == atomTextPlain {
			found = a
		}
	}
	return found
}

// onXDND handles the messages of a drag that comes from another application.
func (v *view) onXDND(ce *C.XClientMessageEvent) bool {
	switch ce.message_type {
	case atomXdndEnter:
		source := C.Window(C.lythrum_cmsg_get(ce, 0))
		version := int(C.lythrum_cmsg_get(ce, 1))
		if version > 5 {
			version = 5
		}
		typ := C.Atom(C.lythrum_cmsg_get(ce, 3))
		if typ == 0 {
			typ = v.xdndTypeList(source)
		}
		if typ != atomUriList && typ != atomTextPlain {
			return true // not a file drag
		}
		v.xdn = xdndState{active: true, source: source, version: version, typ: typ}
		if debugDND {
			fmt.Printf("dnd: enter from 0x%x, version %d\n", uint64(source), version)
		}
	case atomXdndPosition:
		if !v.xdn.active {
			return true
		}
		pos := uint32(C.lythrum_cmsg_get(ce, 2))
		v.xdn.rootX = int((pos >> 16) & 0xffff)
		v.xdn.rootY = int(pos & 0xffff)
		v.xdn.accepted = true
		v.xdndStatus()
		if debugDND {
			fmt.Printf("dnd: position %d,%d\n", v.xdn.rootX, v.xdn.rootY)
		}
	case atomXdndLeave:
		v.xdn = xdndState{}
	case atomXdndDrop:
		if !v.xdn.active {
			return true
		}
		// Ask the source for the list of the files it drags.
		prop := C.XInternAtom(v.dpy, cstr("LYTHRUM_XDND_DROP"), C.False)
		v.xdn.prop = prop
		v.xdn.waiting = true
		v.xdn.dropTime = C.Time(C.lythrum_cmsg_get(ce, 2))
		C.XConvertSelection(v.dpy, atomXdndSelection, v.xdn.typ, prop, v.win,
			C.Time(C.lythrum_cmsg_get(ce, 2)))
		C.XFlush(v.dpy)
		if debugDND {
			fmt.Println("dnd: drop, waiting for the selection")
		}
	case atomXdndStatus:
		// The answer of the application that takes the files of the board.
		v.xdndStatusIn(ce)
	case atomXdndFinished:
		v.xdndFinishedIn(ce)
	}
	return true
}

// xdndStatus tells the source that the board accepts the drop.
func (v *view) xdndStatus() {
	if !v.xdn.active {
		return
	}
	var x, y C.int
	var child C.Window
	C.XTranslateCoordinates(v.dpy, v.win, C.XDefaultRootWindow(v.dpy), 0, 0, &x, &y, &child)
	action := C.long(0)
	if v.xdn.accepted {
		action = 1 // XdndActionCopy
	}
	C.lythrum_cmsg_send(v.dpy, v.xdn.source, atomXdndStatus,
		C.long(v.xdn.source), C.long(v.win), action,
		C.long((x<<16)|(y&0xffff)), C.long(((x+40)<<16)|((y+40)&0xffff)))
	C.XFlush(v.dpy)
}

// xdndFinish tells the source that the drop is over.
func (v *view) xdndFinish(ok bool) {
	if !v.xdn.active {
		return
	}
	result := C.long(1)
	if ok {
		result = 0
	}
	C.lythrum_cmsg_send(v.dpy, v.xdn.source, atomXdndFinished,
		C.long(v.xdn.source), result, C.long(v.xdn.dropTime), 0, 0)
	C.XFlush(v.dpy)
	v.xdn = xdndState{}
}

// ---------------------------------------------------------- the other way -- //

// xdndStart makes the board the source of a drag: it offers the files it
// drags to the application under the pointer.
func (v *view) xdndStart(paths []string) {
	if len(paths) == 0 {
		return
	}
	v.xds = xdndSource{active: true, paths: paths, selTime: C.CurrentTime}
	// The type list tells the target what the board can offer.
	list := []C.Atom{atomUriList, atomTextPlain}
	buf := make([]byte, 4*len(list))
	for i, a := range list {
		binary.LittleEndian.PutUint32(buf[i*4:], uint32(a))
	}
	C.XChangeProperty(v.dpy, v.win, atomXdndTypeList, C.XA_ATOM, 32,
		C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&buf[0])), C.int(len(list)))
	// The board owns the selection with the dragged files.
	C.XSetSelectionOwner(v.dpy, atomXdndSelection, v.win, C.CurrentTime)
	C.XFlush(v.dpy)
}

// xdndStop gives the selection back and forgets the drag.
func (v *view) xdndStop() {
	if !v.xds.active {
		return
	}
	C.XSetSelectionOwner(v.dpy, atomXdndSelection, 0, C.CurrentTime)
	v.xds = xdndSource{}
	C.XFlush(v.dpy)
}

// xdndTarget returns the window under the pointer that accepts drops.
func (v *view) xdndTarget(rx, ry int) C.Window {
	var root, child C.Window
	var rootX, rootY, winX, winY C.int
	var mask C.uint
	C.XQueryPointer(v.dpy, C.XDefaultRootWindow(v.dpy), &root, &child,
		&rootX, &rootY, &winX, &winY, &mask)
	if child == 0 {
		return 0
	}
	w := child
	for i := 0; i < 12 && w != 0; i++ {
		if v.hasXdndAware(w) {
			return w
		}
		var r, parent C.Window
		var children *C.Window
		var n C.uint
		if C.XQueryTree(v.dpy, w, &r, &parent, &children, &n) != C.True {
			return 0
		}
		if children != nil {
			C.XFree(unsafe.Pointer(children))
		}
		if parent == r {
			return 0
		}
		w = parent
	}
	return 0
}

// hasXdndAware tells whether a window accepts drops.
func (v *view) hasXdndAware(w C.Window) bool {
	var actualType C.Atom
	var actualFormat C.int
	var nitems, bytesAfter C.ulong
	var prop *C.uchar
	rc := C.XGetWindowProperty(v.dpy, w, atomXdndAware, 0, 1, C.False,
		xaAtom, &actualType, &actualFormat, &nitems, &bytesAfter, &prop)
	if rc != C.Success || prop == nil {
		return false
	}
	C.XFree(unsafe.Pointer(prop))
	return nitems > 0
}

// xdndMove follows the pointer over the target of the drag.
func (v *view) xdndMove(rx, ry int) {
	if !v.xds.active {
		return
	}
	target := v.xdndTarget(rx, ry)
	if target != v.xds.target {
		if v.xds.target != 0 {
			C.lythrum_cmsg_send(v.dpy, v.xds.target, atomXdndLeave,
				C.long(v.xds.target), 0, 0, 0, 0)
		}
		v.xds.target = target
		v.xds.accepted = false
		if target != 0 {
			C.lythrum_cmsg_send(v.dpy, target, atomXdndEnter,
				C.long(target), 0, 0, C.long(atomUriList), C.CurrentTime)
		}
		C.XFlush(v.dpy)
	}
	if v.xds.target == 0 {
		return
	}
	action := C.long(0)
	if v.xds.accepted {
		action = 1 // XdndActionCopy
	}
	C.lythrum_cmsg_send(v.dpy, v.xds.target, atomXdndPosition,
		C.long(v.xds.target), 0, C.long((rx<<16)|(ry&0xffff)), action,
		C.CurrentTime)
	C.XFlush(v.dpy)
}

// xdndRelease drops the files on the target of the drag.
func (v *view) xdndRelease(rx, ry int) {
	if !v.xds.active {
		return
	}
	if v.xds.target != 0 {
		C.lythrum_cmsg_send(v.dpy, v.xds.target, atomXdndDrop,
			C.long(v.xds.target), 0, C.long((rx<<16)|(ry&0xffff)),
			C.CurrentTime, 0)
		C.XFlush(v.dpy)
		return
	}
	v.xdndStop()
}

// xdndStatusIn is the answer of the target: it tells the board whether it
// accepts the drop.
func (v *view) xdndStatusIn(ce *C.XClientMessageEvent) {
	if !v.xds.active {
		return
	}
	v.xds.accepted = C.lythrum_cmsg_get(ce, 2) != 0
}

// xdndFinishedIn is the end of the drop: the target is done with the files.
func (v *view) xdndFinishedIn(ce *C.XClientMessageEvent) {
	if !v.xds.active {
		return
	}
	ok := C.lythrum_cmsg_get(ce, 1) == 0
	v.xdndStop()
	if ok {
		v.setStatus(fmt.Sprintf("Передано файлов: %d", len(v.xds.paths)))
	}
}

// xdndURIList is the list of the files the board drags, in the format of the
// protocol.
func (v *view) xdndURIList() string {
	var b strings.Builder
	for _, p := range v.xds.paths {
		u := url.URL{Scheme: "file", Path: p}
		b.WriteString(u.String())
		b.WriteString("\r\n")
	}
	return b.String()
}

// onSelectionRequest answers an application that asks for the files the
// board drags.
func (v *view) onSelectionRequest(sr *C.XSelectionRequestEvent) {
	if sr.selection != atomXdndSelection || !v.xds.active {
		return
	}
	text := v.xdndURIList()
	prop := sr.property
	if prop == 0 {
		prop = sr.target
	}
	cs := C.CString(text)
	C.XChangeProperty(v.dpy, sr.target, prop, atomUriList, 8,
		C.PropModeReplace, (*C.uchar)(unsafe.Pointer(cs)), C.int(len(text)))
	C.free(unsafe.Pointer(cs))
	C.lythrum_selection_notify(v.dpy, sr.requestor, sr.selection,
		sr.target, prop, sr.time)
	C.XFlush(v.dpy)
}

// onSelectionNotify receives the list of the files the source drags.
func (v *view) onSelectionNotify(ev *C.XSelectionEvent) {
	if !v.xdn.waiting {
		return
	}
	v.xdn.waiting = false
	if ev.property == 0 {
		v.xdndFinish(false)
		return
	}
	var actualType C.Atom
	var actualFormat C.int
	var nitems, bytesAfter C.ulong
	var prop *C.uchar
	rc := C.XGetWindowProperty(v.dpy, v.win, v.xdn.prop, 0, 1<<18, C.True,
		C.AnyPropertyType, &actualType, &actualFormat, &nitems, &bytesAfter, &prop)
	if rc != C.Success || prop == nil {
		v.xdndFinish(false)
		return
	}
	text := C.GoStringN((*C.char)(unsafe.Pointer(prop)), C.int(nitems))
	C.XFree(unsafe.Pointer(prop))

	paths := parseURIList(text)
	if len(paths) == 0 {
		v.xdndFinish(false)
		v.setStatus("Принято пустое перетаскивание")
		return
	}
	// The files land in the folder under the pointer, or on the desktop.
	dst := v.root
	if t := v.xdndFolder(); t >= 0 {
		dst = v.icons[t].path
	}
	bx, by := v.rootToBoard(v.xdn.rootX, v.xdn.rootY)
	v.dropBX = v.ox + float64(bx)/v.zoom
	v.dropBY = v.oy + float64(by)/v.zoom
	v.xdndFinish(true)
	v.setStatus(fmt.Sprintf("Импорт: %d", len(paths)))
	go v.importFiles(paths, dst)
}

// applyImported shows the files that were copied in from a file manager.
func (v *view) applyImported(files []string) {
	if len(files) == 0 {
		v.setStatus("Импорт: ничего не скопировано")
		return
	}
	v.reload(false)
	for _, path := range files {
		for i, ic := range v.icons {
			if ic.path != path {
				continue
			}
			x, y := v.freeSpot(v.dropBX, v.dropBY)
			v.icons[i].x, v.icons[i].y = x, y
			break
		}
	}
	savePositions(v.root, v.icons)
	v.setStatus(fmt.Sprintf("Импортировано: %d", len(files)))
	v.redraw = true
}

// xdndFolder returns the folder the drag hovers over, if any.
func (v *view) xdndFolder() int {
	if !v.xdn.active {
		return -1
	}
	x, y := v.rootToBoard(v.xdn.rootX, v.xdn.rootY)
	return v.folderAt(x, y)
}

// rootToBoard converts the root coordinates of the pointer into the
// coordinates of the board.
func (v *view) rootToBoard(rx, ry int) (int, int) {
	var x, y C.int
	var child C.Window
	C.XTranslateCoordinates(v.dpy, C.XDefaultRootWindow(v.dpy), v.win,
		C.int(rx), C.int(ry), &x, &y, &child)
	return int(x), int(y)
}

// parseURIList reads the text/uri-list payload of a drop.
func parseURIList(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "file" {
			continue
		}
		p := u.Path
		if p == "" {
			continue
		}
		if dec, err := url.PathUnescape(p); err == nil {
			p = dec
		}
		out = append(out, p)
	}
	return out
}

// importFiles copies the dropped files into a folder of the board. The copy
// runs in the background: a large file must not freeze the board.
func (v *view) importFiles(paths []string, dst string) {
	var done []string
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		target := filepath.Join(dst, filepath.Base(p))
		if exists(target) {
			target = uniquePath(target)
		}
		if err := copyPath(p, target); err != nil {
			continue
		}
		_ = info
		done = append(done, target)
	}
	v.importDone <- done
}

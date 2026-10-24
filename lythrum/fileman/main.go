// Command fileman is the file manager of the Lythrum desktop: a small
// window that shows a folder and lets the user walk through it, open the
// files and drag them onto the board.
package main

/*
#cgo pkg-config: x11 xft
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <locale.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <X11/keysym.h>
#include <X11/Xutil.h>
#include <X11/Xft/Xft.h>

// The data of a client message is a union of five longs: cgo does not expose
// the members, so the shell reads and writes them through these helpers.
static long cmsg_get(XClientMessageEvent *e, int i) {
	return e->data.l[i];
}

static void cmsg_send(Display *dpy, Window w, Atom type,
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

static void selection_notify(Display *dpy, Window requestor, Atom selection,
                             Atom target, Atom property, Time time) {
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

// The input context translates a key into the characters of the user's
// locale, so a file can be renamed in any language and not only in ASCII.
static void fileman_init_locale(void) { setlocale(LC_ALL, ""); }
static XIM fileman_open_im(Display *dpy) {
	return XOpenIM(dpy, NULL, NULL, NULL);
}
static XIC fileman_create_ic(Display *dpy, XIM im, Window win) {
	if (im == NULL) return NULL;
	return XCreateIC(im, XNInputStyle, XIMPreeditNothing | XIMStatusNothing,
	                 XNClientWindow, win, XNFocusWindow, win, NULL);
}
static int fileman_lookup_utf8(XIC ic, XKeyEvent *ev, char *buf, int len) {
	if (ic == NULL) {
		KeySym ks;
		return XLookupString(ev, buf, len, &ks, NULL);
	}
	Status st;
	return Xutf8LookupString(ic, ev, buf, len, NULL, &st);
}
*/
import "C"

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unsafe"
)

const (
	rowH    = 26 // the height of a row of the list
	headH   = 34 // the height of the bar of the path
	iconSz  = 16 // the side of the mark of a file
	winW    = 720
	winH    = 480
	titlePX = 12
)

// The atoms of the drag and drop protocol.
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

const xaAtom = 4

// entry is a line of the list.
type entry struct {
	name  string
	path  string
	isDir bool
}

// The file manager.
type app struct {
	dpy    *C.Display
	screen C.int
	win    C.Window
	root   C.Window
	gc     C.GC
	pm     C.Pixmap
	dpict  C.Picture
	visual *C.Visual
	cmap   C.Colormap
	font   *C.XftFont
	xft    *C.XftDraw
	colors map[uint32]*C.XftColor

	w, h int

	dir        string
	history    []string
	entries    []entry
	hover      int
	pressed    int
	lastIdx    int
	lastAt     time.Time
	renaming   int // the line whose name is being typed, -1 when none
	renameText string

	// the input context that decodes a key into the text of the locale
	im C.XIM
	ic C.XIC
	// notice is the message of the last refused action
	notice string

	// the drag of a file over another application
	dragging      bool
	dragStart     bool
	dragPath      string
	dragTime      C.Time
	dragTargetWin C.Window
	pressX        int
	pressY        int
}

// The colours of the window.
var (
	colBg      = rgb(0x1b, 0x1e, 0x24)
	colPanel   = rgb(0x24, 0x28, 0x30)
	colText    = rgb(0xe8, 0xec, 0xf2)
	colMuted   = rgb(0x9a, 0xa3, 0xb2)
	colSel     = rgb(0x2b, 0x33, 0x42)
	colAccent  = rgb(0x4c, 0x8e, 0xf5)
	colFolder  = rgb(0xe0, 0xa8, 0x2e)
	colFile    = rgb(0x8a, 0x93, 0xa6)
	colBorder  = rgb(0x3a, 0x41, 0x4e)
	colHead    = rgb(0x22, 0x26, 0x2e)
	colDirChip = rgb(0x2f, 0x3a, 0x4a)
	colError   = rgb(0xe0, 0x6a, 0x6a)
)

type rgbT struct{ r, g, b byte }

func rgb(r, g, b byte) rgbT { return rgbT{r, g, b} }

func main() {
	dir := "."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	a := &app{dir: dir, hover: -1, pressed: -1, lastIdx: -1, renaming: -1}
	if err := a.run(); err != nil {
		fmt.Fprintln(os.Stderr, "fileman:", err)
		os.Exit(1)
	}
}

func (a *app) run() error {
	C.fileman_init_locale()
	a.dpy = C.XOpenDisplay(nil)
	if a.dpy == nil {
		return fmt.Errorf("cannot open display")
	}
	a.screen = C.XDefaultScreen(a.dpy)
	a.root = C.XDefaultRootWindow(a.dpy)
	a.visual = C.XDefaultVisual(a.dpy, a.screen)
	a.cmap = C.XDefaultColormap(a.dpy, a.screen)

	a.win = C.XCreateSimpleWindow(a.dpy, a.root, 80, 80, winW, winH, 0,
		0, C.ulong(pix(colBorder)))
	C.XStoreName(a.dpy, a.win, C.CString("Файлы — Lythrum"))
	C.XSelectInput(a.dpy, a.win,
		C.ExposureMask|C.KeyPressMask|C.ButtonPressMask|C.ButtonReleaseMask|
			C.PointerMotionMask|C.StructureNotifyMask)
	C.XMapWindow(a.dpy, a.win)

	a.w, a.h = winW, winH
	a.gc = C.XCreateGC(a.dpy, a.win, 0, nil)
	a.resize(a.w, a.h)
	a.font = C.XftFontOpenName(a.dpy, a.screen, C.CString("sans:size=11"))
	if a.font == nil {
		return fmt.Errorf("no font")
	}
	a.colors = map[uint32]*C.XftColor{}
	a.initAtoms()
	a.im = C.fileman_open_im(a.dpy)
	a.ic = C.fileman_create_ic(a.dpy, a.im, a.win)
	a.readDir()

	var ev C.XEvent
	for {
		C.XNextEvent(a.dpy, &ev)
		any := (*C.XAnyEvent)(unsafe.Pointer(&ev))
		switch any._type {
		case C.Expose:
			a.draw()
		case C.ConfigureNotify:
			ce := (*C.XConfigureEvent)(unsafe.Pointer(&ev))
			if int(ce.width) != a.w || int(ce.height) != a.h {
				a.resize(int(ce.width), int(ce.height))
				a.draw()
			}
		case C.ButtonPress:
			a.onPress((*C.XButtonEvent)(unsafe.Pointer(&ev)))
		case C.ButtonRelease:
			a.onRelease((*C.XButtonEvent)(unsafe.Pointer(&ev)))
		case C.MotionNotify:
			a.onMotion((*C.XMotionEvent)(unsafe.Pointer(&ev)))
		case C.KeyPress:
			a.onKey((*C.XKeyEvent)(unsafe.Pointer(&ev)))
		case C.ClientMessage:
			ce := (*C.XClientMessageEvent)(unsafe.Pointer(&ev))
			if ce.message_type == atomXdndStatus {
				// The answer of the application that takes the file.
			}
			if ce.message_type == atomXdndFinished {
				a.dragging = false
			}
		case C.SelectionRequest:
			a.onSelectionRequest((*C.XSelectionRequestEvent)(unsafe.Pointer(&ev)))
		}
	}
}

// initAtoms interns the atoms of the drag and drop protocol and tells the
// world that the window accepts drops.
func (a *app) initAtoms() {
	d := a.dpy
	atomXdndAware = C.XInternAtom(d, cstr("XdndAware"), C.False)
	atomXdndEnter = C.XInternAtom(d, cstr("XdndEnter"), C.False)
	atomXdndPosition = C.XInternAtom(d, cstr("XdndPosition"), C.False)
	atomXdndStatus = C.XInternAtom(d, cstr("XdndStatus"), C.False)
	atomXdndLeave = C.XInternAtom(d, cstr("XdndLeave"), C.False)
	atomXdndDrop = C.XInternAtom(d, cstr("XdndDrop"), C.False)
	atomXdndFinished = C.XInternAtom(d, cstr("XdndFinished"), C.False)
	atomXdndSelection = C.XInternAtom(d, cstr("XdndSelection"), C.False)
	atomXdndTypeList = C.XInternAtom(d, cstr("XdndTypeList"), C.False)
	atomXdndActionCopy = C.XInternAtom(d, cstr("XdndActionCopy"), C.False)
	atomUriList = C.XInternAtom(d, cstr("text/uri-list"), C.False)
	atomTextPlain = C.XInternAtom(d, cstr("text/plain;charset=utf-8"), C.False)
	val := C.long(5)
	C.XChangeProperty(d, a.win, atomXdndAware, C.Atom(xaAtom), 32,
		C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&val)), 1)
}

func cstr(s string) *C.char { return C.CString(s) }

func pix(c rgbT) int {
	return int(c.r)<<16 | int(c.g)<<8 | int(c.b)
}

// readDir reads the folder the window shows.
func (a *app) readDir() {
	a.entries = nil
	ents, err := os.ReadDir(a.dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		a.entries = append(a.entries, entry{
			name:  name,
			path:  filepath.Join(a.dir, name),
			isDir: e.IsDir(),
		})
	}
	sort.SliceStable(a.entries, func(i, j int) bool {
		if a.entries[i].isDir != a.entries[j].isDir {
			return a.entries[i].isDir
		}
		return strings.ToLower(a.entries[i].name) < strings.ToLower(a.entries[j].name)
	})
}

// resize gives the window its new size.
func (a *app) resize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if a.pm != 0 {
		C.XFreePixmap(a.dpy, a.pm)
	}
	a.pm = C.XCreatePixmap(a.dpy, a.win, C.uint(w), C.uint(h),
		C.uint(C.XDefaultDepth(a.dpy, a.screen)))
	if a.xft != nil {
		C.XftDrawDestroy(a.xft)
	}
	a.xft = C.XftDrawCreate(a.dpy, C.Drawable(a.pm), a.visual, a.cmap)
	a.w, a.h = w, h
}

// fill paints a rectangle of the window.
func (a *app) fill(x, y, w, h int, c rgbT) {
	C.XSetForeground(a.dpy, a.gc, C.ulong(pix(c)))
	C.XFillRectangle(a.dpy, a.pm, a.gc, C.int(x), C.int(y),
		C.uint(w), C.uint(h))
}

// text writes a line of text.
func (a *app) text(x, y int, s string, c rgbT) {
	xc := a.color(c)
	if xc == nil {
		return
	}
	cs := C.CString(s)
	C.XftDrawStringUtf8(a.xft, xc, a.font, C.int(x), C.int(y),
		(*C.FcChar8)(unsafe.Pointer(cs)), C.int(len(s)))
	C.free(unsafe.Pointer(cs))
}

// color returns the Xft colour of a colour of the palette.
func (a *app) color(c rgbT) *C.XftColor {
	key := uint32(pix(c))
	if xc, ok := a.colors[key]; ok {
		return xc
	}
	var rc C.XRenderColor
	rc.red = C.ushort(uint16(c.r) * 257)
	rc.green = C.ushort(uint16(c.g) * 257)
	rc.blue = C.ushort(uint16(c.b) * 257)
	rc.alpha = 0xffff
	p := (*C.XftColor)(C.malloc(C.size_t(unsafe.Sizeof(C.XftColor{}))))
	if C.XftColorAllocValue(a.dpy, a.visual, a.cmap, &rc, p) == 0 {
		C.free(unsafe.Pointer(p))
		return nil
	}
	a.colors[key] = p
	return p
}

// draw paints the whole window.
func (a *app) draw() {
	if a.pm == 0 {
		a.resize(a.w, a.h)
	}
	a.fill(0, 0, a.w, a.h, colBg)
	// the bar of the path
	a.fill(0, 0, a.w, headH, colHead)
	a.fill(0, headH-1, a.w, 1, colBorder)
	// the buttons: back, up, home
	a.button(10, 8, "back")
	a.button(34, 8, "up")
	a.button(58, 8, "home")
	// the path
	path := a.dir
	if home, err := os.UserHomeDir(); err == nil {
		path = strings.Replace(path, home, "~", 1)
	}
	a.text(88, 23, path, colText)
	// the list
	y := headH + 6
	for i, e := range a.entries {
		if y+rowH > a.h {
			break
		}
		if i == a.hover {
			a.fill(6, y, a.w-12, rowH, colSel)
		}
		c := colFile
		if e.isDir {
			c = colFolder
		}
		a.fill(14, y+(rowH-iconSz)/2, iconSz, iconSz, c)
		if e.isDir {
			a.fill(18, y+(rowH-iconSz)/2+4, iconSz-8, 3, colBg)
		}
		name := e.name
		if i == a.renaming {
			name = a.renameText + "▏"
		}
		a.text(40, y+18, name, colText)
		y += rowH
	}
	if a.notice != "" {
		a.text(10, a.h-10, a.notice, colError)
	}
	C.XCopyArea(a.dpy, a.pm, a.win, a.gc, 0, 0,
		C.uint(a.w), C.uint(a.h), 0, 0)
	C.XFlush(a.dpy)
}

// button paints one of the buttons of the bar.
func (a *app) button(x, y int, kind string) {
	a.fill(x, y, 20, 18, colDirChip)
	switch kind {
	case "back":
		// an arrow to the left
		a.fill(x+7, y+4, 2, 10, colAccent)
		a.fill(x+5, y+7, 2, 4, colAccent)
	case "up":
		a.fill(x+9, y+4, 2, 10, colAccent)
		a.fill(x+6, y+7, 8, 2, colAccent)
	case "home":
		a.fill(x+5, y+8, 10, 6, colAccent)
		a.fill(x+9, y+4, 2, 4, colAccent)
	}
}

// buttonAt returns the button of the bar under a point, or an empty string.
func (a *app) buttonAt(x, y int) string {
	if y < 0 || y >= headH {
		return ""
	}
	for _, b := range []struct {
		x    int
		kind string
	}{{10, "back"}, {34, "up"}, {58, "home"}} {
		if x >= b.x && x < b.x+20 {
			return b.kind
		}
	}
	return ""
}

// rowAt returns the entry under a point of the list.
func (a *app) rowAt(y int) int {
	if y < headH {
		return -1
	}
	i := (y - headH - 6) / rowH
	if i < 0 || i >= len(a.entries) {
		return -1
	}
	return i
}

// onPress reacts to a click.
func (a *app) onPress(be *C.XButtonEvent) {
	x, y := int(be.x), int(be.y)
	a.notice = ""
	if be.button == 1 {
		switch a.buttonAt(x, y) {
		case "back":
			a.goBack()
			return
		case "up":
			a.goUp()
			return
		case "home":
			a.goHome()
			return
		}
		i := a.rowAt(y)
		a.pressed = i
		a.pressX, a.pressY = x, y
		a.dragStart = false
		a.dragging = false
	}
}

// onRelease reacts to the end of a click: a double click opens the entry.
func (a *app) onRelease(be *C.XButtonEvent) {
	if be.button != 1 {
		return
	}
	y := int(be.y)
	i := a.rowAt(y)
	a.pressed = -1
	if i < 0 {
		return
	}
	if a.dragStart {
		// The file was dragged: the application under the pointer takes it.
		a.dragStart = false
		a.dragDrop(int(be.x_root), int(be.y_root))
		return
	}
	now := time.Now()
	if a.lastIdx == i && now.Sub(a.lastAt) < 400*time.Millisecond {
		a.open(i)
		a.lastIdx = -1
		return
	}
	a.lastIdx = i
	a.lastAt = now
}

// onMotion follows the pointer.
func (a *app) onMotion(me *C.XMotionEvent) {
	y := int(me.y)
	i := a.rowAt(y)
	if i != a.hover {
		a.hover = i
		a.draw()
	}
	if a.pressed >= 0 && a.pressed == i && me.state&C.Button1Mask != 0 {
		if !a.dragStart {
			dx := int(me.x) - a.pressX
			dy := int(me.y) - a.pressY
			if dx*dx+dy*dy < 16 {
				return
			}
			// The file is dragged: the board and the other applications may
			// take it.
			a.dragStart = true
			a.dragging = true
			a.dragPath = a.entries[i].path
			a.dragTime = C.CurrentTime
			a.ownSelection()
		}
		a.dragMove(int(me.x_root), int(me.y_root))
	}
}

// onKey reacts to a key.
func (a *app) onKey(ke *C.XKeyEvent) {
	ks := C.XLookupKeysym(ke, 0)
	if a.renaming >= 0 {
		a.renameKey(ke, ks)
		return
	}
	a.notice = ""
	switch ks {
	case 0xff1b: // Escape
		C.XCloseDisplay(a.dpy)
		os.Exit(0)
	case 0xff08: // Backspace
		a.goUp()
	case 0xff54: // Down
		if a.hover < len(a.entries)-1 {
			a.hover++
			a.draw()
		}
	case 0xff52: // Up
		if a.hover > 0 {
			a.hover--
			a.draw()
		}
	case 0xff0d: // Return
		if a.hover >= 0 {
			a.open(a.hover)
		}
	case 0xffbe: // F2: rename
		if a.hover >= 0 {
			a.rename(a.hover)
		}
	case 0xffff: // Delete
		if a.hover >= 0 {
			a.remove(a.hover)
		}
	}
}

// open enters a folder or opens a file.
func (a *app) open(i int) {
	if i < 0 || i >= len(a.entries) {
		return
	}
	e := a.entries[i]
	if e.isDir {
		a.history = append(a.history, a.dir)
		a.dir = e.path
		a.readDir()
		a.hover = -1
		a.draw()
		return
	}
	cmd := exec.Command("xdg-open", e.path)
	cmd.Start()
}

// goBack returns to the folder the window came from.
func (a *app) goBack() {
	if len(a.history) == 0 {
		return
	}
	dir := a.history[len(a.history)-1]
	a.history = a.history[:len(a.history)-1]
	a.dir = dir
	a.readDir()
	a.hover = -1
	a.draw()
}

// goHome opens the home folder of the user.
func (a *app) goHome() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	a.history = append(a.history, a.dir)
	a.dir = home
	a.readDir()
	a.hover = -1
	a.draw()
}

// rename asks for another name of a file: the name is typed in the window
// itself, over the line of the file.
func (a *app) rename(i int) {
	if i < 0 || i >= len(a.entries) {
		return
	}
	a.notice = ""
	a.renaming = i
	a.renameText = a.entries[i].name
	a.draw()
}

// validName reports whether name can be used for a file of the shown folder.
// A name is a single path element: no separators, no dot entries.
func validName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 255 {
		return false
	}
	if strings.ContainsAny(name, "/\x00") {
		return false
	}
	return filepath.Base(name) == name
}

// renameKey takes a key while a name is being typed.
func (a *app) renameKey(ke *C.XKeyEvent, ks C.KeySym) bool {
	switch ks {
	case 0xff1b: // Escape: leave the name alone
		a.renaming = -1
		a.renameText = ""
		a.draw()
		return true
	case 0xff0d: // Return: the new name
		i := a.renaming
		name := strings.TrimSpace(a.renameText)
		a.renaming = -1
		a.renameText = ""
		if i < 0 || i >= len(a.entries) {
			a.draw()
			return true
		}
		if !validName(name) {
			a.notice = "недопустимое имя"
			a.draw()
			return true
		}
		e := a.entries[i]
		if name != e.name {
			target := filepath.Join(a.dir, name)
			// os.Rename overwrites the target: never lose an existing file.
			if _, err := os.Lstat(target); err == nil {
				a.notice = "уже существует: " + name
			} else if err := os.Rename(e.path, target); err != nil {
				a.notice = "не удалось переименовать"
			}
		}
		a.readDir()
		a.draw()
		return true
	case 0xff08: // Backspace: drop a whole character, not a byte
		if r := []rune(a.renameText); len(r) > 0 {
			a.renameText = string(r[:len(r)-1])
			a.draw()
		}
		return true
	}
	// The text of the key in the locale of the user: Unicode, not ASCII.
	var buf [64]C.char
	n := C.fileman_lookup_utf8(a.ic, ke, &buf[0], C.int(len(buf)))
	if n <= 0 {
		return false
	}
	text := C.GoStringN(&buf[0], n)
	if len(a.renameText)+len(text) > 255 {
		return false
	}
	changed := false
	for _, r := range text {
		// Control characters and path separators have no place in a name.
		if r < 0x20 || r == 0x7f || r == '/' {
			continue
		}
		a.renameText += string(r)
		changed = true
	}
	if changed {
		a.draw()
	}
	return changed
}

// remove puts a file in the trash. A failed move is reported, never turned
// into a permanent delete.
func (a *app) remove(i int) {
	if i < 0 || i >= len(a.entries) {
		return
	}
	a.notice = ""
	e := a.entries[i]
	trash := filepath.Join(a.home(), ".local", "share", "Trash", "files")
	if err := os.MkdirAll(trash, 0755); err != nil {
		a.notice = "корзина недоступна"
		a.draw()
		return
	}
	base := filepath.Base(e.path)
	target := filepath.Join(trash, base)
	if _, err := os.Lstat(target); err == nil {
		target = filepath.Join(trash, fmt.Sprintf("%d-%s", time.Now().Unix(), base))
	}
	if err := os.Rename(e.path, target); err != nil {
		a.notice = "не удалось переместить в корзину"
		a.draw()
		return
	}
	a.readDir()
	a.draw()
}

// home returns the home folder of the user.
func (a *app) home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "."
}

// goUp goes to the parent folder.
func (a *app) goUp() {
	parent := filepath.Dir(a.dir)
	if parent == a.dir {
		return
	}
	a.history = append(a.history, a.dir)
	a.dir = parent
	a.readDir()
	a.hover = -1
	a.draw()
}

// ownSelection takes the selection with the dragged file.
func (a *app) ownSelection() {
	list := []C.Atom{atomUriList, atomTextPlain}
	buf := make([]byte, 4*len(list))
	for i, at := range list {
		buf[i*4] = byte(uint32(at))
		buf[i*4+1] = byte(uint32(at) >> 8)
		buf[i*4+2] = byte(uint32(at) >> 16)
		buf[i*4+3] = byte(uint32(at) >> 24)
	}
	C.XChangeProperty(a.dpy, a.win, atomXdndTypeList, C.XA_ATOM, 32,
		C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&buf[0])), C.int(len(list)))
	C.XSetSelectionOwner(a.dpy, atomXdndSelection, a.win, a.dragTime)
	C.XFlush(a.dpy)
}

// dragTarget returns the window under the pointer that takes drops.
func (a *app) dragTarget() C.Window {
	var root, child C.Window
	var rx, ry, wx, wy C.int
	var mask C.uint
	C.XQueryPointer(a.dpy, a.root, &root, &child, &rx, &ry, &wx, &wy, &mask)
	if child == 0 {
		return 0
	}
	w := child
	for i := 0; i < 12 && w != 0; i++ {
		if a.aware(w) {
			return w
		}
		var r, parent C.Window
		var children *C.Window
		var n C.uint
		if C.XQueryTree(a.dpy, w, &r, &parent, &children, &n) != C.True {
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

// aware tells whether a window takes drops.
func (a *app) aware(w C.Window) bool {
	var actualType C.Atom
	var actualFormat C.int
	var nitems, bytesAfter C.ulong
	var prop *C.uchar
	rc := C.XGetWindowProperty(a.dpy, w, atomXdndAware, 0, 1, C.False,
		xaAtom, &actualType, &actualFormat, &nitems, &bytesAfter, &prop)
	if rc != C.Success || prop == nil {
		return false
	}
	C.XFree(unsafe.Pointer(prop))
	return nitems > 0
}

// dragMove follows the pointer over the target of the drag.
func (a *app) dragMove(rx, ry int) {
	if !a.dragging {
		return
	}
	target := a.dragTarget()
	if target != a.dragTargetWin {
		if a.dragTargetWin != 0 {
			C.cmsg_send(a.dpy, a.dragTargetWin, atomXdndLeave,
				C.long(a.dragTargetWin), 0, 0, 0, 0)
		}
		a.dragTargetWin = target
		if target != 0 {
			C.cmsg_send(a.dpy, target, atomXdndEnter,
				C.long(target), 0, 0, C.long(atomUriList), C.long(a.dragTime))
		}
		C.XFlush(a.dpy)
	}
	if a.dragTargetWin == 0 {
		return
	}
	C.cmsg_send(a.dpy, a.dragTargetWin, atomXdndPosition,
		C.long(a.dragTargetWin), 0, C.long((rx<<16)|(ry&0xffff)),
		C.long(1), C.long(a.dragTime))
	C.XFlush(a.dpy)
}

// dragDrop leaves the dragged file on the application under the pointer.
func (a *app) dragDrop(rx, ry int) {
	if !a.dragging {
		return
	}
	if a.dragTargetWin != 0 {
		C.cmsg_send(a.dpy, a.dragTargetWin, atomXdndDrop,
			C.long(a.dragTargetWin), 0, C.long((rx<<16)|(ry&0xffff)),
			C.long(a.dragTime), 0)
		C.XFlush(a.dpy)
		return
	}
	a.dragging = false
}

// onSelectionRequest answers an application that asks for the dragged file.
func (a *app) onSelectionRequest(sr *C.XSelectionRequestEvent) {
	if sr.selection != atomXdndSelection || !a.dragging {
		return
	}
	u := url.URL{Scheme: "file", Path: a.dragPath}
	text := u.String() + "\r\n"
	prop := sr.property
	if prop == 0 {
		prop = sr.target
	}
	cs := C.CString(text)
	C.XChangeProperty(a.dpy, sr.target, prop, atomUriList, 8,
		C.PropModeReplace, (*C.uchar)(unsafe.Pointer(cs)), C.int(len(text)))
	C.free(unsafe.Pointer(cs))
	C.selection_notify(a.dpy, sr.requestor, sr.selection, sr.target,
		prop, sr.time)
	C.XFlush(a.dpy)
}

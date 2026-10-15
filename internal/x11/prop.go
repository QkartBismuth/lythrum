package x11

/*
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/Xatom.h>
#include <X11/Xatom.h>

static int ly_get_property(Display *dpy, Window w, Atom property, Atom req_type,
                           long max_elements,
                           Atom *out_type, int *out_format, unsigned long *out_items,
                           unsigned long *out_after, unsigned char **out_data) {
	int format = 0;
	Atom type = 0;
	unsigned long items = 0, after = 0;
	unsigned char *data = 0;
	int rc = XGetWindowProperty(dpy, w, property, 0, max_elements,
		False, req_type, &type, &format, &items, &after, &data);
	*out_type = type;
	*out_format = format;
	*out_items = items;
	*out_after = after;
	*out_data = data;
	return rc;
}

static int ly_set_property(Display *dpy, Window w, Atom property, Atom type, int format,
                           const void *data, int nelements) {
	// Xlib does not necessarily copy the payload when the request is queued:
	// the transport may read straight from the caller's buffer when it
	// eventually writes the request. The buffer therefore has to be one Xlib
	// owns for the whole round trip, so copy it and only release it once the
	// request has actually been sent.
	int bytes = nelements * (format / 8);
	unsigned char *copy = (unsigned char *)malloc(bytes > 0 ? bytes : 1);
	int rc;
	if (!copy)
		return 0;
	memcpy(copy, data, bytes);
	rc = XChangeProperty(dpy, w, property, type, format, PropModeReplace, copy, nelements);
	if (rc)
		XSync(dpy, False);
	free(copy);
	return rc;
}

typedef void ly_copy_atoms_impl(Atom *src, int *n, unsigned long *dst);

static void ly_copy_atoms(Atom *src, int *n, unsigned long *dst) {
	int i;
	for (i = 0; i < *n; i++)
		dst[i] = (unsigned long)src[i];
}

typedef struct {
	char *name;
	int   state;
} ly_text;

static ly_text *ly_get_text(Display *dpy, Window w, Atom prop, int *encoding) {
	XTextProperty tp;
	ly_text *out = (ly_text *)calloc(1, sizeof(ly_text));
	if (!out)
		return 0;
	if (XGetTextProperty(dpy, w, &tp, prop) == 0) {
		out->name = 0;
		out->state = 0;
		*encoding = 0;
		return out;
	}
	out->name = tp.value; // freed by caller with XFree
	out->state = (int)tp.format;
	*encoding = (int)tp.encoding;
	return out;
}
*/
import "C"

import (
	"errors"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// Property is a raw window property as stored by the server.
type Property struct {
	Type   Atom
	Format int // 8, 16 or 32 bits per item
	Data   []byte
}

// Items returns the number of items in the property.
func (p *Property) Items() int {
	switch p.Format {
	case 8:
		return len(p.Data)
	case 16:
		return len(p.Data) / 2
	case 32:
		return len(p.Data) / 4
	}
	return 0
}

// Cardinal32 decodes a 32-bit property into uint32s.
func (p *Property) Cardinal32() []uint32 {
	out := make([]uint32, 0, p.Items())
	for i := 0; i+4 <= len(p.Data); i += 4 {
		out = append(out, u32(p.Data[i:]))
	}
	return out
}

// Cardinal16 decodes a 16-bit property into uint16s.
func (p *Property) Cardinal16() []uint16 {
	out := make([]uint16, 0, p.Items())
	for i := 0; i+2 <= len(p.Data); i += 2 {
		out = append(out, u16(p.Data[i:]))
	}
	return out
}

// defaultPropertyUnits is the reply size used when the caller does not care.
// The X protocol counts it in 32-bit units.
const defaultPropertyUnits = 4096

// GetProperty reads a property. A reqType of 0 means "any type". maxElements
// bounds the reply in 32-bit units; 0 means "up to defaultPropertyUnits".
func (d *Display) GetProperty(w Window, prop, reqType Atom, maxElements int) (*Property, error) {
	if d.dpy == nil {
		return nil, errClosed
	}
	if maxElements <= 0 {
		maxElements = defaultPropertyUnits
	}
	var (
		typ    C.Atom
		format C.int
		items  C.ulong
		after  C.ulong
		data   *C.uchar
	)
	rc := C.ly_get_property(d.dpy, C.Window(w), C.Atom(prop), C.Atom(reqType),
		C.long(maxElements), &typ, &format, &items, &after, &data)
	if rc != C.Success {
		return nil, decodeStatus(int(rc))
	}
	defer C.XFree(unsafe.Pointer(data))
	if data == nil {
		return nil, nil
	}
	n := int(items) * int(format) / 8
	return &Property{Type: Atom(typ), Format: int(format), Data: C.GoBytes(unsafe.Pointer(data), C.int(n))}, nil
}

// SetProperty replaces a property with data of the given type and format.
func (d *Display) SetProperty(w Window, prop, typ Atom, format int, data []byte) error {
	if d.dpy == nil {
		return errClosed
	}
	var p unsafe.Pointer
	if len(data) > 0 {
		p = unsafe.Pointer(&data[0])
	}
	// XChangeProperty takes the number of items, not the number of bytes.
	items := len(data) / (format / 8)
	// XChangeProperty returns a Status: non-zero means success.
	rc := C.ly_set_property(d.dpy, C.Window(w), C.Atom(prop), C.Atom(typ), C.int(format), p, C.int(items))
	if rc == 0 {
		return decodeStatus(C.BadValue)
	}
	d.Flush()
	return nil
}

// DeleteProperty removes a property from a window.
func (d *Display) DeleteProperty(w Window, prop Atom) {
	if d.dpy == nil {
		return
	}
	C.XDeleteProperty(d.dpy, C.Window(w), C.Atom(prop))
	d.Flush()
}

// Properties lists the property atoms set on a window.
func (d *Display) Properties(w Window) ([]Atom, error) {
	if d.dpy == nil {
		return nil, errClosed
	}
	var num C.int
	arr := C.XListProperties(d.dpy, C.Window(w), &num)
	if arr == nil {
		return nil, ErrBadWindow
	}
	defer C.XFree(unsafe.Pointer(arr))
	if int(num) > 4096 {
		return nil, ErrBadValue
	}
	buf := make([]C.ulong, int(num))
	C.ly_copy_atoms(arr, &num, &buf[0])
	out := make([]Atom, 0, int(num))
	for _, v := range buf {
		out = append(out, Atom(v))
	}
	return out, nil
}

// GetCardinals reads a 32-bit list property.
func (d *Display) GetCardinals(w Window, prop Atom) ([]uint32, error) {
	p, err := d.GetProperty(w, prop, 0, 0)
	if err != nil || p == nil {
		return nil, err
	}
	return p.Cardinal32(), nil
}

// SetCardinals writes a 32-bit list property.
func (d *Display) SetCardinals(w Window, prop, typ Atom, vals []uint32) error {
	buf := make([]byte, 4*len(vals))
	for i, v := range vals {
		put32(buf[4*i:], v)
	}
	return d.SetProperty(w, prop, typ, 32, buf)
}

// SetCardinal writes a single 32-bit value.
func (d *Display) SetCardinal(w Window, prop, typ Atom, v uint32) error {
	return d.SetCardinals(w, prop, typ, []uint32{v})
}

// SetAtomValue stores an atom in a property using the CARDINAL[] encoding,
// which is how EWMH stores atom-valued properties such as
// _NET_SUPPORTING_WM_CHECK.
func (d *Display) SetAtomValue(w Window, prop, typ Atom, v Atom) error {
	return d.SetCardinal(w, prop, typ, uint32(v))
}

// GetAtomValue reads a single atom stored in a CARDINAL property.
func (d *Display) GetAtomValue(w Window, prop Atom) (Atom, bool) {
	vals, err := d.GetCardinals(w, prop)
	if err != nil || len(vals) == 0 {
		return 0, false
	}
	return Atom(vals[0]), true
}

// SetUTF8 stores a UTF-8 string in a UTF8_STRING property.
func (d *Display) SetUTF8(w Window, prop Atom, s string) error {
	return d.SetProperty(w, prop, A.UTF8String, 8, []byte(s))
}

// GetUTF8 reads a UTF-8 string property.
func (d *Display) GetUTF8(w Window, prop Atom) (string, bool) {
	p, err := d.GetProperty(w, prop, A.UTF8String, 0)
	if err != nil || p == nil {
		return "", false
	}
	return string(p.Data), true
}

// GetText reads an ICCCM text property (WM_NAME, WM_ICON_NAME, WM_CLASS...).
func (d *Display) GetText(w Window, prop Atom) (string, Atom, bool) {
	if d.dpy == nil {
		return "", 0, false
	}
	var enc C.int
	txt := C.ly_get_text(d.dpy, C.Window(w), C.Atom(prop), &enc)
	if txt == nil {
		return "", 0, false
	}
	defer func() {
		if txt.name != nil {
			C.XFree(unsafe.Pointer(txt.name))
		}
		C.free(unsafe.Pointer(txt))
	}()
	if txt.name == nil {
		return "", 0, false
	}
	encoding := Atom(enc)
	s := goString(txt.name)
	if encoding != A.UTF8String {
		s = latin1ToUTF8(s)
	}
	if !utf8.ValidString(s) {
		s = latin1ToUTF8(s)
	}
	return s, encoding, true
}

// latin1ToUTF8 widens a byte string treated as ISO-8859-1 into UTF-8.
func latin1ToUTF8(s string) string {
	if isASCII(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) * 2)
	for i := 0; i < len(s); i++ {
		b.WriteRune(rune(s[i]))
	}
	return b.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func u16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }

func u32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func put32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func u16at(b []byte, off int) uint16 { return u16(b[off:]) }

func u32at(b []byte, off int) uint32 { return u32(b[off:]) }

func i16at(b []byte, off int) int16 { return int16(u16(b[off:])) }

func decodeStatus(rc int) error {
	switch rc {
	case C.Success:
		return nil
	case C.BadAtom:
		return ErrAtomNotFound
	case C.BadWindow:
		return ErrBadWindow
	case C.BadMatch:
		return ErrBadMatch
	case C.BadDrawable:
		return ErrBadDrawable
	case C.BadAlloc:
		return ErrBadAlloc
	case C.BadLength:
		return ErrBadLength
	case C.BadValue:
		return ErrBadValue
	case C.BadAccess:
		return ErrBadAccess
	}
	return errors.New("x11: request failed")
}

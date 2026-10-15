package x11

/*
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Errors reported by the X server (see protocol-consts.h in the X sources).
var (
	ErrAtomNotFound = errors.New("x11: atom not found")
	ErrBadWindow    = errors.New("x11: bad window")
	ErrBadMatch     = errors.New("x11: bad match")
	ErrBadDrawable  = errors.New("x11: bad drawable")
	ErrBadAlloc     = errors.New("x11: bad alloc")
	ErrNoProperty   = errors.New("x11: property does not exist")
	ErrBadLength    = errors.New("x11: bad length")
	ErrBadValue     = errors.New("x11: bad value")
	ErrBadAccess    = errors.New("x11: bad access")
	ErrNotViewable  = errors.New("x11: not viewable")
)

// ServerError carries a decoded X protocol error.
type ServerError struct {
	Code        byte
	CodeName    string
	Sequence    uint16
	ResourceID  uint32
	MinorOpcode uint16
	MajorOpcode uint8
	Message     string
}

func (e *ServerError) Error() string { return "x11: " + e.Message }

var errorNames = map[byte]string{
	1:  "BadRequest",
	2:  "BadValue",
	3:  "BadWindow",
	4:  "BadPixmap",
	5:  "BadAtom",
	6:  "BadCursor",
	7:  "BadMatch",
	8:  "BadDrawable",
	9:  "BadAccess",
	10: "BadAlloc",
	11: "BadColor",
	12: "BadGC",
	13: "BadIDChoice",
	14: "BadName",
	15: "BadLength",
	16: "BadImplementation",
}

// AsServerError converts an error returned by this package into a
// *ServerError, if it is one.
func AsServerError(err error) (*ServerError, bool) {
	var se *ServerError
	if errors.As(err, &se) {
		return se, true
	}
	return nil, false
}

func cstr(s string) *C.char { return C.CString(s) }

func toCBool(b bool) C.int {
	if b {
		return C.int(1)
	}
	return C.int(0)
}

func goString(cs *C.char) string {
	if cs == nil {
		return ""
	}
	return C.GoString(cs)
}

func bytesToCString(p []byte) *C.char {
	if len(p) == 0 {
		return C.CString("")
	}
	return (*C.char)(unsafe.Pointer(&p[0]))
}

package webview

/*
#include <stdint.h>
#include <stdlib.h>

// lvGoFrame, lvGoTitle, lvGoScheme, lvGoConsole, lvGoLoad and lvGoTerminated
// are implemented in Go and called by the GTK thread.
*/
import "C"

import (
	"unsafe"
)

// cString copies a Go string into memory the C side frees.
func cString(s string) *C.char {
	if s == "" {
		return nil
	}
	return C.CString(s)
}

// cBytes copies Go bytes into a C buffer that the caller frees with free.
func cBytes(b []byte) *C.char {
	if len(b) == 0 {
		return nil
	}
	return (*C.char)(C.CBytes(b))
}

//export lvGoFrame
func lvGoFrame(handle C.uintptr_t, data unsafe.Pointer, width, height C.int, swapped C.int) {
	e := currentEngine()
	if e == nil {
		return
	}
	v := e.lookup(uintptr(handle))
	if v == nil || data == nil {
		return
	}
	size := int(width) * int(height) * 4
	if size <= 0 || size > 64<<20 {
		return
	}
	v.putFrame(C.GoBytes(data, C.int(size)), int(width), int(height))
}

//export lvGoTitle
func lvGoTitle(handle C.uintptr_t, title *C.char) {
	e := currentEngine()
	if e == nil {
		return
	}
	if v := e.lookup(uintptr(handle)); v != nil {
		v.putTitle(C.GoString(title))
		e.log("webview: title %q", C.GoString(title))
	}
}

//export lvGoLoad
func lvGoLoad(handle C.uintptr_t, event C.int) {
	e := currentEngine()
	if e == nil {
		return
	}
	if v := e.lookup(uintptr(handle)); v != nil {
		v.putLoad(int(event))
	}
}

//export lvGoTerminated
func lvGoTerminated(handle C.uintptr_t, reason C.int) {
	e := currentEngine()
	if e == nil {
		return
	}
	if v := e.lookup(uintptr(handle)); v != nil {
		v.putCrash(int(reason))
	}
}

//export lvGoScheme
func lvGoScheme(url, method, fileRange *C.char, request C.uintptr_t) {
	e := currentEngine()
	if e == nil {
		return
	}
	e.serve(SchemeRequest{
		URL:    C.GoString(url),
		Method: C.GoString(method),
		Range:  C.GoString(fileRange),
	}, uintptr(request))
}

//export lvGoMessage
func lvGoMessage(handle C.uintptr_t, payload *C.char) {
	e := currentEngine()
	if e == nil {
		return
	}
	if v := e.lookup(uintptr(handle)); v != nil {
		e.dispatch(v, C.GoString(payload))
	}
}

func currentEngine() *Engine {
	engineMu.Lock()
	defer engineMu.Unlock()
	return engine
}

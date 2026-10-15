package x11

/*
#include <X11/Xlib.h>
#include <X11/keysym.h>
*/
import "C"

func keySymToString(ks KeySym) string {
	if ks == 0 {
		return ""
	}
	s := C.XKeysymToString(C.KeySym(ks))
	if s == nil {
		return ""
	}
	return C.GoString(s)
}

func keySymFromString(name string) KeySym {
	if name == "" {
		return 0
	}
	cname := C.CString(name)
	defer freeCString(cname)
	return KeySym(C.XStringToKeysym(cname))
}

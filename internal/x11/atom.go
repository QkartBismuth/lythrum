package x11

/*
#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>
*/
import "C"

import (
	"unsafe"
)

type atomCache struct {
	m map[string]Atom
	r map[Atom]string
}

func (c *atomCache) init(d *Display) {
	c.m = make(map[string]Atom, 128)
	c.r = make(map[Atom]string, 128)
	for _, name := range []string{
		"PRIMARY", "SECONDARY", "ATOM", "CARDINAL", "INTEGER", "STRING", "WM_NAME",
	} {
		c.predef(d, name)
	}
}

// predef interns the well-known predefined atoms; they are never freed.
func (c *atomCache) predef(d *Display, name string) {
	if _, ok := c.m[name]; ok {
		return
	}
	c.remember(Atom(C.XInternAtom(d.dpy, cstr(name), 1)), name)
}

func (c *atomCache) get(d *Display, name string, onlyIfExists bool) (Atom, error) {
	if a, ok := c.m[name]; ok {
		return a, nil
	}
	cf := C.CString(name)
	defer C.free(unsafe.Pointer(cf))
	a := Atom(C.XInternAtom(d.dpy, cf, toCBool(onlyIfExists)))
	if a == 0 {
		return 0, ErrAtomNotFound
	}
	c.remember(a, name)
	return a, nil
}

func (c *atomCache) remember(a Atom, name string) {
	c.m[name] = a
	c.r[a] = name
}

func (c *atomCache) name(a Atom) (string, bool) {
	n, ok := c.r[a]
	return n, ok
}

// Atom interns an atom name, caching the result.
func (d *Display) Atom(name string) Atom {
	a, err := d.atoms.get(d, name, false)
	if err != nil {
		return 0
	}
	return a
}

// AtomExisting interns a name only if the server already knows it.
func (d *Display) AtomExisting(name string) (Atom, bool) {
	d.atoms.predef(d, name)
	a, err := d.atoms.get(d, name, true)
	if err != nil {
		return 0, false
	}
	return a, true
}

// Name resolves an atom back to its name using the server's atom table.
func (d *Display) Name(a Atom) string {
	if a == 0 {
		return ""
	}
	if n, ok := d.atoms.name(a); ok {
		return n
	}
	name := C.XGetAtomName(d.dpy, C.Atom(a))
	if name == nil {
		return ""
	}
	defer C.XFree(unsafe.Pointer(name))
	s := C.GoString(name)
	d.atoms.remember(a, s)
	return s
}

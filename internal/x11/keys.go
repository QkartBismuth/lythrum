package x11

/*
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/keysym.h>
#include <X11/XKBlib.h>

static KeySym *ly_keyboard_mapping(Display *dpy, int first, int count, int *out_per) {
	int per = 0;
	KeySym *ks = XGetKeyboardMapping(dpy, (KeyCode)first, count, &per);
	*out_per = per;
	return ks;
}

static void ly_copy_keysyms(KeySym *src, int n, unsigned long *dst) {
	int i;
	for (i = 0; i < n; i++)
		dst[i] = (unsigned long)src[i];
}

// ly_mod_map fills out[] with the modifier bits each keycode activates.
static int ly_mod_map(Display *dpy, int first, int count, unsigned long *out) {
	int r, i, per;
	XModifierKeymap *map = XGetModifierMapping(dpy);
	if (!map)
		return 0;
	per = map->max_keypermod;
	for (i = 0; i < count; i++)
		out[i] = 0;
	for (r = 0; r < 8; r++) {
		unsigned long bit = 1UL << r;
		for (i = 0; i < per; i++) {
			int idx = (int)map->modifiermap[r * per + i] - first;
			if (idx >= 0 && idx < count)
				out[idx] |= bit;
		}
	}
	return 1;
}
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

// ModMask is a set of keyboard modifier bits (X.h ModMask).
type ModMask uint16

// Modifier bits.
const (
	ModNone     ModMask = 0
	ShiftMask   ModMask = 1 << 0
	LockMask    ModMask = 1 << 1
	ControlMask ModMask = 1 << 2
	Mod1Mask    ModMask = 1 << 3 // Alt
	Mod2Mask    ModMask = 1 << 4 // Num Lock on most layouts
	Mod3Mask    ModMask = 1 << 5 // Super / AltGr on some layouts
	Mod4Mask    ModMask = 1 << 6 // Super
	Mod5Mask    ModMask = 1 << 7
	AnyModifier ModMask = 0x8000
)

var modNames = map[string]ModMask{
	"shift":   ShiftMask,
	"lock":    LockMask,
	"ctrl":    ControlMask,
	"control": ControlMask,
	"alt":     Mod1Mask,
	"mod1":    Mod1Mask,
	"mod2":    Mod2Mask,
	"mod3":    Mod3Mask,
	"super":   Mod4Mask,
	"mod4":    Mod4Mask,
	"win":     Mod4Mask,
	"meta":    Mod1Mask,
	"mod5":    Mod5Mask,
	"hyper":   Mod4Mask,
	"any":     AnyModifier,
}

// ParseModMask parses a '+' separated modifier list such as "Mod4+Shift".
func ParseModMask(s string) (ModMask, error) {
	var mask ModMask
	s = strings.TrimSpace(s)
	if s == "" {
		return ModNone, nil
	}
	for _, part := range strings.Split(s, "+") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		m, ok := modNames[part]
		if !ok {
			return 0, fmt.Errorf("x11: unknown modifier %q", part)
		}
		mask |= m
	}
	return mask, nil
}

// String renders the modifier mask using canonical names.
func (m ModMask) String() string {
	if m == ModNone {
		return "none"
	}
	names := []struct {
		mask ModMask
		name string
	}{
		{ShiftMask, "shift"}, {LockMask, "lock"}, {ControlMask, "ctrl"},
		{Mod1Mask, "alt"}, {Mod2Mask, "mod2"}, {Mod3Mask, "mod3"},
		{Mod4Mask, "super"}, {Mod5Mask, "mod5"},
	}
	var parts []string
	for _, n := range names {
		if m&n.mask != 0 {
			parts = append(parts, n.name)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("0x%x", uint16(m))
	}
	return strings.Join(parts, "+")
}

// Binding identifies a key combination: a key symbol and a modifier mask.
type Binding struct {
	Mods ModMask
	Sym  KeySym
}

// String renders a binding as "Mod4+Return", suitable for configuration files.
func (b Binding) String() string {
	sym := KeySymName(b.Sym)
	if b.Mods == ModNone {
		return sym
	}
	return b.Mods.String() + "+" + sym
}

// ParseBinding parses a key combination such as "Mod4+Return" or "Super+j".
func ParseBinding(s string) (Binding, error) {
	parts := strings.Split(s, "+")
	if len(parts) == 0 {
		return Binding{}, fmt.Errorf("x11: empty binding")
	}
	var mods ModMask
	var symName string
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if m, ok := modNames[strings.ToLower(p)]; ok {
			mods |= m
			continue
		}
		if symName != "" {
			return Binding{}, fmt.Errorf("x11: binding %q has more than one key", s)
		}
		symName = p
		_ = i
	}
	if symName == "" {
		return Binding{}, fmt.Errorf("x11: binding %q has no key", s)
	}
	sym, err := KeySymFromName(symName)
	if err != nil {
		return Binding{}, err
	}
	return Binding{Mods: mods, Sym: sym}, nil
}

// KeySymName converts a key symbol to its X name ("Return", "a", "XF86AudioPlay"...).
func KeySymName(ks KeySym) string { return keySymToString(ks) }

// KeySymFromName converts an X key name to a key symbol.
func KeySymFromName(name string) (KeySym, error) {
	ks := keySymFromString(name)
	if ks == 0 {
		return 0, fmt.Errorf("x11: unknown key %q", name)
	}
	return ks, nil
}

// keymap caches the keyboard mapping so key events can be translated to
// bindings without a round-trip per keystroke.
type keymap struct {
	first      KeyCode
	last       KeyCode
	perKeycode int
	syms       []KeySym
	mods       []ModMask // modifiers active when the key is pressed
	haveMods   bool
}

var currentKeymap keymap

// RefreshKeymap reloads the keyboard mapping from the server. It must be
// called again whenever a MappingNotify event arrives.
func (d *Display) RefreshKeymap() error {
	if d.dpy == nil {
		return errClosed
	}
	var minCode, maxCode C.int
	C.XDisplayKeycodes(d.dpy, &minCode, &maxCode)
	min, max := int(minCode), int(maxCode)
	if max < min {
		return ErrBadValue
	}
	var per C.int
	ks := C.ly_keyboard_mapping(d.dpy, C.int(min), C.int(max-min+1), &per)
	if ks == nil || per == 0 {
		return ErrBadValue
	}
	defer C.XFree(unsafe.Pointer(ks))

	count := int(max - min + 1)
	raw := make([]C.ulong, count*int(per))
	C.ly_copy_keysyms(ks, C.int(len(raw)), &raw[0])
	syms := make([]KeySym, len(raw))
	for i, v := range raw {
		syms[i] = KeySym(v)
	}
	// Assign the mapping first: loadModMap fills the modifier table of the
	// package level keymap, which a later assignment would throw away.
	currentKeymap = keymap{
		first:      KeyCode(min),
		last:       KeyCode(max),
		perKeycode: int(per),
		syms:       syms,
	}
	currentKeymap.haveMods = d.loadModMap(min, count)
	return nil
}

// loadModMap reads the modifier mapping to learn which modifier bits each
// keycode activates.
func (d *Display) loadModMap(first, count int) bool {
	mods := make([]C.ulong, count)
	if C.ly_mod_map(d.dpy, C.int(first), C.int(count), &mods[0]) == 0 {
		return false
	}
	out := make([]ModMask, count)
	for i, v := range mods {
		out[i] = ModMask(v)
	}
	currentKeymap.mods = out
	return true
}

// LookupKey translates a key event into the binding it corresponds to. Lock
// modifiers (Caps Lock, Num Lock) are ignored, so bindings do not need to
// mention them and work the same with them on or off.
func (d *Display) LookupKey(e KeyEvent) (Binding, bool) {
	km := &currentKeymap
	if !km.haveMods || km.perKeycode == 0 {
		return Binding{}, false
	}
	idx := int(e.KeyCode) - int(km.first)
	if idx < 0 || idx >= len(km.mods) {
		return Binding{}, false
	}
	state := ModMask(e.State) &^ (LockMask | Mod2Mask)

	// A keycode maps to one symbol per shift level: level 0 is unshifted,
	// level 1 requires Shift to be held.
	levels := km.perKeycode
	if levels > 2 {
		levels = 2
	}
	for level := 0; level < levels; level++ {
		sym := km.syms[idx*km.perKeycode+level]
		if sym == 0 {
			continue
		}
		shifted := level == 1
		if shifted != (state&ShiftMask != 0) {
			continue
		}
		return Binding{Mods: state &^ ShiftMask, Sym: sym}, true
	}
	return Binding{}, false
}

// KeycodeFor returns the keycode that produces the given key symbol.
func (d *Display) KeycodeFor(ks KeySym) (KeyCode, bool) {
	if d.dpy == nil {
		return 0, false
	}
	c := C.XKeysymToKeycode(d.dpy, C.KeySym(ks))
	if c == 0 {
		return 0, false
	}
	return KeyCode(c), true
}

// GrabKey binds a key combination on a window. ownerEvents is false so the
// window manager sees the press before any client does.
func (d *Display) GrabKey(b Binding, w Window) error {
	if d.dpy == nil {
		return errClosed
	}
	code, ok := d.KeycodeFor(b.Sym)
	if !ok {
		return fmt.Errorf("x11: key %s is not present in the current keymap", KeySymName(b.Sym))
	}
	C.XGrabKey(d.dpy, C.int(code), C.uint(b.Mods), C.Window(w), C.False,
		C.GrabModeAsync, C.GrabModeAsync)
	d.Flush()
	return nil
}

// UngrabKey releases a binding grabbed with GrabKey.
func (d *Display) UngrabKey(b Binding, w Window) {
	if d.dpy == nil {
		return
	}
	code, ok := d.KeycodeFor(b.Sym)
	if !ok {
		return
	}
	C.XUngrabKey(d.dpy, C.int(code), C.uint(b.Mods), C.Window(w))
	d.Flush()
}

// GrabButton binds a pointer button on a window.
func (d *Display) GrabButton(button, mods uint, w Window) error {
	if d.dpy == nil {
		return errClosed
	}
	C.XGrabButton(d.dpy, C.uint(button), C.uint(mods), C.Window(w), C.False,
		C.ButtonPressMask, C.GrabModeAsync, C.GrabModeAsync, C.None, C.None)
	d.Flush()
	return nil
}

// KeySymText returns the characters a key produces, which the search bar
// appends to its query. Navigation keys return an empty string.
func KeySymText(ks KeySym) string {
	name := KeySymName(ks)
	if name == "" || len(name) != 1 {
		return ""
	}
	if name == "space" {
		return " "
	}
	return name
}

// KeySymRune turns a key symbol into the character it stands for. It covers
// the Latin, Latin-1 and Cyrillic blocks plus the Unicode keysyms, which is
// what a keyboard produces when the layout is not US. It is what lets a
// widget receive real letters instead of key names.
func KeySymRune(ks KeySym) (rune, bool) {
	switch {
	case ks >= 0x01000000 && ks <= 0x0110FFFF:
		return rune(ks & 0x00FFFFFF), true
	case ks >= 0x0020 && ks <= 0x007E: // printable ASCII
		return rune(ks), true
	case ks >= 0x00A0 && ks <= 0x017F: // Latin-1 and Latin Extended-A
		return rune(ks), true
	case ks >= 0x0400 && ks <= 0x04FF: // Cyrillic
		return rune(ks), true
	case ks == 0xFF0D: // KP_Enter
		return '\n', true
	}
	return 0, false
}

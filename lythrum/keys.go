package main

/*
#include <X11/Xlib.h>
#include <X11/keysym.h>
#include <stdlib.h>
*/
import "C"

import (
	"sort"
	"strings"
	"unsafe"
)

// The modifier masks of the X server. The shell keeps its own copies so that
// the binding tables can be parsed and matched without crossing the cgo
// boundary for every key stroke.
const (
	maskShift   = 0x01
	maskLock    = 0x02
	maskControl = 0x04
	maskMod1    = 0x08 // Alt
	maskMod2    = 0x10 // NumLock
	maskMod3    = 0x20
	maskMod4    = 0x40 // Super
	maskMod5    = 0x80
)

// The keysyms of the modifier keys themselves.
const (
	ksShiftL   = 0xffe1
	ksShiftR   = 0xffe2
	ksControlL = 0xffe3
	ksControlR = 0xffe4
	ksAltL     = 0xffe7
	ksAltR     = 0xffe8
	ksSuperL   = 0xffeb
	ksSuperR   = 0xffec
	ksMetaL    = 0xffef
	ksMetaR    = 0xfff0
)

// The actions the shell knows how to perform. Every action can be bound to
// any number of key combinations.
const (
	actCenter   = "center"
	actTerminal = "terminal"
	actSettings = "settings"
	actWidgets  = "widgets"
	actFit      = "fit"
	actReload   = "reload"
	actRestart  = "restart"
	actZoom100  = "zoom100"
	actZoomIn   = "zoom_in"
	actZoomOut  = "zoom_out"
	actQuit     = "quit"
	actSwitch   = "switch"
	actClose    = "close"
	actDelete   = "delete"
	actRename   = "rename"
	actCopy     = "copy"
	actCut      = "cut"
	actPaste    = "paste"
	actPanLeft  = "pan_left"
	actPanRight = "pan_right"
	actPanUp    = "pan_up"
	actPanDown  = "pan_down"
	actPageUp   = "page_up"
	actPageDown = "page_down"
	actEscape   = "escape"
)

// combo is a normalised key combination: a modifier mask and a keysym.
type combo struct {
	mods uint32
	ks   uint32
}

// keyEntry is a combination and the action it triggers.
type keyEntry struct {
	combo  combo
	action string
}

// grabRec remembers a key that was grabbed on the root window, so the grab
// can be released again when the bindings change.
type grabRec struct {
	kc   uint32
	mods uint32
}

// defaultKeybinds is the keyboard of the shell out of the box: Super brings
// the board back to its centre, Super+R opens a terminal, Shift+Tab switches
// between the open windows and Alt+F4 closes the active one.
func defaultKeybinds() map[string][]string {
	return map[string][]string{
		actCenter:   {"Super", "r"},
		actTerminal: {"Super+R", "t"},
		actSettings: {"Super+S"},
		actWidgets:  {"Super+W"},
		actFit:      {"f"},
		actReload:   {"F5", "Shift+R"},
		actRestart:  {"Ctrl+R"},
		actZoom100:  {"0"},
		actZoomIn:   {"plus"},
		actZoomOut:  {"minus"},
		actQuit:     {"q"},
		actSwitch:   {"Shift+Tab"},
		actClose:    {"Alt+F4"},
		actDelete:   {"Delete"},
		actRename:   {"F2"},
		actCopy:     {"Ctrl+C"},
		actCut:      {"Ctrl+X"},
		actPaste:    {"Ctrl+V"},
		actPanLeft:  {"Left"},
		actPanRight: {"Right"},
		actPanUp:    {"Up"},
		actPanDown:  {"Down"},
		actPageUp:   {"Page_Up"},
		actPageDown: {"Page_Down"},
		actEscape:   {"Escape"},
	}
}

// modifierMask turns a modifier name into its mask.
func modifierMask(name string) (uint32, bool) {
	switch strings.ToLower(name) {
	case "ctrl", "control":
		return maskControl, true
	case "alt":
		return maskMod1, true
	case "shift":
		return maskShift, true
	case "super", "win", "windows", "meta", "cmd", "command":
		return maskMod4, true
	case "mod5":
		return maskMod5, true
	}
	return 0, false
}

// modifierOf returns the mask a modifier keysym stands for.
func modifierOf(ks uint32) uint32 {
	switch ks {
	case ksShiftL, ksShiftR:
		return maskShift
	case ksControlL, ksControlR:
		return maskControl
	case ksAltL, ksAltR:
		return maskMod1
	case ksSuperL, ksSuperR:
		return maskMod4
	case ksMetaL, ksMetaR:
		return maskMod5
	}
	return 0
}

// xStringToKeysym maps a key name such as "F5" or "Page_Up" to its keysym.
func xStringToKeysym(name string) uint32 {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return uint32(C.XStringToKeysym(cs))
}

// parseCombo reads a textual combination: "Super+R", "Alt+F4", "F5", "plus" or
// a lone modifier such as "Super".
func parseCombo(text string) (combo, bool) {
	text = strings.TrimSpace(text)
	switch text {
	case "":
		return combo{}, false
	case "+":
		text = "plus"
	case "-":
		text = "minus"
	}
	var mods uint32
	key := ""
	for _, part := range strings.Split(text, "+") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if m, ok := modifierMask(part); ok {
			mods |= m
			continue
		}
		key = part
	}
	if key == "" {
		// A combination made of modifiers only: the modifier is the key.
		switch {
		case mods&maskMod4 != 0:
			key = "Super_L"
		case mods&maskMod1 != 0:
			key = "Alt_L"
		case mods&maskControl != 0:
			key = "Control_L"
		case mods&maskShift != 0:
			key = "Shift_L"
		default:
			return combo{}, false
		}
		mods = 0
	}
	ks := xStringToKeysym(key)
	if ks == 0 {
		if alias, ok := keyAliases[strings.ToLower(key)]; ok {
			ks = xStringToKeysym(alias)
		}
	}
	if ks == 0 {
		return combo{}, false
	}
	return combo{mods: mods, ks: normalizeKey(ks)}, true
}

// formatCombo writes a combination back as text, the way the settings app
// shows it.
func formatCombo(c combo) string {
	var mods []string
	if c.mods&maskControl != 0 {
		mods = append(mods, "Ctrl")
	}
	if c.mods&maskMod1 != 0 {
		mods = append(mods, "Alt")
	}
	if c.mods&maskShift != 0 {
		mods = append(mods, "Shift")
	}
	if c.mods&maskMod4 != 0 {
		mods = append(mods, "Super")
	}
	name := C.XKeysymToString(C.KeySym(c.ks))
	key := "?"
	if name != nil {
		key = C.GoString(name)
	}
	if len(mods) == 0 {
		return key
	}
	return strings.Join(mods, "+") + "+" + key
}

// isLetter reports whether a keysym is a latin letter.
func isLetter(ks uint32) bool {
	return (ks >= 'a' && ks <= 'z') || (ks >= 'A' && ks <= 'Z')
}

// normalizeKey folds the case of the letters: a binding names the key, not
// the shift state, and the shift state travels in the modifier mask.
func normalizeKey(ks uint32) uint32 {
	if isLetter(ks) {
		return ks | 0x20
	}
	return ks
}

// keyAliases maps the names people write to the names X11 knows.
var keyAliases = map[string]string{
	"pageup":   "Page_Up",
	"pagedown": "Page_Down",
	"esc":      "Escape",
	"del":      "Delete",
	"ins":      "Insert",
	"win":      "Super_L",
	"super":    "Super_L",
	"cmd":      "Super_L",
	"space":    "space",
	"return":   "Return",
	"enter":    "Return",
}

// match reports whether a pressed key triggers the combination. Letters are
// matched regardless of the case, because the shift state is carried by the
// modifier mask; the punctuation and the function keys ignore the shift state
// altogether, since the same key produces them with or without shift.
func (c combo) match(ks uint32, mods uint32) bool {
	if m := modifierOf(ks); m != 0 {
		mods &^= m
	}
	if m := modifierOf(c.ks); m != 0 {
		c.mods &^= m
	}
	ks = normalizeKey(ks)
	if isLetter(c.ks) {
		return c.mods == mods && c.ks == ks
	}
	return c.mods&^maskShift == mods&^maskShift && c.ks == ks
}

// globalCombo reports whether a combination has to be grabbed on the root
// window: the Super and Alt combinations would otherwise be swallowed by the
// focused application.
func globalCombo(c combo) bool {
	if c.mods&(maskMod4|maskMod1) != 0 {
		return true
	}
	return c.ks == ksSuperL || c.ks == ksSuperR
}

// grabVariants lists the modifier masks a grab is installed for: the plain
// combination and the same one with the lock keys engaged.
func grabVariants(mods uint32) []uint32 {
	return []uint32{
		mods,
		mods | maskLock,
		mods | maskMod2,
		mods | maskLock | maskMod2,
	}
}

// rebuildKeyMap turns the binding table of the settings into a lookup table.
func (v *view) rebuildKeyMap() {
	v.keyList = nil
	v.keyMap = map[combo]string{}
	for action, list := range v.cfg.Keybinds {
		for _, text := range list {
			c, ok := parseCombo(text)
			if !ok {
				continue
			}
			v.keyList = append(v.keyList, keyEntry{combo: c, action: action})
			if _, dup := v.keyMap[c]; !dup {
				v.keyMap[c] = action
			}
		}
	}
	// A stable order keeps the fallback matching predictable.
	sort.Slice(v.keyList, func(i, j int) bool {
		if v.keyList[i].action != v.keyList[j].action {
			return v.keyList[i].action < v.keyList[j].action
		}
		if v.keyList[i].combo.ks != v.keyList[j].combo.ks {
			return v.keyList[i].combo.ks < v.keyList[j].combo.ks
		}
		return v.keyList[i].combo.mods < v.keyList[j].combo.mods
	})
}

// actionFor returns the action a pressed key triggers, or an empty string.
func (v *view) actionFor(ks uint32, mods uint32) string {
	ks = normalizeKey(ks)
	if act, ok := v.keyMap[combo{mods: mods, ks: ks}]; ok {
		return act
	}
	for _, e := range v.keyList {
		if e.combo.match(ks, mods) {
			return e.action
		}
	}
	return ""
}

// regrabKeys releases the global grabs of the previous bindings and installs
// the ones the current settings ask for.
func (v *view) regrabKeys() {
	xIgnoreBadAccess()
	root := C.XDefaultRootWindow(v.dpy)
	for _, g := range v.grabs {
		C.XUngrabKey(v.dpy, C.int(g.kc), C.uint(g.mods), root)
	}
	v.grabs = nil
	for _, e := range v.keyList {
		if !globalCombo(e.combo) {
			continue
		}
		kc := uint32(C.XKeysymToKeycode(v.dpy, C.KeySym(e.combo.ks)))
		if kc == 0 {
			continue
		}
		for _, m := range grabVariants(e.combo.mods) {
			C.XGrabKey(v.dpy, C.int(kc), C.uint(m), root,
				C.False, C.GrabModeAsync, C.GrabModeAsync)
			v.grabs = append(v.grabs, grabRec{kc: kc, mods: m})
		}
	}
	C.XFlush(v.dpy)
}

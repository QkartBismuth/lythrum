package x11

import (
	"os"
	"testing"
)

// TestKeyboardLookup checks that a key press is translated back into the
// binding that describes it. This is what every key binding depends on, and
// an empty modifier table here silently disables the whole keyboard layer.
func TestKeyboardLookup(t *testing.T) {
	if os.Getenv("LYTHRUM_X11_TEST") == "" {
		t.Skip("set LYTHRUM_X11_TEST=1 to run against a live display")
	}
	d, err := Open("")
	if err != nil {
		t.Skipf("no display: %v", err)
	}
	defer d.Close()

	if err := d.RefreshKeymap(); err != nil {
		t.Fatalf("RefreshKeymap: %v", err)
	}
	km := &currentKeymap
	if len(km.mods) == 0 {
		t.Fatal("the modifier table is empty; key lookups cannot work")
	}
	if km.perKeycode < 1 {
		t.Fatalf("keys per keycode = %d", km.perKeycode)
	}

	for _, key := range []struct {
		sym  KeySym
		mod  ModMask
		name string
	}{
		{0xff51, ModNone, "Left"},
		{0xff52, ModNone, "Up"},
		{0xff53, ModNone, "Right"},
		{0xff54, ModNone, "Down"},
		{0xff1b, ModNone, "Escape"},
		{0xff0d, ModNone, "Return"},
		{0xff55, ModNone, "Prior"},
		{0xff56, ModNone, "Next"},
		{KeySym('j'), Mod4Mask, "Mod4+j"},
		{KeySym('q'), ControlMask | Mod4Mask, "Ctrl+Mod4+q"},
	} {
		code, ok := d.KeycodeFor(key.sym)
		if !ok {
			t.Fatalf("%s is not in the keymap", key.name)
		}
		got, found := d.LookupKey(KeyEvent{
			header:  header{State: uint16(key.mod)},
			KeyCode: code,
		})
		if !found {
			t.Errorf("%s: the key press was not recognised", key.name)
			continue
		}
		if got.Sym != key.sym {
			t.Errorf("%s: keysym = %s", key.name, KeySymName(got.Sym))
		}
		if got.Mods&^LockMask != key.mod&^LockMask {
			t.Errorf("%s: modifiers = %s, want %s", key.name, got.Mods, key.mod)
		}
	}

	// Lock modifiers must not change the binding, otherwise Caps Lock and
	// Num Lock would break every binding.
	code, _ := d.KeycodeFor(KeySym('j'))
	plain, ok := d.LookupKey(KeyEvent{header: header{State: 0}, KeyCode: code})
	if !ok {
		t.Fatal("j not recognised")
	}
	locked, ok := d.LookupKey(KeyEvent{header: header{State: uint16(LockMask | Mod2Mask)}, KeyCode: code})
	if !ok {
		t.Fatal("j with locks not recognised")
	}
	if plain != locked {
		t.Errorf("lock modifiers changed the binding: %s vs %s", plain, locked)
	}
	// A real modifier still comes through.
	modded, ok := d.LookupKey(KeyEvent{header: header{State: uint16(LockMask | Mod4Mask)}, KeyCode: code})
	if !ok {
		t.Fatal("super+j not recognised")
	}
	if modded.Mods&Mod4Mask == 0 {
		t.Errorf("Mod4 was dropped: %s", modded)
	}
}

func TestParseBinding(t *testing.T) {
	cases := []struct {
		in    string
		mods  ModMask
		sym   string
		valid bool
	}{
		{"Mod4+Return", Mod4Mask, "Return", true},
		{"super+j", Mod4Mask, "j", true},
		{"Ctrl+Alt+Delete", ControlMask | Mod1Mask, "Delete", true},
		{"XF86AudioPlay", ModNone, "XF86AudioPlay", true},
		{"", ModNone, "", false},
		{"Mod4+", ModNone, "", false},
		{"Mod4+a+b", Mod4Mask, "a", false},
	}
	for _, c := range cases {
		b, err := ParseBinding(c.in)
		if c.valid != (err == nil) {
			t.Errorf("ParseBinding(%q) error = %v, want valid=%v", c.in, err, c.valid)
			continue
		}
		if !c.valid {
			continue
		}
		if b.Mods != c.mods {
			t.Errorf("ParseBinding(%q).Mods = %s, want %s", c.in, b.Mods, c.mods)
		}
		if got := KeySymName(b.Sym); got != c.sym {
			t.Errorf("ParseBinding(%q) keysym = %s, want %s", c.in, got, c.sym)
		}
	}
}

func TestParseModMask(t *testing.T) {
	cases := map[string]ModMask{
		"":            ModNone,
		"shift":       ShiftMask,
		"Shift+Ctrl":  ShiftMask | ControlMask,
		"Mod4+Mod1":   Mod4Mask | Mod1Mask,
		"super+alt":   Mod4Mask | Mod1Mask,
		"CONTROL":     ControlMask,
		"mod2 + mod3": Mod2Mask | Mod3Mask,
	}
	for in, want := range cases {
		got, err := ParseModMask(in)
		if err != nil {
			t.Errorf("ParseModMask(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseModMask(%q) = %s, want %s", in, got, want)
		}
	}
	if _, err := ParseModMask("nonsense"); err == nil {
		t.Error("an unknown modifier should be rejected")
	}
}

func TestBindingString(t *testing.T) {
	b := Binding{Mods: Mod4Mask | ShiftMask, Sym: KeySym('k')}
	if got := b.String(); got != "shift+super+k" {
		t.Errorf("Binding.String = %q", got)
	}
	back, err := ParseBinding(b.String())
	if err != nil || back.Mods != b.Mods || back.Sym != b.Sym {
		t.Errorf("round trip = %v %v", back, err)
	}
	if got := (ModMask(ModNone)).String(); got != "none" {
		t.Errorf("empty mask = %q", got)
	}
}

// TestKeySymRune checks that a key symbol becomes the character of the
// current layout, which is what a widget receives instead of a Latin key name.
func TestKeySymRune(t *testing.T) {
	cases := []struct {
		name string
		sym  KeySym
		want rune
	}{
		{"latin", KeySym('a'), 'a'},
		{"digit", KeySym('7'), '7'},
		{"cyrillic", KeySym(0x0421), 'С'},
		{"latin1", KeySym(0x00E9), 'é'},
		{"unicode keysym", KeySym(0x01000000 | 0x1F600), 0x1F600},
	}
	for _, c := range cases {
		got, ok := KeySymRune(c.sym)
		if !ok {
			t.Errorf("KeySymRune(%s) reported no character", c.name)
			continue
		}
		if got != c.want {
			t.Errorf("KeySymRune(%s) = %q, want %q", c.name, got, c.want)
		}
	}
	for _, sym := range []KeySym{0xFF70 /* F1 */, 0xFF50 /* KP_Home */, KeySym(0)} {
		if r, ok := KeySymRune(sym); ok && r != 0 {
			t.Errorf("KeySymRune(0x%x) = %q, want no character", sym, r)
		}
	}
}

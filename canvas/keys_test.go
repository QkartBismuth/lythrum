package main

import "testing"

func TestParseCombo(t *testing.T) {
	cases := []struct {
		text string
		mods uint32
		ks   uint32
	}{
		{"Super", 0, ksSuperL},
		{"Super+R", maskMod4, 'r'},
		{"super+r", maskMod4, 'r'},
		{"Alt+F4", maskMod1, 0xffc1},
		{"Ctrl+C", maskControl, 'c'},
		{"F5", 0, 0xffc2},
		{"plus", 0, '+'},
		{"+", 0, '+'},
		{"-", 0, '-'},
		{"Shift+Tab", maskShift, 0xff09},
		{"Page_Up", 0, 0xff55},
		{"PageUp", 0, 0xff55},
	}
	for _, c := range cases {
		got, ok := parseCombo(c.text)
		if !ok {
			t.Fatalf("parseCombo(%q) failed", c.text)
		}
		if got.mods != c.mods || got.ks != c.ks {
			t.Errorf("parseCombo(%q) = {mods %#x, ks %#x}, want {mods %#x, ks %#x}",
				c.text, got.mods, got.ks, c.mods, c.ks)
		}
	}
}

func TestActionMatching(t *testing.T) {
	v := &view{cfg: defaultSettings()}
	v.rebuildKeyMap()

	if act := v.actionFor('r', 0); act != actCenter {
		t.Errorf("'r' should recentre, got %q", act)
	}
	if act := v.actionFor('R', maskShift); act != actReload {
		t.Errorf("Shift+R should reload, got %q", act)
	}
	if act := v.actionFor('R', 0); act != actCenter {
		t.Errorf("plain R should recentre, got %q", act)
	}
	if act := v.actionFor('r', maskControl); act != actRestart {
		t.Errorf("Ctrl+R should restart, got %q", act)
	}
	if act := v.actionFor('c', maskControl); act != actCopy {
		t.Errorf("Ctrl+C should copy, got %q", act)
	}
	if act := v.actionFor('R', maskMod4); act != actTerminal {
		t.Errorf("Super+R should open a terminal, got %q", act)
	}
	if act := v.actionFor(0xff09, maskShift); act != actSwitch {
		t.Errorf("Shift+Tab should switch windows, got %q", act)
	}
	if act := v.actionFor(0xffc1, maskMod1); act != actClose {
		t.Errorf("Alt+F4 should close the window, got %q", act)
	}
	if act := v.actionFor(ksSuperL, 0); act != actCenter {
		t.Errorf("Super alone should recentre, got %q", act)
	}
	// A shifted letter is a key of its own: it must not trigger the plain
	// binding.
	if act := v.actionFor('F', maskShift); act != "" {
		t.Errorf("Shift+F should do nothing, got %q", act)
	}
	// The plus key produces a shifted keysym on most layouts.
	if act := v.actionFor('+', maskShift); act != actZoomIn {
		t.Errorf("Shift++ should zoom in, got %q", act)
	}
	if act := v.actionFor('z', 0); act != "" {
		t.Errorf("unbound key should do nothing, got %q", act)
	}
}

func TestGlobalCombos(t *testing.T) {
	super, _ := parseCombo("Super")
	if !globalCombo(super) {
		t.Error("Super alone must be grabbed globally")
	}
	alt, _ := parseCombo("Alt+F4")
	if !globalCombo(alt) {
		t.Error("Alt+F4 must be grabbed globally")
	}
	f5, _ := parseCombo("F5")
	if globalCombo(f5) {
		t.Error("F5 must not be grabbed globally")
	}
}

package x11

import (
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// liveDisplay reports whether the test should talk to a real X server. These
// checks are skipped by default so the unit test suite stays hermetic.
func liveDisplay(t *testing.T) *Display {
	t.Helper()
	if os.Getenv("LYTHRUM_X11_TEST") == "" {
		t.Skip("set LYTHRUM_X11_TEST=1 to run against a live display")
	}
	d, err := Open("")
	if err != nil {
		t.Skipf("cannot open the display: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// TestPropertyRoundTrip writes a property and reads it back.
func TestPropertyRoundTrip(t *testing.T) {
	d := liveDisplay(t)
	atom := d.Atom("LYTHRUM_ROUNDTRIP")
	want := []uint32{0x01020304, 0xaabbccdd, 0x11223344}

	if err := d.SetCardinals(d.Root(), atom, A.Cardinal, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	d.Sync()

	p, err := d.GetProperty(d.Root(), atom, 0, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if p == nil {
		t.Fatal("the property disappeared right after it was written")
	}
	if p.Format != 32 {
		t.Fatalf("format = %d, want 32", p.Format)
	}
	got := p.Cardinal32()
	if len(got) != len(want) {
		t.Fatalf("read %d items, want %d (%s)", len(got), len(want), hex.EncodeToString(p.Data))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d = 0x%08x, want 0x%08x", i, got[i], want[i])
		}
	}
}

// TestPropertyTextRoundTrip checks format-8 properties, which is how window
// titles and classes travel.
func TestPropertyTextRoundTrip(t *testing.T) {
	d := liveDisplay(t)
	atom := d.Atom("LYTHRUM_TEXT")
	const text = "Lythrum round trip"

	if err := d.SetProperty(d.Root(), atom, A.String, 8, []byte(text)); err != nil {
		t.Fatalf("write: %v", err)
	}
	d.Sync()

	p, err := d.GetProperty(d.Root(), atom, 0, 0)
	if err != nil || p == nil {
		t.Fatalf("read: %v", err)
	}
	if got := string(p.Data); got != text {
		t.Errorf("read %q, want %q", got, text)
	}
}

// TestPropertyMatchesExternalReader cross-checks the values against xprop,
// which uses libX11 independently of this package.
func TestPropertyMatchesExternalReader(t *testing.T) {
	d := liveDisplay(t)
	xpropBin, err := exec.LookPath("xprop")
	if err != nil {
		t.Skip("xprop is not installed")
	}
	atom := d.Atom("LYTHRUM_XPROP_CHECK")
	want := []uint32{0x01020304, 0xaabbccdd, 0x11223344}
	if err := d.SetCardinals(d.Root(), atom, A.Cardinal, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	d.Sync()

	out, err := exec.Command(xpropBin, "-root", "LYTHRUM_XPROP_CHECK").Output()
	if err != nil {
		t.Fatalf("xprop: %v", err)
	}
	got := strings.TrimSpace(string(out))
	t.Logf("xprop reports: %s", got)
	for _, v := range want {
		if !strings.Contains(got, strings.ToLower(strconv32(v))) {
			t.Errorf("xprop does not report %d: %s", v, got)
		}
	}
}

func strconv32(v uint32) string {
	const digits = "0123456789"
	buf := make([]byte, 0, 10)
	if v == 0 {
		return "0"
	}
	for v > 0 {
		buf = append([]byte{digits[v%10]}, buf...)
		v /= 10
	}
	return string(buf)
}

package wm

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"lythrum/internal/layout"
)

func TestPowerCommandsFallBackInOrder(t *testing.T) {
	cases := map[string][][]string{
		"suspend":   {{"systemctl", "suspend"}, {"loginctl", "suspend"}},
		"hibernate": {{"systemctl", "hibernate"}, {"loginctl", "hibernate"}},
		"reboot":    {{"systemctl", "reboot"}, {"loginctl", "reboot"}},
		"poweroff":  {{"systemctl", "poweroff"}, {"loginctl", "poweroff"}},
	}
	for id, want := range cases {
		got := powerCommands(id)
		if len(got) != len(want) {
			t.Fatalf("powerCommands(%q) = %v, want %v", id, got, want)
		}
		for i := range want {
			if got[i][0] != want[i][0] || got[i][1] != want[i][1] {
				t.Errorf("powerCommands(%q)[%d] = %v, want %v", id, i, got[i], want[i])
			}
		}
	}
	if got := powerCommands("nonsense"); got != nil {
		t.Errorf("powerCommands(\"nonsense\") = %v, want nothing", got)
	}
}

func TestPowerCommandsLogoutUsesTheSession(t *testing.T) {
	t.Setenv("XDG_SESSION_ID", "42")
	got := powerCommands("logout")
	if len(got) != 1 {
		t.Fatalf("logout commands = %v, want one entry", got)
	}
	want := []string{"loginctl", "terminate-session", "42"}
	for i := range want {
		if got[0][i] != want[i] {
			t.Fatalf("logout command = %v, want %v", got[0], want)
		}
	}
	t.Setenv("XDG_SESSION_ID", "")
	if got := powerCommands("logout"); len(got) != 0 {
		t.Errorf("logout without a session = %v, want nothing to run", got)
	}
}

func TestPowerModeAvailableReadsTheKernel(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.WriteFile(state, []byte("freeze mem disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := powerStatePath
	powerStatePath = state
	defer func() { powerStatePath = old }()

	if !powerModeAvailable("mem") || !powerModeAvailable("disk") {
		t.Error("mem and disk should be reported as available")
	}
	if powerModeAvailable("s2disk") {
		t.Error("s2disk is not in the kernel list and must not be reported")
	}
	if err := os.WriteFile(state, []byte("freeze\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if powerModeAvailable("mem") {
		t.Error("a machine without mem in the state file cannot suspend")
	}
}

func TestPowerAvailableMarksMissingModes(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.WriteFile(state, []byte("freeze mem\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := powerStatePath
	powerStatePath = state
	defer func() { powerStatePath = old }()

	if ok, _ := powerAvailable("suspend", "сон"); !ok {
		t.Error("suspend should be available when the kernel lists mem")
	}
	ok, hint := powerAvailable("hibernate", "гибернация")
	if ok {
		t.Error("hibernate needs disk, which this machine does not list")
	}
	if hint == "гибернация" {
		t.Errorf("unavailable rows should explain themselves, got %q", hint)
	}
}

func TestShellActionsCoverTheShell(t *testing.T) {
	actions := shellActions()
	if len(actions) == 0 {
		t.Fatal("the settings page needs a list of actions")
	}
	seen := map[string]bool{}
	for _, a := range actions {
		if a.ID == "" || a.Title == "" || a.Group == "" {
			t.Fatalf("incomplete action %+v", a)
		}
		if seen[a.ID] {
			t.Errorf("action %q is listed twice", a.ID)
		}
		seen[a.ID] = true
	}
	for _, want := range []string{"start_screen", "search", "power", "quit", "tile", "desktop_1"} {
		if !seen[want] {
			t.Errorf("action %q is missing from the list the settings page shows", want)
		}
	}
}

func TestItoaMatchesTheStandard(t *testing.T) {
	for _, n := range []int{0, 1, 9, 10, 32, 99, 12345} {
		if got, want := itoa(n), strconv.Itoa(n); got != want {
			t.Errorf("itoa(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestPowerRowHitTesting(t *testing.T) {
	s := &powerScreen{rects: []layout.Rect{
		{X: 100, Y: 200, W: 400, H: 48},
		{X: 100, Y: 256, W: 400, H: 48},
	}}
	cases := []struct {
		x, y int
		want int
	}{
		{100, 200, 0},  // top left corner belongs to the row
		{499, 247, 0},  // bottom right corner belongs to the row
		{500, 248, -1}, // one pixel outside misses
		{300, 300, 1},  // the second row
		{99, 220, -1},  // left of the rows
		{300, 199, -1}, // above the rows
	}
	for _, c := range cases {
		if got := s.rowAt(c.x, c.y); got != c.want {
			t.Errorf("rowAt(%d,%d) = %d, want %d", c.x, c.y, got, c.want)
		}
	}
}

func TestPowerLayoutKeepsRowsInsideTheCard(t *testing.T) {
	w := &WM{}
	s := newPowerScreen(w)
	s.area = layout.Rect{X: 0, Y: 40, W: 1280, H: 760}
	s.rows = []powerRow{{id: "logout"}, {id: "suspend"}, {id: "hibernate"},
		{id: "reboot"}, {id: "poweroff"}}
	card, rows := s.layoutRects()
	if card.X < s.area.X || card.Y < s.area.Y ||
		card.X+card.W > s.area.X+s.area.W || card.Y+card.H > s.area.Y+s.area.H {
		t.Errorf("card %+v leaves the work area %+v", card, s.area)
	}
	if len(rows) != len(s.rows) {
		t.Fatalf("%d rows for %d entries", len(rows), len(s.rows))
	}
	for i, r := range rows {
		if r.H <= 0 || r.W <= 0 {
			t.Errorf("row %d has no size: %+v", i, r)
		}
		if r.X < card.X || r.X+r.W > card.X+card.W {
			t.Errorf("row %d %+v sticks out of the card %+v", i, r, card)
		}
		if r.Y < card.Y || r.Y+r.H > card.Y+card.H {
			t.Errorf("row %d %+v sticks out of the card %+v", i, r, card)
		}
	}
}

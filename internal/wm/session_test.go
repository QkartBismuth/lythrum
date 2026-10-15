package wm

import "testing"

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestSessionEnvDetectsWayland(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"explicit wayland", map[string]string{
			"XDG_SESSION_TYPE": "wayland", "WAYLAND_DISPLAY": "wayland-0",
		}, true},
		{"explicit x11", map[string]string{
			"XDG_SESSION_TYPE": "x11",
		}, false},
		{"upper case", map[string]string{"XDG_SESSION_TYPE": "Wayland"}, true},
		{"only a wayland socket", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, true},
		{"nothing at all", map[string]string{}, false},
	}
	for _, c := range cases {
		got := SessionEnv(envOf(c.env)).Wayland
		if got != c.want {
			t.Errorf("%s: Wayland = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSessionKeepsTheDesktopName(t *testing.T) {
	s := SessionEnv(envOf(map[string]string{
		"XDG_CURRENT_DESKTOP": " KDE ",
		"XDG_SESSION_TYPE":    "wayland",
		"DISPLAY":             ":1",
		"WAYLAND_DISPLAY":     "wayland-0",
	}))
	if s.Desktop != "KDE" {
		t.Errorf("desktop = %q, want KDE", s.Desktop)
	}
	if s.XWaylandDisplay != ":1" {
		t.Errorf("display = %q, want :1", s.XWaylandDisplay)
	}
	if !s.Wayland {
		t.Error("a wayland session type must set the flag")
	}
	if got := s.Summary(); got == "" || got == s.String() {
		t.Errorf("summary %q should say what the shell can manage", got)
	}
}

func TestSessionSummaryX11(t *testing.T) {
	s := SessionEnv(envOf(map[string]string{"XDG_SESSION_TYPE": "x11"}))
	if s.Wayland {
		t.Fatal("an x11 session must not report wayland")
	}
	if got := s.Summary(); got != "X11-сессия: оболочка управляет всеми окнами" {
		t.Errorf("summary = %q", got)
	}
	if got := s.String(); got != "X11 (unknown)" {
		t.Errorf("String = %q, want a readable line", got)
	}
}

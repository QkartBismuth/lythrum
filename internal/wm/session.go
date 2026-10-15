package wm

import (
	"os"
	"strings"
)

// Session describes where the shell is running. Lythrum speaks X11 only, so a
// Wayland desktop means the shell lives inside XWayland and sees the X11
// clients of that session and nothing else.
type Session struct {
	// Wayland is true when a Wayland compositor is hosting the session.
	Wayland bool
	// XWaylandDisplay is the DISPLAY XWayland offered, if any.
	XWaylandDisplay string
	// Desktop is the name the session reports, used in the log and in the
	// settings page.
	Desktop string
	// WaylandDisplay is the compositor socket, useful in a bug report.
	WaylandDisplay string
}

// String describes the session for a log line.
func (s Session) String() string {
	if !s.Wayland {
		return "X11 (" + orUnknown(s.Desktop) + ")"
	}
	return "XWayland (" + orUnknown(s.Desktop) + "), wayland=" + orUnknown(s.WaylandDisplay)
}

// Summary is the one line a user cares about: what this shell can manage.
func (s Session) Summary() string {
	if s.Wayland {
		return "Wayland-сессия: оболочка работает через XWayland и управляет только X11-окнами"
	}
	return "X11-сессия: оболочка управляет всеми окнами"
}

// SessionEnv reads the environment the session hands down. It is a function so
// the tests can describe an environment without touching the real one.
func SessionEnv(getenv func(string) string) Session {
	env := getenv
	if env == nil {
		env = os.Getenv
	}
	session := Session{
		Desktop:        firstNonEmpty(env("XDG_CURRENT_DESKTOP"), env("DESKTOP_SESSION")),
		WaylandDisplay: env("WAYLAND_DISPLAY"),
	}
	switch strings.ToLower(strings.TrimSpace(env("XDG_SESSION_TYPE"))) {
	case "wayland":
		session.Wayland = true
	case "x11":
		session.Wayland = false
	default:
		// Not every session sets XDG_SESSION_TYPE. A Wayland display socket is
		// a reliable hint, and XWayland is the only way we could be talking.
		session.Wayland = session.WaylandDisplay != ""
	}
	session.XWaylandDisplay = env("DISPLAY")
	return session
}

// DetectSession reports the session the shell is running in.
func DetectSession() Session { return SessionEnv(nil) }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func orUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

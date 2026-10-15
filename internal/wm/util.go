package wm

import (
	"fmt"
	"os"
	"syscall"

	"lythrum/internal/layout"
	"lythrum/internal/x11"
)

// colorCache avoids resolving the same colour string on every repaint.
var colorCache = map[string]uint32{}

// color resolves a "#rrggbb" colour string to a pixel value.
func (w *WM) color(spec string) uint32 {
	if spec == "" {
		return 0
	}
	if px, ok := colorCache[spec]; ok {
		return px
	}
	r, g, b, err := x11.HexColor(spec)
	if err != nil {
		w.log.Warn("bad colour", "value", spec)
		return 0
	}
	px := w.d.PixelFromRGB(r, g, b)
	colorCache[spec] = px
	return px
}

// killProcess sends a signal to the process that owns a client window. It is
// the last resort when a client ignores WM_DELETE_WINDOW.
func killProcess(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("wm: invalid pid %d", pid)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(sig)
}

// abs is the distance from zero.
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// rootPoint turns a coordinate inside one of the shell's own windows into a
// root coordinate. Adding the window origin is the whole job: mixing a root
// coordinate into the same arithmetic lands the click in a different row, which
// is how a power menu row became impossible to press.
func rootPoint(area layout.Rect, x, y int) (int, int) {
	return x + area.X, y + area.Y
}

package wm

import (
	"fmt"
	"os"
	"syscall"

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

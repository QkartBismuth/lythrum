package layout

import (
	"fmt"
	"strings"
)

// StackKind selects the arrangement of the managed windows.
type StackKind string

// Parse maps a configured layout name to its kind.
func Parse(name string) (StackKind, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "stack":
		return LayoutStack, nil
	case "monocle":
		return LayoutMonocle, nil
	case "tiled":
		return LayoutTiled, nil
	case "float", "floating", "floating-stack":
		return LayoutFloat, nil
	}
	return LayoutStack, fmt.Errorf("unknown layout %q", name)
}

// Available layouts. LayoutStack is the bootstrap arrangement: every window
// gets the whole work area and the focused one is on top.
const (
	LayoutStack   StackKind = "stack"
	LayoutMonocle StackKind = "monocle"
	LayoutTiled   StackKind = "tiled"
	LayoutFloat   StackKind = "float"
)

// Options tune the geometry produced by the layouts.
type Options struct {
	// Gap is the space between neighbouring windows.
	Gap int
	// Border is the space between the window and the edge of its area.
	Border int
	// TitleBar is the height reserved at the top of every window area.
	TitleBar int
}

// DefaultOptions are used when the configuration leaves a value unset.
func DefaultOptions() Options {
	return Options{Gap: 0, Border: 0, TitleBar: 0}
}

// Area returns the area a single window of the given size occupies inside work.
func (o Options) Area(work Rect) Rect {
	return work.AsymmetricInset(o.Border, o.Border, o.Border+o.TitleBar, o.Border)
}

// Stack places every window in the full work area. The window manager raises
// the focused one, so the arrangement stays visually meaningful.
func Stack(work Rect) []Rect {
	return []Rect{work}
}

// Monocle gives every window the whole work area, like a fullscreen stack.
func Monocle(work Rect) []Rect {
	return []Rect{work}
}

// Tiled splits the work area into n equal parts. It is the first real tiling
// layout and doubles as the geometry engine for the tree layouts to come.
func Tiled(work Rect, n int, o Options) []Rect {
	if n <= 0 {
		return nil
	}
	dir := Horizontal
	if work.H > work.W {
		dir = Vertical
	}
	parts := work.Divide(dir, n, o.Gap)
	out := make([]Rect, 0, len(parts))
	for _, p := range parts {
		out = append(out, o.Area(p))
	}
	return out
}

package layout

// Desktop icons: the entries of the desktop directory drawn over the
// wallpaper. The geometry lives here, without X, so the placement can be
// checked by an ordinary test.

// DesktopIconOptions tunes the icon grid of the desktop.
type DesktopIconOptions struct {
	// Cell is the width and height of one icon slot, caption included.
	Cell int
	// Icon is the edge of the icon image itself.
	Icon int
	// Gap separates the slots.
	Gap int
	// Left and Top are the margins from the monitor edges.
	Left, Top int
}

// DefaultDesktopIconOptions returns the built-in icon grid.
func DefaultDesktopIconOptions() DesktopIconOptions {
	return DesktopIconOptions{Cell: 128, Icon: 72, Gap: 16, Left: 28, Top: 28}
}

// DesktopIcons places count icon slots inside area, filling the first column
// from top to bottom and moving on to the next one when the screen runs out of
// rows. The rectangles are in the coordinates of area.
func DesktopIcons(area Rect, count int, o DesktopIconOptions) []Rect {
	if count <= 0 {
		return nil
	}
	cell := o.Cell
	if cell < 32 {
		cell = 32
	}
	gap := o.Gap
	if gap < 0 {
		gap = 0
	}
	// The right margin mirrors the left one, so the last column keeps the same
	// breathing room as the first.
	right := area.W - o.Left
	if right < cell {
		right = cell
	}
	rows := (area.H - 2*o.Top + gap) / (cell + gap)
	if rows < 1 {
		rows = 1
	}
	out := make([]Rect, 0, count)
	for i := 0; i < count; i++ {
		row, col := i%rows, i/rows
		x := o.Left + col*(cell+gap)
		if x+cell > right {
			break // the next column would fall off the screen
		}
		out = append(out, Rect{X: area.X + x, Y: area.Y + o.Top + row*(cell+gap), W: cell, H: cell})
	}
	return out
}

// IconHit returns the index of the slot under a point, or -1.
func IconHit(rects []Rect, x, y int) int {
	for i, r := range rects {
		if r.Contains(x, y) {
			return i
		}
	}
	return -1
}

package layout

import "math"

// GridOptions tunes the start-screen tile grid.
type GridOptions struct {
	// Gap is the space between tiles, Border the space around the grid.
	Gap    int
	Border int
	// Aspect is the preferred tile width divided by its height. Windows 8
	// used wide tiles; 1 gives square ones.
	Aspect float64
	// Columns pins the number of columns when greater than zero.
	Columns int
	// MaxCell caps the tile edge so a wide monitor does not end up with a
	// single row of enormous tiles; the grid simply gains columns.
	MaxCell int
	// MinCell is the smallest acceptable tile edge; the column count is
	// reduced when tiles would end up smaller.
	MinCell int
}

// DefaultGridOptions returns sensible start-screen defaults.
func DefaultGridOptions() GridOptions {
	return GridOptions{Gap: 8, Border: 24, Aspect: 1.0, MaxCell: 180, MinCell: 72}
}

// Grid is the computed tile arrangement for a work area.
type Grid struct {
	Area     Rect
	Rows     int
	Columns  int
	PageSize int
	Pages    int
	Cells    []Rect // one full page of cells
}

// ComputeGrid lays out tiles inside area. The tile grid has a fixed page
// size: the column count follows the requested aspect and the number of rows
// is whatever fits on screen, so extra items spill onto further pages instead
// of squeezing the tiles.
func ComputeGrid(area Rect, count int, o GridOptions) Grid {
	if count < 1 {
		count = 1
	}
	inner := area.AsymmetricInset(o.Border, o.Border, o.Border, o.Border)
	if inner.W < 1 || inner.H < 1 {
		return Grid{
			Area:     area,
			Rows:     1,
			Columns:  1,
			PageSize: 1,
			Pages:    1,
			Cells:    []Rect{{X: area.X, Y: area.Y, W: 1, H: 1}},
		}
	}
	aspect := o.Aspect
	if aspect <= 0 {
		aspect = 1
	}

	cols := o.Columns
	if cols <= 0 {
		cols = int(math.Round(math.Sqrt(float64(count) * aspect * float64(inner.W) / float64(inner.H))))
		cols = clamp(cols, 1, count)
	}
	// Drop columns while tiles get too small to read.
	for cols > 1 && o.MinCell > 0 {
		cellW := (inner.W - o.Gap*(cols-1)) / cols
		if cellW >= o.MinCell {
			break
		}
		cols--
	}

	cellW := (inner.W - o.Gap*(cols-1)) / cols
	// A tile is never smaller than MinCell and, unless that would break the
	// minimum, never larger than MaxCell.
	switch {
	case o.MinCell > 0 && cellW < o.MinCell:
		cellW = o.MinCell
	case o.MaxCell > 0 && (o.MinCell <= 0 || o.MaxCell >= o.MinCell) && cellW > o.MaxCell:
		cellW = o.MaxCell
	}
	if cellW < 1 {
		cellW = 1
	}
	cellH := int(float64(cellW) / aspect)
	if cellH < 1 {
		cellH = 1
	}
	if cellH > inner.H {
		cellH = inner.H
	}
	rows := (inner.H + o.Gap) / (cellH + o.Gap)
	if rows < 1 {
		rows = 1
	}

	cells := make([]Rect, 0, cols*rows)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			cells = append(cells, Rect{
				X: inner.X + c*(cellW+o.Gap),
				Y: inner.Y + r*(cellH+o.Gap),
				W: cellW,
				H: cellH,
			})
		}
	}

	pages := (count + cols*rows - 1) / (cols * rows)
	if pages < 1 {
		pages = 1
	}
	return Grid{
		Area:     area,
		Rows:     rows,
		Columns:  cols,
		PageSize: cols * rows,
		Pages:    pages,
		Cells:    cells,
	}
}

// Page returns the cells of one page together with the entries that belong to
// it. Entries beyond the end of the grid are dropped.
func (g Grid) Page(page, count int) []Rect {
	if page < 0 {
		page = 0
	}
	start := page * g.PageSize
	if start >= count || len(g.Cells) == 0 {
		return nil
	}
	end := start + g.PageSize
	if end > count {
		end = count
	}
	n := end - start
	if n > len(g.Cells) {
		n = len(g.Cells)
	}
	out := make([]Rect, n)
	copy(out, g.Cells[:n])
	return out
}

// Hit returns the index of the cell containing a point, or -1.
func (g Grid) Hit(cells []Rect, x, y int) int {
	for i, c := range cells {
		if c.Contains(x, y) {
			return i
		}
	}
	return -1
}

// Next wraps an index by delta, staying inside the current page.
func Next(index, delta, length int) int {
	if length <= 0 {
		return -1
	}
	idx := index + delta
	for idx < 0 {
		idx += length
	}
	for idx >= length {
		idx -= length
	}
	return idx
}

// PageFor returns the page an absolute item index belongs to.
func (g Grid) PageFor(index int) int {
	if g.PageSize <= 0 {
		return 0
	}
	return index / g.PageSize
}

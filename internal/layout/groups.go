package layout

// Groups and large tiles, in the spirit of the Windows 8.1 start screen:
// entries are bucketed into labelled groups, each group is a band with a
// header and a strip of tiles, and the first tile of a group is drawn large
// (two columns by two rows) so the screen has a focal point.

// GroupSpec describes one band of the start screen.
type GroupSpec struct {
	// Name is the header shown above the group.
	Name string
	// First is the absolute index of the first entry of the group.
	First int
	// Count is how many entries the group holds.
	Count int
}

// Tile is one entry inside a group.
type Tile struct {
	// Index is the absolute index of the entry in the flat list.
	Index int
	// Rect is where the tile is drawn, relative to the grid area.
	Rect Rect
	// Row and Col place the tile in the grid; a large tile spans two of both.
	Row, Col int
	// Large marks the group's first tile.
	Large bool
	// SpanX and SpanY are the tile size in grid cells.
	SpanX, SpanY int
}

// Band is one group laid out on screen.
type Band struct {
	Name   string
	Header Rect
	Tiles  []Tile
	Height int
}

// GroupOptions tunes the grouped start screen.
type GroupOptions struct {
	// Gap is the space between tiles and bands.
	Gap int
	// Border is the space around the whole grid.
	Border int
	// Cell is the preferred tile edge; the column count follows from it.
	Cell int
	// HeaderHeight is the vertical space a group header occupies.
	HeaderHeight int
	// BandGap is the extra space below a group.
	BandGap int
	// Columns pins the column count when greater than zero.
	Columns int
	// Large enables the double-by-double first tile of every group.
	Large bool
	// LargeMin is how many entries a group needs before its first tile is
	// drawn large; a group with a single entry would only waste space.
	LargeMin int
}

// DefaultGroupOptions returns the defaults used by the start screen.
func DefaultGroupOptions() GroupOptions {
	return GroupOptions{
		Gap:          8,
		Border:       24,
		Cell:         180,
		HeaderHeight: 34,
		BandGap:      28,
		Large:        true,
		LargeMin:     3,
	}
}

// GroupGrid is the whole start screen: the bands of every group and how they
// are split into pages.
type GroupGrid struct {
	Area    Rect
	Columns int
	Pages   int
	// Bands holds every band in order; a page is a slice of it.
	Bands []Band
	// Pages splits Bands into consecutive runs that each fit on screen.
	Pages_ [][]Band
}

// Page returns the bands of one page.
func (g GroupGrid) Page(page int) []Band {
	if page < 0 || page >= len(g.Pages_) {
		return nil
	}
	return g.Pages_[page]
}

// PageCount returns how many pages the grid has.
func (g GroupGrid) PageCount() int { return len(g.Pages_) }

// Tiles flattens the tiles of one page in reading order.
func (g GroupGrid) Tiles(page int) []Tile {
	bands := g.Page(page)
	out := make([]Tile, 0, len(bands)*4)
	for _, b := range bands {
		out = append(out, b.Tiles...)
	}
	return out
}

// ComputeGroups lays the groups out and flows their tiles across as many
// pages as the area needs. A group that does not fit continues on the next
// page with its heading repeated, which is how the Windows 8.1 start screen
// scrolled.
func ComputeGroups(area Rect, groups []GroupSpec, o GroupOptions) GroupGrid {
	inner := area.AsymmetricInset(o.Border, o.Border, o.Border, o.Border)
	g := GroupGrid{Area: area, Columns: 1}
	if inner.W < 1 || inner.H < 1 || len(groups) == 0 {
		g.Pages_ = [][]Band{{}}
		g.Pages = 1
		return g
	}

	cell := o.Cell
	if cell < 32 {
		cell = 32
	}
	cols := o.Columns
	if cols <= 0 {
		// Fit as many preferred-size tiles as the width allows.
		cols = (inner.W + o.Gap) / (cell + o.Gap)
		if cols < 2 {
			cols = 2
		}
	}
	if widest := (inner.W - o.Gap*(cols-1)) / cols; cell > widest && widest > 0 {
		cell = widest
	}
	g.Columns = cols

	limit := inner.Y + inner.H
	var page []Band
	var cur *Band
	pageHasContent := false
	y := inner.Y

	closeBand := func() {
		if cur == nil {
			return
		}
		bottom := cur.Header.Y
		for _, t := range cur.Tiles {
			if t.Rect.Bottom() > bottom {
				bottom = t.Rect.Bottom()
			}
		}
		cur.Height = bottom - cur.Header.Y + o.BandGap
		g.Bands = append(g.Bands, *cur)
		page = append(page, *cur)
		cur = nil
	}
	closePage := func() {
		closeBand()
		g.Pages_ = append(g.Pages_, page)
		page = nil
		pageHasContent = false
		y = inner.Y
	}

	for _, spec := range groups {
		if spec.Count <= 0 {
			continue
		}
		rows := layoutRows(spec, cols, cell, o)

		row := 0
		for _, rowTiles := range rows {
			rowHeight := 0
			for _, tile := range rowTiles {
				if tile.SpanY == 2 {
					rowHeight = 2*cell + o.Gap
					break
				}
			}
			if rowHeight == 0 {
				rowHeight = cell
			}

			headerSpace := 0
			if cur == nil {
				headerSpace = o.HeaderHeight
			}
			if y+headerSpace+rowHeight > limit {
				if pageHasContent {
					closePage()
					y = inner.Y
				}
				headerSpace = o.HeaderHeight
				if y+headerSpace+rowHeight > limit {
					// Even a fresh page cannot hold the row, so show it cut
					// off at the bottom instead of outside the screen.
					rowHeight = max(limit-y-headerSpace, cell/2)
				}
			}
			if cur == nil {
				cur = &Band{Name: spec.Name}
				cur.Header = Rect{X: inner.X, Y: y, W: 0, H: o.HeaderHeight}
				row = 0
			}
			for _, tile := range rowTiles {
				tile.Row = row
				tile.Rect = Rect{
					X: inner.X + tile.Col*(cell+o.Gap),
					Y: y + headerSpace,
					W: cell*tile.SpanX + o.Gap*(tile.SpanX-1),
					H: cell*tile.SpanY + o.Gap*(tile.SpanY-1),
				}
				if tile.Rect.Bottom() > limit {
					tile.Rect.H = max(limit-tile.Rect.Y, 1)
				}
				cur.Tiles = append(cur.Tiles, tile)
			}
			y += rowHeight + o.BandGap
			pageHasContent = true
			row++
		}
		if cur != nil {
			closeBand()
			// Leave room for the heading of the next group, otherwise it
			// would be orphaned at the bottom of the page.
			if y+o.HeaderHeight > limit {
				closePage()
			}
		}
	}

	if pageHasContent {
		closePage()
	}

	if len(g.Pages_) == 0 {
		g.Pages_ = [][]Band{{}}
	}
	g.Pages = len(g.Pages_)
	return g
}

// layoutRows wraps the tiles of a group into rows. The first tile of a large
// group spans two columns and two rows, so the tiles after it fill the space
// beside it first, exactly like the focal tiles of the original start screen.
func layoutRows(spec GroupSpec, cols, cell int, o GroupOptions) [][]Tile {
	var rows [][]Tile
	addRow := func(index int, tile Tile) {
		for len(rows) <= index {
			rows = append(rows, nil)
		}
		rows[index] = append(rows[index], tile)
	}

	taken := make(map[[2]int]bool)
	mark := func(row, col, spanX, spanY int) {
		for dy := 0; dy < spanY; dy++ {
			for dx := 0; dx < spanX; dx++ {
				taken[[2]int{row + dy, col + dx}] = true
			}
		}
	}

	row, col := 0, 0
	next := func() {
		col++
		for col >= cols {
			col = 0
			row++
		}
	}
	for col < cols && taken[[2]int{row, col}] {
		next()
	}

	for i := 0; i < spec.Count; i++ {
		tile := Tile{Index: spec.First + i, SpanX: 1, SpanY: 1, Row: row, Col: col}
		if i == 0 && o.Large && cols >= 3 && spec.Count >= max(o.LargeMin, 1) {
			tile.Large = true
			tile.SpanX, tile.SpanY = 2, 2
		}
		addRow(row, tile)
		mark(row, col, tile.SpanX, tile.SpanY)

		if tile.SpanX > 1 || tile.SpanY > 1 {
			// Step over the whole block the large tile occupies.
			row, col = 0, tile.SpanX
		} else {
			next()
		}
		for col < cols && taken[[2]int{row, col}] {
			next()
		}
	}

	out := make([][]Tile, 0, len(rows))
	for _, r := range rows {
		if len(r) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// Hit returns the tile under a point, or nil.
func Hit(tiles []Tile, x, y int) *Tile {
	for i := range tiles {
		if tiles[i].Rect.Contains(x, y) {
			return &tiles[i]
		}
	}
	return nil
}

// Move returns the tile in a direction from current, or nil when the edge of
// the page is reached. Spanned tiles take part through their rectangle, so a
// large tile is reachable from both of its columns.
func Move(tiles []Tile, current *Tile, dx, dy int) *Tile {
	if current == nil || len(tiles) == 0 {
		return nil
	}
	from := current.Rect
	best := -1
	bestDist := 0

	for i := range tiles {
		t := tiles[i]
		if t.Index == current.Index {
			continue
		}
		// A tile is a candidate only when it lies strictly on the requested
		// side and shares an edge along the other axis, so a tile below a
		// large one is not treated as being to its left.
		switch {
		case dx > 0 && t.Rect.X >= from.Right() && overlap(t.Rect.Y, t.Rect.Bottom(), from.Y, from.Bottom()):
		case dx < 0 && t.Rect.Right() <= from.X && overlap(t.Rect.Y, t.Rect.Bottom(), from.Y, from.Bottom()):
		case dy > 0 && t.Rect.Y >= from.Bottom() && overlap(t.Rect.X, t.Rect.Right(), from.X, from.Right()):
		case dy < 0 && t.Rect.Bottom() <= from.Y && overlap(t.Rect.X, t.Rect.Right(), from.X, from.Right()):
		default:
			continue
		}
		dist := distance(from, t.Rect, dx, dy)
		if best < 0 || dist < bestDist {
			best, bestDist = i, dist
		}
	}
	if best < 0 {
		return nil
	}
	return &tiles[best]
}

// distance ranks the candidates: the gap along the direction of travel comes
// first, and how well the tiles line up across it only breaks ties. That is
// what makes "right" pick the tile in the same row rather than one further
// down.
func distance(from, to Rect, dx, dy int) int {
	along, across := 0, 0
	switch {
	case dx > 0:
		along, across = to.X-from.Right(), abs(to.Y-from.Y)
	case dx < 0:
		along, across = from.X-to.Right(), abs(to.Y-from.Y)
	case dy > 0:
		along, across = to.Y-from.Bottom(), abs(to.X-from.X)
	default:
		along, across = from.Y-to.Bottom(), abs(to.X-from.X)
	}
	return along + across/8
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func overlap(a1, a2, b1, b2 int) bool { return a1 < b2 && b1 < a2 }

// PageOf reports which page holds an entry index.
func (g GroupGrid) PageOf(index int) int {
	for i, page := range g.Pages_ {
		for _, band := range page {
			for _, t := range band.Tiles {
				if t.Index == index {
					return i
				}
			}
		}
	}
	return 0
}

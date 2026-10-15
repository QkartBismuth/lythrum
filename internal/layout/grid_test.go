package layout

import "testing"

func TestComputeGridFillsArea(t *testing.T) {
	area := Rect{W: 1920, H: 1080}
	g := ComputeGrid(area, 12, DefaultGridOptions())
	if len(g.Cells) == 0 {
		t.Fatal("no cells")
	}
	if g.PageSize != len(g.Cells) {
		t.Errorf("page size %d, cells %d", g.PageSize, len(g.Cells))
	}
	for i, c := range g.Cells {
		if c.Empty() {
			t.Fatalf("cell %d is empty: %+v", i, c)
		}
		if c.X < area.X || c.Y < area.Y || c.Right() > area.Right() || c.Bottom() > area.Bottom() {
			t.Errorf("cell %d escapes the area: %+v vs %+v", i, c, area)
		}
	}
}

func TestComputeGridGapsAreRespected(t *testing.T) {
	area := Rect{W: 1000, H: 800}
	o := DefaultGridOptions()
	o.Columns = 4
	o.Gap = 10
	g := ComputeGrid(area, 8, o)
	if g.Columns != 4 {
		t.Fatalf("columns = %d, want 4", g.Columns)
	}
	cells := g.Page(0, 8)
	if len(cells) != 8 {
		t.Fatalf("got %d cells", len(cells))
	}
	if got := cells[1].X - cells[0].Right(); got != o.Gap {
		t.Errorf("horizontal gap = %d, want %d", got, o.Gap)
	}
	if got := cells[4].Y - cells[0].Bottom(); got != o.Gap {
		t.Errorf("vertical gap = %d, want %d", got, o.Gap)
	}
}

func TestComputeGridBorder(t *testing.T) {
	o := DefaultGridOptions()
	o.Columns = 2
	o.Border = 30
	o.Gap = 0
	o.MaxCell = 0 // let the tiles fill the area so the border is measurable
	g := ComputeGrid(Rect{W: 800, H: 600}, 2, o)
	if g.Cells[0].X != 30 || g.Cells[0].Y != 30 {
		t.Errorf("first cell = %+v, want the border offset", g.Cells[0])
	}
	if g.Cells[0].W != (800-60)/2 {
		t.Errorf("cell width = %d, want %d", g.Cells[0].W, (800-60)/2)
	}
}

func TestComputeGridPages(t *testing.T) {
	o := DefaultGridOptions()
	o.Columns = 3
	g := ComputeGrid(Rect{W: 1920, H: 1080}, 7, o)
	if g.PageSize != g.Columns*g.Rows {
		t.Fatalf("rows=%d columns=%d pageSize=%d", g.Rows, g.Columns, g.PageSize)
	}
	if g.Pages != 1 {
		t.Errorf("7 items in a %d-cell grid should be one page, got %d", g.PageSize, g.Pages)
	}
	if cells := g.Page(0, 7); len(cells) != 7 {
		t.Errorf("page 0 has %d cells, want 7", len(cells))
	}
	if cells := g.Page(1, 7); len(cells) != 0 {
		t.Errorf("page 1 should be empty, got %d cells", len(cells))
	}

	g2 := ComputeGrid(Rect{W: 1920, H: 1080}, 20, o)
	if g2.PageSize >= 20 || g2.Pages < 2 {
		t.Errorf("20 items in a %d-cell grid should spill over, pages=%d", g2.PageSize, g2.Pages)
	}
	last := (20 - 1) / g2.PageSize
	if cells := g2.Page(last, 20); len(cells) != 20-last*g2.PageSize {
		t.Errorf("last page has %d cells, want %d", len(cells), 20-last*g2.PageSize)
	}
	if cells := g2.Page(-1, 20); len(cells) != g2.PageSize {
		t.Errorf("negative page should clamp to the first, got %d cells", len(cells))
	}
}

func TestComputeGridMinCellLimitsColumns(t *testing.T) {
	o := DefaultGridOptions()
	o.MinCell = 400
	o.MaxCell = 0
	o.Border = 0
	g := ComputeGrid(Rect{W: 1000, H: 1000}, 16, o)
	if g.Columns*g.Cells[0].W > 1000 {
		t.Fatalf("grid does not fit: %d columns of %d", g.Columns, g.Cells[0].W)
	}
	if g.Cells[0].W < o.MinCell {
		t.Errorf("cell width %d is below the minimum %d", g.Cells[0].W, o.MinCell)
	}
}

func TestComputeGridWideAspect(t *testing.T) {
	o := DefaultGridOptions()
	o.Aspect = 2
	o.Border = 0
	o.Gap = 0
	wide := ComputeGrid(Rect{W: 1600, H: 600}, 8, o)
	if wide.Cells[0].W <= wide.Cells[0].H {
		t.Errorf("aspect 2 should make tiles wider than tall, got %+v", wide.Cells[0])
	}
}

func TestComputeGridDegenerateArea(t *testing.T) {
	g := ComputeGrid(Rect{}, 5, DefaultGridOptions())
	if len(g.Cells) == 0 || g.Cells[0].Empty() {
		t.Errorf("degenerate area should still yield a usable cell: %+v", g)
	}
	g = ComputeGrid(Rect{W: 100, H: 50}, 0, DefaultGridOptions())
	if g.PageSize < 1 || g.Pages < 1 {
		t.Errorf("zero items should still produce one page: %+v", g)
	}
}

func TestGridHitAndNext(t *testing.T) {
	o := DefaultGridOptions()
	o.Columns = 3
	o.Gap = 0
	g := ComputeGrid(Rect{W: 900, H: 600}, 6, o)
	cells := g.Page(0, 6)
	if len(cells) != 6 {
		t.Fatalf("cells = %d", len(cells))
	}
	hit := g.Hit(cells, cells[4].X+1, cells[4].Y+1)
	if hit != 4 {
		t.Errorf("hit = %d, want 4", hit)
	}
	if hit := g.Hit(cells, -1, -1); hit != -1 {
		t.Errorf("outside point should miss, got %d", hit)
	}
	if got := Next(5, 1, 6); got != 0 {
		t.Errorf("Next wrapped to %d, want 0", got)
	}
	if got := Next(0, -1, 6); got != 5 {
		t.Errorf("Next wrapped to %d, want 5", got)
	}
	if got := Next(0, 1, 0); got != -1 {
		t.Errorf("Next on an empty grid = %d, want -1", got)
	}
	if got := (Grid{PageSize: 6}).PageFor(7); got != 1 {
		t.Errorf("PageFor = %d, want 1", got)
	}
}

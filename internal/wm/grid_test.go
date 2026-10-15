package wm

import (
	"strings"
	"testing"

	"lythrum/internal/config"
	"lythrum/internal/desk"
	"lythrum/internal/layout"
	"lythrum/internal/widget"
)

// TestEntryMovementCrossesPages models what the keyboard does in the grid,
// without needing a display.
func TestStartScreenMovementUsesTheLayout(t *testing.T) {
	s := &startScreen{
		wm:      &WM{cfg: config.Default()},
		entries: make([]desk.Entry, 12),
		overlays: []*overlay{{
			area: layout.Rect{W: 800, H: 900},
			grid: layout.ComputeGroups(layout.Rect{W: 800, H: 900},
				[]layout.GroupSpec{{Name: "A", First: 0, Count: 6}, {Name: "B", First: 6, Count: 6}},
				layout.DefaultGroupOptions()),
		}},
	}
	for i := range s.entries {
		s.entries[i] = desk.Entry{Name: "e"}
	}
	tiles := s.overlays[0].grid.Tiles(0)
	if len(tiles) < 2 {
		t.Fatalf("tiles = %d", len(tiles))
	}
	s.selection = tiles[0].Index
	s.currentX, s.currentY = tiles[0].Rect.X, tiles[0].Rect.Y

	s.MoveSelection(1, 0)
	if s.selection == tiles[0].Index {
		t.Error("moving right did not change the selection")
	}
}

func TestEntryMovementCrossesPages(t *testing.T) {
	entries := make([]desk.Entry, 10)
	for i := range entries {
		entries[i] = desk.Entry{Name: "item"}
	}
	o := layout.DefaultGridOptions()
	o.Columns = 4
	o.MaxCell = 0 // let the tiles fill the screen, which leaves room for many rows
	o.Gap = 8
	o.Border = 24
	g := layout.ComputeGrid(layout.Rect{W: 1920, H: 1080}, len(entries), o)
	if g.Pages < 2 {
		t.Fatalf("the test needs several pages, got %d", g.Pages)
	}

	// Moving right past the end of a page wraps around the whole list.
	idx := g.PageSize - 1
	next := layout.Next(idx, 1, len(entries))
	if next != g.PageSize {
		t.Errorf("moving past the last cell of a page went to %d, want %d", next, g.PageSize)
	}
	if got := (layout.Grid{PageSize: g.PageSize}).PageFor(next); got != 1 {
		t.Errorf("item %d should be on page %d, got %d", next, 1, got)
	}

	back := layout.Next(0, -1, len(entries))
	if back != len(entries)-1 {
		t.Errorf("moving left from the first cell went to %d, want %d", back, len(entries)-1)
	}
}

// TestTileHitAreas checks that every tile in a page has a hit area and that
// the gaps between tiles belong to no tile.
func TestTileHitAreas(t *testing.T) {
	o := layout.DefaultGridOptions()
	o.Columns = 4
	o.Gap = 10
	area := layout.Rect{W: 1280, H: 800}
	g := layout.ComputeGrid(area, 8, o)
	cells := g.Page(0, 8)
	if len(cells) != 8 {
		t.Fatalf("cells = %d", len(cells))
	}
	for i, c := range cells {
		if g.Hit(cells, c.X, c.Y) != i {
			t.Errorf("top-left of cell %d does not hit it", i)
		}
		if g.Hit(cells, c.Right()-1, c.Bottom()-1) != i {
			t.Errorf("bottom-right of cell %d does not hit it", i)
		}
	}
	if g.Hit(cells, cells[0].Right()+2, cells[0].Y+2) != -1 && cells[0].Right()+2 < cells[1].X {
		t.Error("the gap between tiles should belong to no tile")
	}
}

func TestCaptionLinesFitTheTile(t *testing.T) {
	// Without a font the caption falls back to character counting, which must
	// still produce at least one line.
	s := &startScreen{}
	lines := s.captionLines("Положение Лига твоего имени (Северсталь).pdf", 120, 2)
	if len(lines) == 0 || len(lines) > 2 {
		t.Errorf("captionLines = %q", lines)
	}
	if one := s.captionLines("Положение Лига твоего имени (Северсталь).pdf", 120, 1); len(one) != 1 {
		t.Errorf("a large tile must get a single caption line, got %q", one)
	}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" && len(lines) > 1 {
			t.Errorf("empty line in %q", lines)
		}
	}
	if got := s.captionLines("x", 0, 2); got == nil {
		t.Error("a zero width should still yield a caption")
	}
}

func TestStartScreenStatusIsJSONFriendly(t *testing.T) {
	s := &startScreen{dir: "/home/Desktop"}
	status := s.Status()
	for _, key := range []string{"open", "dir", "entries", "page", "selection"} {
		if _, ok := status[key]; !ok {
			t.Errorf("status is missing %q", key)
		}
	}
	if !strings.Contains(s.String(), "/home/Desktop") {
		t.Errorf("String() = %q", s.String())
	}
}

// TestStartScreenMovesAcrossPages checks that walking down past the last row
// continues on the following page, and back up on the previous one.
func TestStartScreenMovesAcrossPages(t *testing.T) {
	entries := make([]desk.Entry, 40)
	for i := range entries {
		entries[i] = desk.Entry{Name: "item"}
	}
	cfg := config.Default()
	cfg.GridMaxCell = 180
	cfg.GridLarge = false

	s := &startScreen{
		wm:      &WM{cfg: cfg},
		entries: entries,
		groups:  []desk.Group{{Key: "files", Title: "Files", First: 0, Count: 40}},
		overlays: []*overlay{{
			area: layout.Rect{W: 1280, H: 720},
		}},
	}
	s.layout()
	g := s.overlays[0].grid
	if g.PageCount() < 2 {
		t.Fatalf("the test needs several pages, got %d", g.PageCount())
	}

	first := g.Tiles(0)
	s.selection = first[len(first)-1].Index

	s.MoveSelection(0, 1)
	if s.page != 1 {
		t.Fatalf("moving down past the last row stayed on page %d, want 1", s.page)
	}
	if page := g.PageOf(s.selection); page != 1 {
		t.Errorf("selection %d is on page %d, want 1", s.selection, page)
	}

	s.MoveSelection(0, -1)
	if s.page != 0 {
		t.Errorf("moving up from the first row went to page %d, want 0", s.page)
	}
	if page := g.PageOf(s.selection); page != 0 {
		t.Errorf("selection %d is on page %d, want 0", s.selection, page)
	}
}

// TestStartScreenLayoutAfterWidgetEntries checks the real scan path: the
// widget tiles have to be laid out together with the desktop files.
func TestStartScreenLayoutAfterWidgetEntries(t *testing.T) {
	reg := widget.NewRegistry(widget.Session{})
	s := &startScreen{wm: &WM{cfg: config.Default(), widgets: reg}}
	entries := []desk.Entry{
		{Name: "Заметки.txt", Path: "/home/u/Desktop/Заметки.txt", Kind: desk.KindFile},
		{Name: "clip.mp4", Path: "/home/u/Desktop/clip.mp4", Kind: desk.KindFile},
	}
	entries = s.addWidgetEntries(entries)
	if len(entries) < 3 {
		t.Fatalf("widget entries were not added: %+v", entries)
	}
	s.groups, s.entries = desk.Grouped(entries, s.wm.cfg.GridGroups)
	if len(s.groups) < 2 {
		t.Fatalf("groups = %+v", s.groups)
	}
	var tiles int
	for _, band := range s.groups {
		tiles += band.Count
	}
	if tiles != len(s.entries) {
		t.Errorf("the bands cover %d of %d entries", tiles, len(s.entries))
	}
	if s.entries[0].Widget == "" {
		t.Error("the first tile must be a widget")
	}
}

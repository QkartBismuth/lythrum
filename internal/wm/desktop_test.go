package wm

import (
	"image/color"
	"testing"

	"lythrum/internal/config"
	"lythrum/internal/desk"
	"lythrum/internal/layout"
	"lythrum/internal/x11"
)

// TestRootPointOffsetsByTheWindowOrigin checks the conversion a grabbed
// pointer needs. The power menu lives in the middle of the screen, so a click
// has to travel the window origin, not double its own distance.
func TestRootPointOffsetsByTheWindowOrigin(t *testing.T) {
	area := layout.Rect{X: 0, Y: 40, W: 1280, H: 760}
	x, y := rootPoint(area, 100, 380)
	if x != 100 || y != 420 {
		t.Errorf("rootPoint = (%d,%d), want (100,420)", x, y)
	}
	// A press on the top row of a menu that starts below the panel has to land
	// on that row, not above it.
	card, rows := func() (layout.Rect, []layout.Rect) {
		s := &powerScreen{wm: &WM{}, area: area}
		s.rows = []powerRow{{id: "logout"}, {id: "suspend"}}
		return s.layoutRects()
	}()
	if card.Y <= area.Y {
		t.Fatalf("the card %+v should sit inside the work area %+v", card, area)
	}
	lx, ly := rows[0].X-area.X, rows[0].Y-area.Y
	rx, ry := rootPoint(area, lx, ly)
	if got := (&powerScreen{rects: rows}).rowAt(rx, ry); got != 0 {
		t.Errorf("a press on the first row landed on row %d, want 0", got)
	}
	lx, ly = rows[0].X-area.X, rows[0].Y-area.Y-rows[0].H
	if rx, ry = rootPoint(area, lx, ly); (&powerScreen{rects: rows}).rowAt(rx, ry) != -1 {
		t.Error("a press above the rows should belong to no row")
	}
}

// TestDesktopLayerKeepsOnlyItsOwnDirectory checks that the icons are the files
// of the desktop directory, and not the launchers and home files the start
// screen collects.
func TestDesktopLayerKeepsOnlyItsOwnDirectory(t *testing.T) {
	dir := t.TempDir()
	d := &desktopLayer{wm: &WM{cfg: config.Default()}}
	d.dir = dir
	d.entries = nil

	in := []desk.Entry{
		{Path: dir + "/Заметки.txt", Kind: desk.KindFile},
		{Path: dir + "/Work", Kind: desk.KindDir},
		{Path: "/usr/share/applications/firefox.desktop", Kind: desk.KindApp},
		{Path: dir + "/../Documents/report.pdf", Kind: desk.KindFile},
	}
	for _, e := range in {
		if underDir(e.Path, dir) {
			d.entries = append(d.entries, e)
		}
	}
	if len(d.entries) != 2 {
		t.Fatalf("icons = %+v, want the two entries inside %s", d.entries, dir)
	}
}

// TestDesktopIconSelectionMovesWithTheScan checks that a scan that loses an
// entry cannot leave the selection pointing past the end of the list.
func TestDesktopIconSelectionMovesWithTheScan(t *testing.T) {
	d := &desktopLayer{wm: &WM{cfg: config.Default()}, entries: make([]desk.Entry, 5)}
	for i := range d.entries {
		d.entries[i] = desk.Entry{Name: "f", Path: "/tmp/" + string(rune('a'+i))}
	}
	d.selection = 4
	d.entries = d.entries[:2]
	if d.selection >= len(d.entries) {
		d.selection = maxInt(len(d.entries)-1, 0)
	}
	if d.selection != 1 {
		t.Errorf("selection = %d, want 1", d.selection)
	}
}

// TestGlyphForIsOpaque checks the placeholder icon: X cannot blend it with the
// wallpaper, so it must carry its own plate.
func TestGlyphForIsOpaque(t *testing.T) {
	colors := gridColors{
		GlyphFile: color.RGBA{R: 0xbf, G: 0x95, B: 0x6b, A: 0xff},
		GlyphApp:  color.RGBA{R: 0x88, G: 0xc0, B: 0xd0, A: 0xff},
	}
	img := onPlate(glyphFor(desk.Entry{Name: "x", Kind: desk.KindApp}, 32, colors), 32,
		color.RGBA{R: 0x16, G: 0x1b, B: 0x24, A: 0xff})
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if got := img.RGBAAt(x, y).A; got != 0xff {
				t.Fatalf("pixel (%d,%d) has alpha %d, the plate has to be opaque", x, y, got)
			}
		}
	}
	if img.RGBAAt(16, 16) == img.RGBAAt(0, 0) {
		t.Error("the icon itself should differ from the corner of the plate")
	}
}

// TestStartScreenDragPagesTheGrid checks that a drag moves the canvas while a
// plain press still selects a tile.
func TestStartScreenDragPagesTheGrid(t *testing.T) {
	cfg := config.Default()
	cfg.GridLarge = false
	s := &startScreen{
		wm:       &WM{cfg: cfg},
		entries:  make([]desk.Entry, 40),
		overlays: []*overlay{{area: layout.Rect{W: 1280, H: 720}}},
	}
	for i := range s.entries {
		s.entries[i] = desk.Entry{Name: "item"}
	}
	s.groups = []desk.Group{{Key: "files", Title: "Files", First: 0, Count: 40}}
	s.open = true
	s.layout()
	if s.overlays[0].grid.PageCount() < 2 {
		t.Fatalf("the test needs several pages, got %d", s.overlays[0].grid.PageCount())
	}

	s.BeginDrag(600, 600)
	s.DragTo(600, 600-dragThreshold+1)
	if s.page != 0 {
		t.Fatalf("a small move paged the grid to %d", s.page)
	}
	s.DragTo(600, 600-dragThreshold-5)
	if s.page != 1 {
		t.Fatalf("dragging up went to page %d, want 1", s.page)
	}
	// The press that turned into a drag must not select anything on release.
	before := s.selection
	s.HandleButton(x11.ButtonPress{ButtonEvent: x11.ButtonEvent{Button: 1}}, 10, 10)
	if s.selection != before {
		t.Errorf("a drag changed the selection from %d to %d", before, s.selection)
	}
	s.EndDrag()
}

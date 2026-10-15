package layout

import "testing"

func threeGroups() []GroupSpec {
	return []GroupSpec{
		{Name: "Applications", First: 0, Count: 3},
		{Name: "Documents", First: 3, Count: 2},
		{Name: "Folders", First: 5, Count: 1},
	}
}

func TestComputeGroupsLaysOutBands(t *testing.T) {
	area := Rect{W: 1920, H: 1080}
	g := ComputeGroups(area, threeGroups(), DefaultGroupOptions())

	if g.Columns < 3 {
		t.Fatalf("columns = %d, want room for a large tile", g.Columns)
	}
	if len(g.Bands) != 3 {
		t.Fatalf("bands = %d", len(g.Bands))
	}
	if g.PageCount() == 0 {
		t.Fatal("no pages")
	}
	placed := 0
	for page := 0; page < g.PageCount(); page++ {
		placed += len(g.Tiles(page))
	}
	if placed != 6 {
		t.Errorf("placed %d tiles across %d pages, want 6", placed, g.PageCount())
	}
	for _, band := range g.Bands {
		if band.Name == "" {
			t.Error("a band has no header name")
		}
		if band.Tiles[0].Rect.X != area.X+DefaultGroupOptions().Border {
			t.Errorf("band %q starts at %d, want the border offset", band.Name, band.Tiles[0].Rect.X)
		}
		for _, tile := range band.Tiles {
			if tile.Rect.Empty() {
				t.Errorf("band %q has an empty tile %+v", band.Name, tile)
			}
			if tile.Rect.Right() > area.Right() || tile.Rect.Bottom() > area.Bottom() {
				t.Errorf("band %q tile %+v escapes the area %+v", band.Name, tile.Rect, area)
			}
		}
	}
}

func TestComputeGroupsLargeTile(t *testing.T) {
	o := DefaultGroupOptions()
	area := Rect{W: 1920, H: 1080}
	g := ComputeGroups(area, []GroupSpec{{Name: "Apps", First: 0, Count: 5}}, o)

	tiles := g.Tiles(0)
	if !tiles[0].Large {
		t.Fatal("the first tile should be large")
	}
	if tiles[0].SpanX != 2 || tiles[0].SpanY != 2 {
		t.Errorf("large tile spans %dx%d", tiles[0].SpanX, tiles[0].SpanY)
	}
	second := tiles[1]
	if second.Row != 0 || second.Col != 2 {
		t.Errorf("the tile next to a large one should start at column 2, got %+v", second)
	}
	if second.Rect.X != tiles[0].Rect.Right()+o.Gap {
		t.Errorf("the gap next to the large tile is %d", second.Rect.X-tiles[0].Rect.Right())
	}
	// The large tile is twice the width and height of a small one.
	third := tiles[2]
	if tiles[0].Rect.W <= third.Rect.W || tiles[0].Rect.H <= third.Rect.H {
		t.Errorf("large %+v is not larger than small %+v", tiles[0].Rect, third.Rect)
	}
}

func TestComputeGroupsWithoutLargeTiles(t *testing.T) {
	o := DefaultGroupOptions()
	o.Large = false
	g := ComputeGroups(Rect{W: 1920, H: 1080}, []GroupSpec{{Name: "Apps", First: 0, Count: 4}}, o)
	for _, tile := range g.Tiles(0) {
		if tile.Large {
			t.Error("large tiles should be off")
		}
	}
	if got := g.Tiles(0)[1]; got.Row != 0 || got.Col != 1 {
		t.Errorf("second tile at %+v", got)
	}
}

func TestComputeGroupsPages(t *testing.T) {
	o := DefaultGroupOptions()
	o.Cell = 180
	var specs []GroupSpec
	for g := 0; g < 6; g++ {
		specs = append(specs, GroupSpec{Name: "Group", First: g * 5, Count: 5})
	}
	grid := ComputeGroups(Rect{W: 1280, H: 720}, specs, o)
	if grid.PageCount() < 2 {
		t.Fatalf("six groups of five tiles should need several pages, got %d", grid.PageCount())
	}

	total := 0
	seen := map[int]bool{}
	for page := 0; page < grid.PageCount(); page++ {
		for _, tile := range grid.Tiles(page) {
			if seen[tile.Index] {
				t.Errorf("tile %d appears on more than one page", tile.Index)
			}
			seen[tile.Index] = true
			total++
		}
	}
	if total != 30 {
		t.Errorf("placed %d tiles, want 30", total)
	}
	for i := 0; i < 30; i++ {
		if !seen[i] {
			t.Errorf("tile %d was never placed", i)
		}
	}
}

func TestComputeGroupsEmptyAndTiny(t *testing.T) {
	if g := ComputeGroups(Rect{W: 100, H: 100}, nil, DefaultGroupOptions()); g.PageCount() != 1 {
		t.Errorf("no groups should still give one page, got %d", g.PageCount())
	}
	if g := ComputeGroups(Rect{W: 1920, H: 1080}, []GroupSpec{{Name: "Empty", First: 0, Count: 0}}, DefaultGroupOptions()); g.PageCount() != 1 {
		t.Errorf("an empty group should still give one page, got %d", g.PageCount())
	}
	g := ComputeGroups(Rect{}, []GroupSpec{{Name: "x", First: 0, Count: 1}}, DefaultGroupOptions())
	if g.PageCount() != 1 || len(g.Tiles(0)) != 0 {
		t.Errorf("a degenerate area should give one empty page: %+v", g)
	}
	if got := g.Page(5); got != nil {
		t.Errorf("an out-of-range page should be empty, got %+v", got)
	}
}

func TestGroupHitAndMove(t *testing.T) {
	// A narrow area makes the strip wrap, so there is a row to move to.
	g := ComputeGroups(Rect{W: 800, H: 900}, []GroupSpec{{Name: "Apps", First: 0, Count: 8}}, DefaultGroupOptions())
	tiles := g.Tiles(0)
	if len(tiles) < 4 {
		t.Fatalf("tiles = %d", len(tiles))
	}

	// Hit testing uses the tile rectangles.
	hit := Hit(tiles, tiles[2].Rect.X+5, tiles[2].Rect.Y+5)
	if hit == nil || hit.Index != tiles[2].Index {
		t.Fatalf("hit = %+v, want tile %d", hit, tiles[2].Index)
	}
	if Hit(tiles, -10, -10) != nil {
		t.Error("a point outside every tile should not hit")
	}

	// Moving right from the large tile lands on the tile in column two.
	right := Move(tiles, &tiles[0], 1, 0)
	if right == nil || right.Index != tiles[1].Index {
		t.Errorf("moving right from the large tile = %+v, want tile %d", right, tiles[1].Index)
	}
	// Moving left from the tile in column two lands on the large tile.
	left := Move(tiles, &tiles[1], -1, 0)
	if left == nil || left.Index != tiles[0].Index {
		t.Errorf("moving left = %+v, want the large tile", left)
	}
	// Down from the first small tile finds the tile below it.
	down := Move(tiles, &tiles[1], 0, 1)
	if down == nil {
		t.Fatal("no tile below")
	}
	if down.Rect.Y <= tiles[1].Rect.Y {
		t.Errorf("moving down went to %+v", down)
	}
	if up := Move(tiles, &tiles[len(tiles)-1], 0, -1); up == nil {
		t.Error("no tile above the last one")
	}
	// At the edge of a page there is nothing to move to.
	if got := Move(tiles, &tiles[0], -1, 0); got != nil {
		t.Errorf("moving left off the page returned %+v", got)
	}
	if got := Move(nil, nil, 1, 0); got != nil {
		t.Error("moving on an empty page should return nothing")
	}
}

func TestGroupPageOf(t *testing.T) {
	o := DefaultGroupOptions()
	var specs []GroupSpec
	for g := 0; g < 6; g++ {
		specs = append(specs, GroupSpec{Name: "G", First: g * 4, Count: 4})
	}
	grid := ComputeGroups(Rect{W: 1280, H: 720}, specs, o)
	for page := 0; page < grid.PageCount(); page++ {
		for _, tile := range grid.Tiles(page) {
			if got := grid.PageOf(tile.Index); got != page {
				t.Errorf("tile %d is on page %d but PageOf says %d", tile.Index, page, got)
			}
		}
	}
	if got := grid.PageOf(999); got != 0 {
		t.Errorf("PageOf of an unknown index = %d", got)
	}
}

func TestComputeGroupsClampsTallBands(t *testing.T) {
	o := DefaultGroupOptions()
	o.Cell = 400
	specs := []GroupSpec{{Name: "Huge", First: 0, Count: 40}}
	area := Rect{W: 1200, H: 500}
	g := ComputeGroups(area, specs, o)
	for _, tile := range g.Tiles(0) {
		if tile.Rect.Bottom() > area.Bottom() {
			t.Errorf("tile %+v escapes the short area %+v", tile.Rect, area)
		}
	}
	if len(g.Tiles(0)) == 0 {
		t.Error("a clamped band should still show something")
	}
}

func TestComputeGroupsLargeTileNeedsEnoughEntries(t *testing.T) {
	o := DefaultGroupOptions()
	o.Columns = 6
	o.Large = true
	o.LargeMin = 3

	small := ComputeGroups(Rect{W: 1600, H: 900}, []GroupSpec{{Name: "S", First: 0, Count: 2}}, o)
	for _, tile := range small.Tiles(0) {
		if tile.Large {
			t.Fatal("a group below grid_large_min must not get a large tile")
		}
	}

	big := ComputeGroups(Rect{W: 1600, H: 900}, []GroupSpec{{Name: "B", First: 0, Count: 3}}, o)
	if len(big.Tiles(0)) == 0 || !big.Tiles(0)[0].Large {
		t.Fatal("a group at grid_large_min should start with a large tile")
	}
	if got := big.Tiles(0)[0].Rect.W; got != 2*o.Cell+o.Gap {
		t.Errorf("large tile width = %d, want %d", got, 2*o.Cell+o.Gap)
	}
}

func TestComputeGroupsFlowsGroupsAcrossPages(t *testing.T) {
	o := DefaultGroupOptions()
	o.Large = false
	o.Cell = 180
	specs := []GroupSpec{{Name: "Flow", First: 0, Count: 40}}
	screen := Rect{W: 1280, H: 720}
	g := ComputeGroups(screen, specs, o)
	if g.PageCount() < 2 {
		t.Fatalf("a long group should flow onto more pages, got %d", g.PageCount())
	}
	seen := map[int]bool{}
	for page := 0; page < g.PageCount(); page++ {
		bands := g.Page(page)
		if len(bands) == 0 {
			t.Fatalf("page %d is empty", page)
		}
		for _, b := range bands {
			if b.Name != "Flow" {
				t.Errorf("page %d has band %q, want the continued group", page, b.Name)
			}
			if b.Header.H != o.HeaderHeight {
				t.Errorf("band header height = %d, want %d", b.Header.H, o.HeaderHeight)
			}
			for _, tile := range b.Tiles {
				if seen[tile.Index] {
					t.Errorf("tile %d appears twice", tile.Index)
				}
				seen[tile.Index] = true
				if tile.Rect.Bottom() > screen.Bottom() {
					t.Errorf("tile %d escapes the screen: %+v", tile.Index, tile.Rect)
				}
			}
		}
	}
	if len(seen) != 40 {
		t.Errorf("laid out %d tiles, want 40", len(seen))
	}
}

func TestComputeGroupsKeepsHeadingsWithTiles(t *testing.T) {
	o := DefaultGroupOptions()
	o.Large = false
	var specs []GroupSpec
	for i := 0; i < 4; i++ {
		specs = append(specs, GroupSpec{Name: "G", First: i * 3, Count: 3})
	}
	g := ComputeGroups(Rect{W: 1280, H: 720}, specs, o)
	for page := 0; page < g.PageCount(); page++ {
		for _, b := range g.Page(page) {
			if len(b.Tiles) == 0 {
				t.Errorf("page %d has a heading without tiles", page)
			}
			if b.Header.Bottom() > b.Tiles[0].Rect.Y {
				t.Errorf("page %d: heading %+v overlaps the first tile %+v", page, b.Header, b.Tiles[0].Rect)
			}
			for _, tile := range b.Tiles {
				if tile.Rect.Y < b.Header.Bottom() {
					t.Errorf("page %d: tile %+v overlaps its heading %+v", page, tile.Rect, b.Header)
				}
			}
		}
	}
}

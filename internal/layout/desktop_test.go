package layout

import "testing"

// TestDesktopIconsStayOnScreen checks that every slot fits the monitor and
// that the slots do not overlap.
func TestDesktopIconsStayOnScreen(t *testing.T) {
	area := Rect{W: 1280, H: 800}
	o := DefaultDesktopIconOptions()
	rects := DesktopIcons(area, 500, o)
	if len(rects) == 0 {
		t.Fatal("no icon slots were placed")
	}
	for i, r := range rects {
		if r.Right() > area.Right() || r.Bottom() > area.Bottom() {
			t.Fatalf("slot %d %+v does not fit %+v", i, r, area)
		}
		if r.X < area.X || r.Y < area.Y {
			t.Fatalf("slot %d %+v starts before %+v", i, r, area)
		}
		for j := 0; j < i; j++ {
			if overlap(r.X, r.Right(), rects[j].X, rects[j].Right()) &&
				overlap(r.Y, r.Bottom(), rects[j].Y, rects[j].Bottom()) {
				t.Fatalf("slots %d and %d overlap: %+v and %+v", i, j, r, rects[j])
			}
		}
	}
	if got := IconHit(rects, rects[0].X+1, rects[0].Y+1); got != 0 {
		t.Errorf("IconHit on the first slot = %d, want 0", got)
	}
	if got := IconHit(rects, area.W-1, area.H-1); got != -1 {
		t.Errorf("IconHit in the empty corner = %d, want -1", got)
	}
}

// TestDesktopIconsFillColumnsFirst checks the reading order: the first slots
// form a column, then the next column starts.
func TestDesktopIconsFillColumnsFirst(t *testing.T) {
	area := Rect{W: 640, H: 480}
	o := DesktopIconOptions{Cell: 100, Icon: 64, Gap: 10, Left: 10, Top: 10}
	rects := DesktopIcons(area, 30, o)
	if len(rects) < 5 {
		t.Fatalf("slots = %d", len(rects))
	}
	if rects[1].X != rects[0].X || rects[1].Y <= rects[0].Y {
		t.Errorf("the second slot should sit below the first: %+v then %+v", rects[0], rects[1])
	}
	// A 480 tall screen takes four rows of 100 pixels, so the fifth slot opens
	// the second column.
	rows := (area.H - 2*o.Top + o.Gap) / (o.Cell + o.Gap)
	second := rects[rows]
	if second.X <= rects[0].X {
		t.Errorf("the column did not advance: %+v", second)
	}
}

func TestDesktopIconsOfNothing(t *testing.T) {
	if rects := DesktopIcons(Rect{W: 800, H: 600}, 0, DefaultDesktopIconOptions()); rects != nil {
		t.Errorf("no entries should place no slots, got %+v", rects)
	}
}

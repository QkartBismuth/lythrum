package x11

import (
	"image/color"
	"os"
	"testing"
)

// TestPaintAndText draws a small scene on a scratch window. It is the smoke
// test for the GC, rectangle, image and Xft text paths.
func TestPaintAndText(t *testing.T) {
	if os.Getenv("LYTHRUM_X11_TEST") == "" {
		t.Skip("set LYTHRUM_X11_TEST=1 to run against a live display")
	}
	d, err := Open("")
	if err != nil {
		t.Skipf("no display: %v", err)
	}
	defer d.Close()

	win, err := d.CreateWindow(CreateWindowParams{
		Parent:           d.Root(),
		X:                0,
		Y:                0,
		Width:            320,
		Height:           200,
		BackPixel:        0x202020,
		OverrideRedirect: true,
	})
	if err != nil {
		t.Fatalf("create window: %v", err)
	}
	defer d.DestroyWindow(win)

	gc, err := d.CreateGC(win)
	if err != nil {
		t.Fatalf("create gc: %v", err)
	}
	defer d.FreeGC(gc)

	font, err := d.OpenFont("sans:size=13", 17)
	if err != nil {
		t.Skipf("no usable font: %v", err)
	}
	defer d.CloseFont(font)

	fd, err := d.NewFontDraw(win, font)
	if err != nil {
		t.Fatalf("font draw: %v", err)
	}
	defer fd.Close(d)

	// Metrics must be sane for a string of known glyphs.
	if w := font.Width("Lythrum"); w <= 0 || w > 320 {
		t.Errorf("Width(\"Lythrum\") = %d, want a positive value below the window width", w)
	}
	if h := font.Height(); h <= 0 || h > 200 {
		t.Errorf("Height = %d", h)
	}
	if got := font.Truncate("a very long caption that will not fit", 40); got == "" {
		t.Error("Truncate returned nothing")
	} else if w := font.Width(got); w > 40 {
		t.Errorf("Truncate left %d pixels wide", w)
	}

	d.MapRaised(win)
	d.SetForegroundOf(gc, 0x3355aa)
	d.FillRectangle(win, gc, 10, 10, 100, 60)
	d.SetForegroundOf(gc, 0xffffff)
	fd.SetColor(d, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	fd.DrawText(d, 20, 100, "Lythrum start screen")

	img := SolidImage(32, 32, color.RGBA{R: 0x88, G: 0xc0, B: 0xd0, A: 0xff})
	if err := d.UploadImage(win, gc, img, 200, 20); err != nil {
		t.Errorf("UploadImage: %v", err)
	}

	pix, err := d.CreatePixmap(64, 64)
	if err != nil {
		t.Errorf("CreatePixmap: %v", err)
	} else {
		d.SetForegroundOf(gc, 0xff8800)
		d.FillRectangle(Window(pix), gc, 0, 0, 64, 64)
		d.CopyPixmap(win, gc, pix, 0, 0, 260, 20, 64, 64)
		d.FreePixmap(pix)
	}

	d.Sync()
}

// TestFontMeasuresUTF8Letters guards the text measuring: a Cyrillic letter is
// two bytes, and measuring byte by byte made every Russian label look twice as
// wide as it is, which showed up as clipped titles and hints.
func TestFontMeasuresUTF8Letters(t *testing.T) {
	if os.Getenv("LYTHRUM_X11_TEST") == "" {
		t.Skip("set LYTHRUM_X11_TEST=1 to run against a live display")
	}
	d, err := Open("")
	if err != nil {
		t.Skipf("no display: %v", err)
	}
	defer d.Close()
	font, err := d.OpenFont("sans:size=13", 17)
	if err != nil {
		t.Skipf("no usable font: %v", err)
	}
	defer d.CloseFont(font)

	latin := font.Width("aaaa")
	cyrillic := font.Width("аааа")
	if latin <= 0 {
		t.Skip("the face reports no widths")
	}
	if cyrillic > latin*13/10 || cyrillic < latin*7/10 {
		t.Errorf("four Cyrillic letters measure %d px, four Latin letters %d px", cyrillic, latin)
	}
	if got := font.Width("Завершить сеанс"); got <= 0 {
		t.Errorf("a Cyrillic label measures %d px", got)
	}
	// Truncation has to keep the label inside the space it was given.
	label := "Завершить сеанс и выйти из учётной записи"
	if truncated := font.Truncate(label, 120); truncated == label {
		t.Error("a label wider than the limit was not truncated")
	} else if font.Width(truncated) > 120 {
		t.Errorf("truncated label measures %d px, want at most 120", font.Width(truncated))
	}
}

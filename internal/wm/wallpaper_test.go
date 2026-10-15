package wm

import (
	"image"
	"image/color"
	"testing"
)

// TestDimImageMatchesTheSetting checks that wallpaper_dim is the share of the
// light that is taken away: zero keeps the picture, one turns it black.
func TestDimImageMatchesTheSetting(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 0xff})

	if got := dimImage(src, 0); got.RGBAAt(0, 0) != src.RGBAAt(0, 0) {
		t.Errorf("dim 0 changed the picture: %+v", got.RGBAAt(0, 0))
	}
	half := dimImage(src, 0.5).RGBAAt(0, 0)
	if half.R < 95 || half.R > 105 {
		t.Errorf("dim 0.5 gave %+v, want about half the brightness", half)
	}
	black := dimImage(src, 1).RGBAAt(0, 0)
	if black.R != 0 || black.G != 0 || black.B != 0 {
		t.Errorf("dim 1 gave %+v, want black", black)
	}
	if half.A != 0xff || black.A != 0xff {
		t.Error("a dimmed wallpaper has to stay opaque")
	}
}

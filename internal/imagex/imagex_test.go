package imagex

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestFitKeepsAspect(t *testing.T) {
	src := solid(200, 100, color.RGBA{R: 255, A: 255})
	got := Fit(src, 50, 50)
	if got.Bounds().Dx() != 50 || got.Bounds().Dy() != 50 {
		t.Fatalf("bounds = %v", got.Bounds())
	}
	// A 2:1 image in a square box is 50 wide and 25 tall, centred.
	c := got.RGBAAt(25, 25)
	if c.A == 0 {
		t.Error("the middle should be covered by the image")
	}
	if got.RGBAAt(25, 2).A != 0 {
		t.Error("the top strip should stay transparent")
	}
	if got.RGBAAt(25, 47).A != 0 {
		t.Error("the bottom strip should stay transparent")
	}
}

func TestFitUpScales(t *testing.T) {
	src := solid(4, 4, color.RGBA{B: 255, A: 255})
	got := Fit(src, 32, 32)
	if got.RGBAAt(16, 16).B != 255 {
		t.Error("a small image should be scaled up")
	}
}

func TestFitDegenerate(t *testing.T) {
	if got := Fit(solid(0, 0, color.RGBA{}), 16, 16); got.Bounds().Dx() != 16 {
		t.Error("a zero-sized source should still produce the target size")
	}
	if got := Fit(solid(8, 8, color.RGBA{}), 0, 0); got.Bounds().Dx() != 1 {
		t.Error("a zero-sized target should be clamped to one pixel")
	}
}

func TestTint(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	src.Set(1, 0, color.RGBA{R: 10, A: 0})
	out := Tint(src, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	if got := out.RGBAAt(0, 0); got.R != 255 || got.G != 0 || got.A != 255 {
		t.Errorf("opaque pixel = %+v", got)
	}
	if got := out.RGBAAt(1, 0); got.A != 0 {
		t.Errorf("transparent pixel = %+v", got)
	}
}

func TestLoaderCachesAndFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "icon.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, solid(32, 32, color.RGBA{G: 255, A: 255})); err != nil {
		t.Fatal(err)
	}
	f.Close()

	l := NewLoader()
	img, err := l.Load(path, 48)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if img.Bounds().Dx() != 48 {
		t.Errorf("scaled size = %v", img.Bounds())
	}
	again, err := l.Load(path, 48)
	if err != nil || again != img {
		t.Error("the second load should hit the cache")
	}

	broken := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(broken, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Load(broken, 48); err == nil {
		t.Error("a broken image should fail")
	}
	if _, err := l.Load(broken, 48); err == nil {
		t.Error("a cached failure should still report an error")
	}
	if _, err := l.Load(filepath.Join(dir, "missing.png"), 48); err == nil {
		t.Error("a missing file should fail")
	}
}

func TestLoaderConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "icon.png")
	f, _ := os.Create(path)
	png.Encode(f, solid(16, 16, color.RGBA{R: 255, A: 255}))
	f.Close()

	l := NewLoader()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := l.Load(path, 32); err != nil {
				t.Errorf("concurrent load: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestLoadIcon(t *testing.T) {
	dir := t.TempDir()
	icon := filepath.Join(dir, "abs.png")
	if err := os.WriteFile(icon, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := LoadIcon(icon, 48); !ok || got != icon {
		t.Errorf("absolute path = %q %v", got, ok)
	}
	if _, ok := LoadIcon("", 48); ok {
		t.Error("an empty key should not resolve")
	}
	if _, ok := LoadIcon(filepath.Join(dir, "missing.png"), 48); ok {
		t.Error("a missing absolute path should not resolve")
	}
	// A bare name may or may not exist; the call must simply not fail.
	if _, ok := LoadIcon("definitely-not-an-icon-xyz", 48); ok {
		t.Error("an unknown theme icon should not resolve")
	}
}

func TestHasImageExtension(t *testing.T) {
	for _, p := range []string{"a.PNG", "b.jpg", "c.jpeg", "d.gif", "e.svg", "f.xpm", "g.bmp", "h.ico", "i.webp"} {
		if !HasImageExtension(p) {
			t.Errorf("%s should be an image", p)
		}
	}
	for _, p := range []string{"a.txt", "noextension", "b.png.txt"} {
		if HasImageExtension(p) {
			t.Errorf("%s should not be an image", p)
		}
	}
}

func TestCounter(t *testing.T) {
	var c Counter
	if c.Value() != 0 {
		t.Error("a fresh counter should be zero")
	}
	c.Add()
	c.Add()
	if c.Value() != 2 {
		t.Errorf("value = %d", c.Value())
	}
}

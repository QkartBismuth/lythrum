package preview

import (
	"context"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func writePNGFile(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 5), 90, 255})
		}
	}
	raw, err := encodePNG(img)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPicturePreview(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	writePNGFile(t, path, 320, 200)

	service := NewService("")
	got := service.For(context.Background(), path, Options{Width: 128, Height: 128})
	if got.Kind != KindPhoto {
		t.Errorf("kind = %q, want photo", got.Kind)
	}
	if got.Missing {
		t.Fatal("a readable picture must produce a preview")
	}
	if got.Width != 128 || got.Height != 128 {
		t.Errorf("size = %dx%d, want 128x128", got.Width, got.Height)
	}
	if got.Image == nil || got.Image.Bounds().Dx() != 128 {
		t.Fatalf("image = %v", got.Image)
	}
}

func TestPreviewIsCached(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	writePNGFile(t, path, 64, 64)

	service := NewService("")
	first := service.For(context.Background(), path, Options{Width: 32, Height: 32})
	// A second call must not touch the file again, so it works even when the
	// file is gone.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	second := service.For(context.Background(), path, Options{Width: 32, Height: 32})
	if second.Image == nil {
		t.Fatal("the second call lost the cached picture")
	}
	if first.Image == nil || second.Image.Pix[0] != first.Image.Pix[0] {
		t.Error("the cached picture differs from the original")
	}
}

func TestDiskCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	path := filepath.Join(dir, "photo.png")
	writePNGFile(t, path, 48, 48)

	service := NewService(cache)
	if got := service.For(context.Background(), path, Options{Width: 24, Height: 24}); got.Missing {
		t.Fatal("the preview was not built")
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("nothing was written to the cache")
	}

	fresh := NewService(cache)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got := fresh.For(context.Background(), path, Options{Width: 24, Height: 24})
	if got.Image == nil {
		t.Fatal("a fresh service must reuse the cache on disk")
	}

	fresh.Clear()
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("Clear left %d files behind", len(entries))
	}
}

func TestMissingFileStillHasAPreview(t *testing.T) {
	service := NewService("")
	got := service.For(context.Background(), filepath.Join(t.TempDir(), "ghost.png"),
		Options{Width: 64, Height: 64, Label: "нет файла"})
	if got.Label != "нет файла" {
		t.Errorf("label = %q", got.Label)
	}
	if !got.Missing {
		t.Error("a missing file must be reported as missing")
	}
	if got.Kind != KindPhoto {
		t.Errorf("kind = %q", got.Kind)
	}
}

func TestUnknownKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("привет"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewService("")
	got := service.For(context.Background(), path, Options{Width: 64, Height: 64})
	if got.Kind != KindIcon {
		t.Errorf("kind = %q, want icon", got.Kind)
	}
	if got.Image != nil && !got.Missing {
		t.Error("a text file has no picture to show")
	}
}

func TestCacheKeyDependsOnSize(t *testing.T) {
	a := CacheKey("/tmp/x.png", Options{Width: 64, Height: 64})
	b := CacheKey("/tmp/x.png", Options{Width: 128, Height: 64})
	if a == b {
		t.Error("previews of different sizes must have different keys")
	}
	if CacheKey("/tmp/x.png", Options{Width: 64, Height: 64}) != a {
		t.Error("the key must be stable")
	}
}

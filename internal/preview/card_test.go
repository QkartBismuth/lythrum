package preview

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"lythrum/internal/core"
)

// 3.4 — классификация файлов по расширению.
func TestKindOf(t *testing.T) {
	cases := map[string]MediaKind{
		"photo.png":   KindImage,
		"photo.JPG":   KindImage,
		"anim.gif":    KindGIF,
		"clip.mp4":    KindVideo,
		"clip.mkv":    KindVideo,
		"song.mp3":    KindAudio,
		"voice.ogg":   KindAudio,
		"archive.zip": KindUnknown,
		"noext":       KindUnknown,
	}
	for name, want := range cases {
		if got := KindOf(name); got != want {
			t.Fatalf("KindOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// Генератор тестовых изображений.
func solidPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	path := filepath.Join(t.TempDir(), "img.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

// Карточка картинки: постер декодирован, ID уникальны, пропорции сохранены.
func TestOpenCardImage(t *testing.T) {
	path := solidPNG(t, 200, 100)
	c1, err := OpenCard(path, core.Rect{X: 0, Y: 0, W: 100, H: 100})
	if err != nil {
		t.Fatalf("OpenCard: %v", err)
	}
	if c1.Kind != KindImage || c1.Title != "img.png" {
		t.Fatalf("поля карточки: %+v", c1)
	}
	if c1.Poster == nil || c1.Poster.Bounds().Dx() != 200 {
		t.Fatal("постер не декодирован")
	}

	// PosterSize: вписывание в квадрат сохраняет пропорции 2:1.
	w, h := c1.PosterSize()
	if w != 100 || h != 50 {
		t.Fatalf("PosterSize = %v×%v, want 100×50", w, h)
	}

	c2, err := OpenCard(path, core.Rect{X: 0, Y: 0, W: 10, H: 10})
	if err != nil {
		t.Fatal(err)
	}
	if c1.ID == c2.ID {
		t.Fatal("ID карточек совпали")
	}
}

// Неизвестный тип — ошибка, не паника.
func TestOpenCardUnknown(t *testing.T) {
	if _, err := OpenCard("/tmp/foo.zip", core.Rect{}); err == nil {
		t.Fatal("ожидалась ошибка для unknown-медиа")
	}
}

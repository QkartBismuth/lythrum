package media

import (
	"context"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeGIF builds a small animated file so the tests do not need fixtures.
func writeGIF(t *testing.T, dir, name string, frames int) string {
	t.Helper()
	animation := &gif.GIF{}
	for i := 0; i < frames; i++ {
		palette := color.Palette{color.Transparent, color.RGBA{uint8(40 * i), 80, 160, 255}}
		img := image.NewPaletted(image.Rect(0, 0, 8, 6), palette)
		for y := 0; y < 6; y++ {
			for x := 0; x < 8; x++ {
				img.SetColorIndex(x, y, uint8((x+y+i)%2))
			}
		}
		animation.Image = append(animation.Image, img)
		animation.Delay = append(animation.Delay, 5)
	}
	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := gif.EncodeAll(file, animation); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenGIFPlaysEveryFrame(t *testing.T) {
	path := writeGIF(t, t.TempDir(), "animation.gif", 3)
	source, err := OpenGIF(path, Options{Loop: true})
	if err != nil {
		t.Fatalf("OpenGIF: %v", err)
	}
	defer source.Close()

	if !source.Animated() {
		t.Error("a three frame file must be animated")
	}
	if w, h := source.Size(); w != 8 || h != 6 {
		t.Errorf("size = %dx%d, want 8x6", w, h)
	}
	for i := 0; i < 3; i++ {
		frame, ok := source.Next()
		if !ok {
			t.Fatalf("frame %d is missing", i)
		}
		if frame.Image == nil || frame.Delay != 50*time.Millisecond {
			t.Errorf("frame %d = %+v", i, frame)
		}
	}
	// Looping restarts at the first frame.
	if _, ok := source.Next(); !ok {
		t.Fatal("a looping source must keep producing frames")
	}
}

func TestGIFWithoutLoopStops(t *testing.T) {
	path := writeGIF(t, t.TempDir(), "once.gif", 2)
	source, err := OpenGIF(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	for i := 0; i < 2; i++ {
		if _, ok := source.Next(); !ok {
			t.Fatalf("frame %d is missing", i)
		}
	}
	if _, ok := source.Next(); ok {
		t.Error("a source without looping must stop at the end")
	}
}

func TestGIFRescales(t *testing.T) {
	path := writeGIF(t, t.TempDir(), "small.gif", 1)
	source, err := OpenGIF(path, Options{Width: 32, Height: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if w, h := source.Size(); w != 32 || h != 24 {
		t.Errorf("resized to %dx%d, want 32x24", w, h)
	}
	frame, ok := source.Next()
	if !ok {
		t.Fatal("no frame after rescaling")
	}
	if frame.Image.Bounds().Dx() != 32 {
		t.Errorf("frame width = %d", frame.Image.Bounds().Dx())
	}
}

func TestProbeFindsKind(t *testing.T) {
	dir := t.TempDir()
	gifPath := writeGIF(t, dir, "probe.gif", 2)
	info := Probe(gifPath)
	if info.Kind != KindGIF {
		t.Errorf("kind = %q, want gif", info.Kind)
	}
	if info.Width != 8 || info.Height != 6 {
		t.Errorf("size = %dx%d, want 8x6", info.Width, info.Height)
	}
	if !info.Animated {
		t.Error("a two frame gif must be animated")
	}

	missing := Probe(filepath.Join(dir, "nothing.mp4"))
	if missing.Kind == KindStill {
		t.Errorf("a missing file must not be reported as a picture: %+v", missing)
	}
}

func TestOpenStillShowsOneFrame(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "solid.png")
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	if err := writePNG(path, img); err != nil {
		t.Fatal(err)
	}
	source, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer source.Close()
	if source.Animated() {
		t.Error("a png is not animated")
	}
	if _, ok := source.Next(); !ok {
		t.Fatal("a picture must yield one frame")
	}
	if _, ok := source.Next(); ok {
		t.Error("a picture has only one frame")
	}
}

func TestOpenUnknownFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.unknownext")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path, Options{}); err == nil {
		t.Fatal("an unknown file must not open")
	} else if !strings.Contains(err.Error(), "no decoder") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVideoArgs(t *testing.T) {
	args := VideoArgs("/tmp/clip.mp4", Options{Width: 640, Height: 480, FPS: 30})
	joined := strings.Join(args, " ")
	for _, want := range []string{"-i /tmp/clip.mp4", "scale=640:480", "fps=30", "pix_fmt rgba", "rawvideo", "pipe:1", "-an"} {
		if !strings.Contains(joined, want) {
			t.Errorf("arguments %q miss %q", joined, want)
		}
	}

	bare := strings.Join(VideoArgs("/tmp/clip.mp4", Options{}), " ")
	if !strings.Contains(bare, "fps=24") {
		t.Errorf("default frame rate missing: %q", bare)
	}
	if strings.Contains(bare, "scale=") {
		t.Errorf("no size was requested, so no scaling: %q", bare)
	}
}

func TestThrottle(t *testing.T) {
	path := writeGIF(t, t.TempDir(), "slow.gif", 4)
	source, err := OpenGIF(path, Options{Loop: true})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	throttle := NewThrottle(source, 10)
	if throttle.Interval() != 100*time.Millisecond {
		t.Errorf("interval = %v, want 100ms", throttle.Interval())
	}
	now := time.Now()
	if !throttle.Due(now) {
		t.Error("the first frame should be due immediately")
	}
	if _, ok := throttle.Take(now); !ok {
		t.Fatal("the first frame is missing")
	}
	if throttle.Due(now.Add(10 * time.Millisecond)) {
		t.Error("a frame is not due yet")
	}
	if _, ok := throttle.Take(now.Add(10 * time.Millisecond)); ok {
		t.Error("the throttle handed out a frame too early")
	}
	if _, ok := throttle.Take(now.Add(200 * time.Millisecond)); !ok {
		t.Error("a frame should be due after the interval")
	}
}

func TestInfoKindHelpers(t *testing.T) {
	if !KindVideo.IsKnown() {
		t.Error("video must be a known kind")
	}
	if KindUnread.IsKnown() {
		t.Error("unknown is not a kind the shell can show")
	}
}

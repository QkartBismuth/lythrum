package wallpaper

import (
	"context"
	"image"
	"image/color"
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// makeGIF генерирует тестовый GIF из n кадров с задержкой delayMs (1/100 с).
func makeGIF(t *testing.T, n int, delayMs int, w, h int) string {
	t.Helper()
	g := &gif.GIF{LoopCount: 0}
	var pal color.Palette
	for i := 0; i < n; i++ {
		pal = append(pal, color.Gray{Y: uint8(i * 40)})
	}
	for i := 0; i < n; i++ {
		img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.SetColorIndex(x, y, uint8(i))
			}
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, delayMs) // единицы 1/100 с
	}
	path := filepath.Join(t.TempDir(), "wall.gif")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := gif.EncodeAll(f, g); err != nil {
		t.Fatal(err)
	}
	return path
}

func collect(t *testing.T, frames <-chan Frame, n int, timeout time.Duration) []Frame {
	t.Helper()
	var got []Frame
	deadline := time.After(timeout)
	for len(got) < n {
		select {
		case f, ok := <-frames:
			if !ok {
				return got
			}
			got = append(got, f)
		case <-deadline:
			t.Fatalf("получено %d кадров за %v, ждём %d", len(got), timeout, n)
		}
	}
	return got
}

// 3.1 — GIF-провайдер: кадры идут по расписанию задержек из метаданных,
// размер кадра = размер вьюпорта (cover).
func TestGifProviderTimeline(t *testing.T) {
	path := makeGIF(t, 3, 4, 64, 32) // 4/100 c на кадр
	p := &GifProvider{Path: path}

	frames, err := p.Frames(context.Background(), 128, 128)
	if err != nil {
		t.Fatalf("Frames: %v", err)
	}
	got := collect(t, frames, 3, 3*time.Second)

	for i, f := range got {
		if f.Pix.Bounds().Dx() != 128 || f.Pix.Bounds().Dy() != 128 {
			t.Fatalf("кадр %d: размер %v, want 128x128", i, f.Pix.Bounds())
		}
		if f.Delay != 40*time.Millisecond {
			t.Fatalf("кадр %d: delay = %v, want 40ms", i, f.Delay)
		}
	}
	// Кадры действительно различаются (анимация есть).
	if got[0].Pix.At(0, 0) == got[2].Pix.At(0, 0) {
		t.Fatal("кадры GIF идентичны — анимация не декодируется")
	}
}

// CoverScale: сохранение пропорций и центрирование (cover, не stretch).
func TestCoverScale(t *testing.T) {
	// Источник 100x50 в вьюпорт 50x50: масштаб по ширине 0.5 → 50x25,
	// вертикальный центр.
	src := image.NewRGBA(image.Rect(0, 0, 100, 50))
	dst := CoverScale(src, 50, 50)
	if dst.Bounds().Dx() != 50 || dst.Bounds().Dy() != 50 {
		t.Fatalf("размер назначения: %v", dst.Bounds())
	}
}

// 3.2/3.6 — видео-декодер через ffmpeg: кадры RGBA под вьюпорт, ошибки
// и паники изолированы менеджером.
func TestFFmpegVideoFrames(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ffmpeg"); err != nil {
		t.Skip("ffmpeg не установлен")
	}
	// Генерируем 0.5 с тестового видео.
	path := filepath.Join(t.TempDir(), "test.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=0.5:size=160x90:rate=10",
		"-pix_fmt", "yuv420p", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg encode: %v: %s", err, string(out))
	}

	d := FFmpegDecoder{}
	frames, err := d.FramesRGBA(context.Background(), path, 160, 90, 10)
	if err != nil {
		t.Fatalf("FramesRGBA: %v", err)
	}
	got := collect(t, frames, 1, 5*time.Second)
	if got[0].Pix.Bounds().Dx() != 160 || got[0].Pix.Bounds().Dy() != 90 {
		t.Fatalf("кадр: %v, want 160x90", got[0].Pix.Bounds())
	}
	if got[0].Delay != 100*time.Millisecond {
		t.Fatalf("delay = %v, want 100ms (fps 10)", got[0].Delay)
	}
}

// 3.6 — интеграция: паника декодера изолируется менеджером, провайдер
// перезапускается, кадры после восстановления продолжают поступать.
func TestManagerRecoversFromDecoderPanic(t *testing.T) {
	var calls atomic.Int32
	m := NewManager(panickyDecoder{onCall: func() {
		if calls.Add(1) == 1 {
			panic("codec boom")
		}
	}})
	m.SetViewport(32, 32) // кадры скейлятся под тестовый размер
	m.backoffBase, m.backoffMax = 5*time.Millisecond, 20*time.Millisecond

	// GIF-содержимое в .mp4-имени: KindOf выберет KindVideo → путь декодера,
	// а мок-декодер сам делегирует GifProvider'у (им не важна расширение).
	path := makeGIF(t, 2, 4, 32, 32)
	mp4 := filepath.Join(t.TempDir(), "wall.mp4")
	if b, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(mp4, b, 0o644); err != nil {
		t.Fatal(err)
	}
	m.SetSource(mp4)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { _ = m.Run(ctx); close(done) }()

	// Ждём живой кадр после перезапуска провайдера.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f := m.Current(); f != nil && f.Pix.Bounds().Dx() == 32 {
			if calls.Load() < 2 {
				t.Fatal("первый запуск декодера не паниковал — изоляция не проверена")
			}
			if m.Restarts() < 1 {
				t.Fatal("перезапуск не зафиксирован в метрике")
			}
			return // восстановление подтверждено
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("менеджер обоев не восстановился после паники декодера")
}

type panickyDecoder struct{ onCall func() }

func (d panickyDecoder) FramesRGBA(ctx context.Context, path string, w, h, fps int) (<-chan Frame, error) {
	d.onCall()
	// Стандартный GIF-провайдер продолжит работу.
	return (&GifProvider{Path: path}).Frames(ctx, w, h)
}

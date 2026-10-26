// Package wallpaper — анимированные обои холста: GIF, видео (FFmpeg) и
// процедурные шейдеры.
//
// Архитектура изоляции: провайдер живёт в горутине менеджера и единственный
// побочный эффект — «последний кадр» под мьютексом (latest-wins). Паника или
// ошибка декодера приводит к перезапуску провайдера с backoff'ом и НИКОГДА
// не доходит до main loop: обои — расходный материал, холст — святое.
package wallpaper

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	xdraw "golang.org/x/image/draw"
)

// Kind — тип обоев.
type Kind string

const (
	KindNone   Kind = "none"
	KindImage  Kind = "image"  // статичная картинка
	KindGIF    Kind = "gif"    // циклический GIF
	KindVideo  Kind = "video"  // видео (FFmpeg)
	KindShader Kind = "shader" // процедурный шейдер
)

// KindOf классифицирует файл обоев по расширению.
func KindOf(path string) Kind {
	switch ext := filepath.Ext(path); ext {
	case ".gif":
		return KindGIF
	case ".mp4", ".mkv", ".webm", ".mov", ".avi":
		return KindVideo
	case ".png", ".jpg", ".jpeg", ".webp":
		return KindImage
	default:
		return KindNone
	}
}

// Frame — кадр фона: RGBA под размер вьюпорта и пауза до следующего кадра.
type Frame struct {
	Pix   *image.RGBA
	Delay time.Duration
}

// Provider — источник кадров обоев.
type Provider interface {
	Kind() Kind
	// Frames возвращает поток кадров до отмены ctx; реализация обязана
	// прекратить работу по закрытию ctx. Frame'ы уже отскейлены
	// под размер вьюпорта (cover).
	Frames(ctx context.Context, w, h int) (<-chan Frame, error)
}

// ── Масштабирование cover ────────────────────────────────────────────────

// CoverScale вписывает src в w×h «покрытием» (cover): сохраняет пропорции,
// центрирует, обрезает излишки. Возвращает RGBA нужного размера.
func CoverScale(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if src == nil || w <= 0 || h <= 0 {
		return dst
	}
	sb := src.Bounds()
	if sb.Dx() == 0 || sb.Dy() == 0 {
		return dst
	}

	// Коэффициент, при котором картинка закрывает весь вьюпорт.
	scale := math.Max(float64(w)/float64(sb.Dx()), float64(h)/float64(sb.Dy()))
	dw, dh := int(float64(sb.Dx())*scale+0.5), int(float64(sb.Dy())*scale+0.5)

	// Центрирование: источник рисуется с отрицательным смещением.
	xdraw.ApproxBiLinear.Scale(dst, image.Rect(0, 0, dw, dh), src, sb, xdraw.Src, nil)
	if dw == w && dh == h {
		return dst
	}
	// Переносим центр кадра в центр вьюпорта.
	shifted := image.NewRGBA(image.Rect(0, 0, w, h))
	dx, dy := (w-dw)/2, (h-dh)/2
	draw.Draw(shifted, image.Rect(dx, dy, dx+dw, dy+dh), dst, image.Point{X: 0, Y: 0}, draw.Src)
	return shifted
}

// ── Статичная картинка ──────────────────────────────────────────────────

// ImageProvider — статичные обои (один кадр).
type ImageProvider struct{ Path string }

func (p *ImageProvider) Kind() Kind { return KindImage }

func (p *ImageProvider) Frames(ctx context.Context, w, h int) (<-chan Frame, error) {
	img, err := decodeImage(p.Path)
	if err != nil {
		return nil, err
	}
	out := make(chan Frame, 1)
	go func() {
		defer close(out)
		select {
		case out <- Frame{Pix: CoverScale(img, w, h), Delay: 0}:
		case <-ctx.Done():
			return
		}
		<-ctx.Done() // держим канал до отмены: повторной отправки нет
	}()
	return out, nil
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	switch ext := filepath.Ext(path); ext {
	case ".png":
		return png.Decode(f)
	case ".jpg", ".jpeg":
		return jpeg.Decode(f)
	case ".gif":
		g, err := gif.Decode(f)
		return g, err
	default:
		img, _, err := image.Decode(f)
		return img, err
	}
}

// ── GIF ──────────────────────────────────────────────────────────────────

// GifProvider — циклические GIF-обои: кадры воспроизводятся по расписанию
// задержек из метаданных (единицы 1/100 с; нулевая задержка → 100 мс).
type GifProvider struct{ Path string }

func (p *GifProvider) Kind() Kind { return KindGIF }

func (p *GifProvider) Frames(ctx context.Context, w, h int) (<-chan Frame, error) {
	f, err := os.Open(p.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		return nil, err
	}
	if len(g.Image) == 0 {
		return nil, os.ErrInvalid
	}

	out := make(chan Frame, 2)
	go func() {
		defer close(out)
		for {
			for i := range g.Image {
				delay := gifDelay(g, i)
				// Отправка с учётом темпа: следующий кадр не раньше паузы.
				select {
				case out <- Frame{Pix: CoverScale(g.Image[i], w, h), Delay: delay}:
				case <-ctx.Done():
					return
				}
			}
			// GIF крутится бесконечно — это обои.
		}
	}()
	return out, nil
}

func gifDelay(g *gif.GIF, i int) time.Duration {
	d := time.Duration(g.Delay[i]) * 10 * time.Millisecond
	if d <= 0 {
		d = 100 * time.Millisecond
	}
	return d
}

// ── Видео ────────────────────────────────────────────────────────────────

// VideoDecoder — изолированный декодер видео. Реализация по умолчанию —
// exec ffmpeg (rawvideo в stdout); в Спринте 4 добавится cgo-вариант.
// Ошибки декодера обязаны остаться внутри провайдера (см. Manager).
type VideoDecoder interface {
	FramesRGBA(ctx context.Context, path string, w, h int, fps int) (<-chan Frame, error)
}

// VideoProvider — видео-обои через VideoDecoder.
type VideoProvider struct {
	Path    string
	FPS     int // целевой темп кадров рендера
	Decoder VideoDecoder
}

func (p *VideoProvider) Kind() Kind { return KindVideo }

func (p *VideoProvider) Frames(ctx context.Context, w, h int) (<-chan Frame, error) {
	fps := p.FPS
	if fps <= 0 {
		fps = 24
	}
	dec := p.Decoder
	if dec == nil {
		dec = FFmpegDecoder{}
	}
	return dec.FramesRGBA(ctx, p.Path, w, h, fps)
}

// FFmpegDecoder — декодер через внешний ffmpeg. Отдельный процесс = та же
// изоляция отказов, что и у HTML5-виджетов: крах кодека не роняет обои.
type FFmpegDecoder struct {
	Bin string // пусто → "ffmpeg"
}

func (d FFmpegDecoder) FramesRGBA(ctx context.Context, path string, w, h int, fps int) (<-chan Frame, error) {
	bin := d.Bin
	if bin == "" {
		bin = "ffmpeg"
	}
	delay := time.Second / time.Duration(fps)

	cmd := exec.Command(bin,
		"-i", path,
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"-vf", fmt.Sprintf("scale=%d:%d", w, h),
		"-r", strconv.Itoa(fps),
		"-",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	out := make(chan Frame, 2)
	go func() {
		defer close(out)
		defer func() {
			cmd.Process.Kill() // stdout закрыт — процесс завершаем
			cmd.Wait()
		}()

		frameBytes := w * h * 4
		buf := make([]byte, frameBytes)
		for {
			if !readFull(stdout, buf) {
				return // конец потока или ошибка чтения
			}
			pix := make([]byte, frameBytes)
			copy(pix, buf)
			frame := &image.RGBA{
				Pix:    pix,
				Stride: 4 * w,
				Rect:   image.Rect(0, 0, w, h),
			}
			select {
			case out <- Frame{Pix: frame, Delay: delay}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func readFull(r io.Reader, buf []byte) bool {
	n := 0
	for n < len(buf) {
		k, err := r.Read(buf[n:])
		n += k
		if err != nil {
			return false
		}
	}
	return true
}

// ── Процедурный шейдер (заготовка Спринта 3) ────────────────────────────

// ShaderProvider — простые процедурные обои: плазма, «дышащая» по времени.
// Служит и заглушкой WebGL-подобных шейдеров (GLSL-шейдеры придут с
// GPU-рендерером), и честным тестовым источником живых кадров.
type ShaderProvider struct{}

func (p *ShaderProvider) Kind() Kind { return KindShader }

func (p *ShaderProvider) Frames(ctx context.Context, w, h int) (<-chan Frame, error) {
	out := make(chan Frame, 2)
	go func() {
		defer close(out)
		tick := time.NewTicker(33 * time.Millisecond)
		defer tick.Stop()
		for t := 0.0; ; {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				t += 0.033
			}
			out <- Frame{Pix: plasmaFrame(t, w, h), Delay: 33 * time.Millisecond}
		}
	}()
	return out, nil
}

func plasmaFrame(t float64, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := math.Sin(float64(x)/24.0+t) +
				math.Sin(float64(y)/16.0-t*0.7) +
				math.Sin(float64(x+y)/32.0+t*0.5)
			c := uint8((v + 3) / 6 * 255) // [0..6] → [0..255]
			i := img.PixOffset(x, y)
			img.Pix[i+0] = c / 2
			img.Pix[i+1] = c / 3
			img.Pix[i+2] = c
			img.Pix[i+3] = 0xFF
		}
	}
	return img
}

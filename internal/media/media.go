// Package media turns pictures, animations and videos into a stream of frames
// the shell can paint. It covers three sources: still images, animated GIFs
// and videos, the last one through an ffmpeg process.
package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "image/jpeg"
	_ "image/png"
)

// ErrNoSource means the path is empty or has no known decoder.
var ErrNoSource = errors.New("media: no decoder for this file")

// Frame is one decoded picture, always in RGBA order.
type Frame struct {
	Image *image.RGBA
	Delay time.Duration
}

// Source produces frames until it runs out or is closed.
type Source interface {
	// Next returns the next frame, or false when the source is finished.
	Next() (Frame, bool)
	// Width and Height report the size of the frames.
	Size() (int, int)
	// Animated reports whether the source has more than one frame, which the
	// shell uses to decide if it has to keep painting.
	Animated() bool
	// Close releases the source.
	Close() error
}

// Info describes a file before it is played.
type Info struct {
	Path      string
	Kind      Kind
	Width     int
	Height    int
	Duration  time.Duration
	Animated  bool
	Audio     bool
	HasPoster bool
}

// Kind is what a file turned out to be.
type Kind string

// The kinds the shell knows.
const (
	KindStill  Kind = "still"
	KindGIF    Kind = "gif"
	KindVideo  Kind = "video"
	KindAudio  Kind = "audio"
	KindUnread Kind = "unknown"
)

// Classify guesses what a file is from its header and extension. It never
// starts a process, so a directory listing stays fast even when it walks a
// whole development tree.
func Classify(path string) Kind {
	if data, err := os.Open(path); err == nil {
		defer data.Close()
		header := make([]byte, 512)
		n, _ := data.Read(header)
		header = header[:n]
		switch {
		case bytes.HasPrefix(header, []byte("GIF8")):
			return KindGIF
		case bytes.HasPrefix(header, []byte("\xff\xd8\xff")), bytes.HasPrefix(header, []byte("\x89PNG")),
			bytes.HasPrefix(header, []byte("BM")):
			return KindStill
		case len(header) > 12 && string(header[4:8]) == "ftyp":
			return KindVideo
		case len(header) > 3 && string(header[:3]) == "ID3":
			return KindAudio
		case len(header) > 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE":
			return KindAudio
		case bytes.HasPrefix(header, []byte("\xff\xfb")), bytes.HasPrefix(header, []byte("\xff\xf3")),
			bytes.HasPrefix(header, []byte("\xff\xf2")), bytes.HasPrefix(header, []byte("OggS")),
			bytes.HasPrefix(header, []byte("fLaC")):
			return KindAudio
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".bmp", ".tif", ".tiff", ".avif", ".jxl":
		return KindStill
	case ".gif":
		return KindGIF
	case ".mp4", ".mkv", ".webm", ".mov", ".avi", ".m4v":
		return KindVideo
	case ".mp3", ".flac", ".ogg", ".opus", ".wav", ".m4a", ".aac":
		return KindAudio
	}
	return KindUnread
}

// Probe looks at a file and reports what it is, using ffprobe when it is
// installed and falling back to extensions and the file header.
func Probe(path string) Info {
	info := Info{Path: path, Kind: Classify(path)}
	if info.Kind == KindUnread {
		return info
	}

	if probe := ffprobe(path); probe != nil {
		info.Width, info.Height = probe.Width, probe.Height
		info.Duration = probe.Duration
		info.Audio = probe.Audio
		if probe.Video {
			if probe.Frames > 1 || probe.Duration > time.Second {
				info.Kind = KindVideo
			}
			info.Animated = true
		}
		if probe.HasArt {
			info.HasPoster = true
		}
	}

	switch info.Kind {
	case KindGIF:
		if g := gifDimensions(path); g != nil {
			info.Width, info.Height = g.width, g.height
			info.Animated = g.frames > 1
		}
	case KindStill:
		if file, err := openFile(path); err == nil {
			defer file.Close()
			if cfg, _, err := image.DecodeConfig(file); err == nil {
				info.Width, info.Height = cfg.Width, cfg.Height
			}
		}
	}
	return info
}

func openFile(path string) (*os.File, error) { return os.Open(path) }

type gifInfo struct {
	width, height int
	frames        int
}

func gifDimensions(path string) *gifInfo {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	animation, err := gif.DecodeAll(file)
	if err != nil || len(animation.Image) == 0 {
		return nil
	}
	return &gifInfo{width: animation.Config.Width, height: animation.Config.Height, frames: len(animation.Image)}
}

// Options shape the frames a source produces.
type Options struct {
	// Width and Height rescale the frames; zero keeps the original size.
	Width, Height int
	// FPS caps how fast a video is decoded.
	FPS int
	// Loop repeats animations forever.
	Loop bool
}

func (o Options) fps() int {
	if o.FPS <= 0 {
		return 24
	}
	if o.FPS > 60 {
		return 60
	}
	return o.FPS
}

// Open picks a source for the file.
func Open(ctx context.Context, path string, opts Options) (Source, error) {
	info := Probe(path)
	switch info.Kind {
	case KindStill:
		return openStill(path, opts)
	case KindGIF:
		return OpenGIF(path, opts)
	case KindVideo:
		return OpenVideo(ctx, path, opts)
	}
	return nil, fmt.Errorf("%w: %s", ErrNoSource, filepath.Base(path))
}

// still is a single picture.
type still struct {
	img *image.RGBA
}

func openStill(path string, opts Options) (Source, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoded, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	img := toRGBA(decoded, opts)
	return &still{img: img}, nil
}

func (s *still) Next() (Frame, bool) {
	if s.img == nil {
		return Frame{}, false
	}
	frame := Frame{Image: s.img}
	s.img = nil
	return frame, true
}

func (s *still) Size() (int, int) {
	if s.img == nil {
		return 0, 0
	}
	b := s.img.Bounds()
	return b.Dx(), b.Dy()
}

func (s *still) Animated() bool { return false }
func (s *still) Close() error   { return nil }

// toRGBA converts a picture and applies the requested size.
func toRGBA(src image.Image, opts Options) *image.RGBA {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if opts.Width > 0 && opts.Height > 0 && (opts.Width != width || opts.Height != height) {
		scaled := image.NewRGBA(image.Rect(0, 0, opts.Width, opts.Height))
		drawScale(scaled, src, bounds)
		return scaled
	}
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	copyRGBA(out, src, bounds)
	return out
}

func drawScale(dst *image.RGBA, src image.Image, bounds image.Rectangle) {
	dw, dh := dst.Bounds().Dx(), dst.Bounds().Dy()
	sw, sh := bounds.Dx(), bounds.Dy()
	for y := 0; y < dh; y++ {
		sy := bounds.Min.Y + y*sh/dh
		for x := 0; x < dw; x++ {
			sx := bounds.Min.X + x*sw/dw
			r, g, b, a := src.At(sx, sy).RGBA()
			i := (y*dst.Stride + x*4)
			dst.Pix[i+0] = uint8(r >> 8)
			dst.Pix[i+1] = uint8(g >> 8)
			dst.Pix[i+2] = uint8(b >> 8)
			dst.Pix[i+3] = uint8(a >> 8)
		}
	}
}

func copyRGBA(dst *image.RGBA, src image.Image, bounds image.Rectangle) {
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			r, g, b, a := src.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			i := y*dst.Stride + x*4
			dst.Pix[i+0] = uint8(r >> 8)
			dst.Pix[i+1] = uint8(g >> 8)
			dst.Pix[i+2] = uint8(b >> 8)
			dst.Pix[i+3] = uint8(a >> 8)
		}
	}
}

// GIFSource plays an animated GIF.
type GIFSource struct {
	frames []*image.RGBA
	delays []time.Duration
	mu     sync.Mutex
	index  int
	loop   bool
}

func gifs() *GIFSource { return &GIFSource{} }

// OpenGIF reads an animation into memory. GIF files are small, so keeping the
// frames avoids decoding the same file on every loop.
func OpenGIF(path string, opts Options) (Source, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	animation, err := gif.DecodeAll(file)
	if err != nil {
		return nil, err
	}
	if len(animation.Image) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoSource, filepath.Base(path))
	}
	src := gifs()
	src.loop = opts.Loop
	canvasBounds := animation.Image[0].Bounds()
	for i, frame := range animation.Image {
		// Composite over the previous frame, as the format expects.
		canvas := image.NewRGBA(canvasBounds)
		if i > 0 {
			copy(canvas.Pix, src.frames[i-1].Pix)
		}
		drawFrame(canvas, frame, canvasBounds)
		src.frames = append(src.frames, toRGBA(canvas, opts))
		delay := time.Duration(animation.Delay[i]) * 10 * time.Millisecond
		if delay <= 0 {
			delay = 100 * time.Millisecond
		}
		src.delays = append(src.delays, delay)
	}
	return src, nil
}

func drawFrame(dst *image.RGBA, frame *image.Paletted, bounds image.Rectangle) {
	bounds = bounds.Intersect(frame.Bounds())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			index := frame.ColorIndexAt(x, y)
			c := frame.Palette[index]
			cr, cg, cb, ca := c.RGBA()
			a := uint8(ca >> 8)
			if a == 0 {
				continue
			}
			r, g, b := uint8(cr>>8), uint8(cg>>8), uint8(cb>>8)
			i := (y-dst.Bounds().Min.Y)*dst.Stride + (x-dst.Bounds().Min.X)*4
			if a == 255 {
				dst.Pix[i+0], dst.Pix[i+1], dst.Pix[i+2] = r, g, b
				dst.Pix[i+3] = 255
				continue
			}
			mixPixel(dst, i, r, g, b, a)
		}
	}
}

func mixPixel(dst *image.RGBA, i int, r, g, b uint8, a uint8) {
	inv := 255 - int(a)
	dst.Pix[i+0] = uint8((int(r)*int(a) + int(dst.Pix[i+0])*inv) / 255)
	dst.Pix[i+1] = uint8((int(g)*int(a) + int(dst.Pix[i+1])*inv) / 255)
	dst.Pix[i+2] = uint8((int(b)*int(a) + int(dst.Pix[i+2])*inv) / 255)
	dst.Pix[i+3] = uint8(int(a) + (int(dst.Pix[i+3])*inv)/255)
}

// Next returns the next frame of the animation.
func (g *GIFSource) Next() (Frame, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.frames) == 0 {
		return Frame{}, false
	}
	if g.index >= len(g.frames) {
		if !g.loop {
			return Frame{}, false
		}
		g.index = 0
	}
	frame := Frame{Image: g.frames[g.index], Delay: g.delays[g.index]}
	g.index++
	return frame, true
}

// Seek restarts the animation.
func (g *GIFSource) Seek() {
	g.mu.Lock()
	g.index = 0
	g.mu.Unlock()
}

// Size reports the frame size.
func (g *GIFSource) Size() (int, int) {
	if len(g.frames) == 0 {
		return 0, 0
	}
	b := g.frames[0].Bounds()
	return b.Dx(), b.Dy()
}

// Animated reports whether the file has more than one frame.
func (g *GIFSource) Animated() bool { return len(g.frames) > 1 }

// Close frees the frames.
func (g *GIFSource) Close() error {
	g.mu.Lock()
	g.frames, g.delays = nil, nil
	g.mu.Unlock()
	return nil
}

// VideoArgs builds the ffmpeg command that decodes a file into raw frames.
func VideoArgs(path string, opts Options) []string {
	fps := opts.fps()
	width, height := opts.Width, opts.Height
	scale := ""
	if width > 0 && height > 0 {
		scale = fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,", width, height, width, height)
	}
	return []string{
		"-v", "error",
		"-i", path,
		"-an",
		"-vf", scale + "fps=" + itoa(fps),
		"-pix_fmt", "rgba",
		"-f", "rawvideo",
		"pipe:1",
	}
}

func itoa(v int) string { return fmt.Sprintf("%d", v) }

// VideoSource decodes a video with ffmpeg and hands out raw frames.
type VideoSource struct {
	cmd    *exec.Cmd
	stdout interface{ Read([]byte) (int, error) }
	width  int
	height int
	frame  []byte
	closed bool
	loop   bool
	mu     sync.Mutex
}

// OpenVideo starts ffmpeg on a file. The caller must Close it.
func OpenVideo(ctx context.Context, path string, opts Options) (Source, error) {
	width, height := opts.Width, opts.Height
	if width <= 0 || height <= 0 {
		info := Probe(path)
		width, height = info.Width, info.Height
		if width <= 0 || height <= 0 {
			return nil, fmt.Errorf("%w: cannot read the size of %s", ErrNoSource, filepath.Base(path))
		}
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", VideoArgs(path, opts)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &VideoSource{
		cmd:    cmd,
		stdout: stdout,
		width:  width,
		height: height,
		frame:  make([]byte, width*height*4),
		loop:   opts.Loop,
	}, nil
}

// Next decodes the following frame.
func (v *VideoSource) Next() (Frame, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return Frame{}, false
	}
	if _, err := readFull(v.stdout, v.frame); err != nil {
		return Frame{}, false
	}
	img := &image.RGBA{Pix: v.frame, Stride: v.width * 4, Rect: image.Rect(0, 0, v.width, v.height)}
	return Frame{Image: img}, true
}

func readFull(r interface{ Read([]byte) (int, error) }, buf []byte) (int, error) {
	read := 0
	for read < len(buf) {
		n, err := r.Read(buf[read:])
		read += n
		if err != nil {
			return read, err
		}
		if n == 0 {
			return read, io.EOF
		}
	}
	return read, nil
}

// Size reports the decoded frame size.
func (v *VideoSource) Size() (int, int) { return v.width, v.height }

// Animated reports that a video always has more frames.
func (v *VideoSource) Animated() bool { return true }

// Close stops ffmpeg.
func (v *VideoSource) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return nil
	}
	v.closed = true
	if v.cmd.Process != nil {
		_ = v.cmd.Process.Kill()
	}
	_ = v.cmd.Wait()
	return nil
}

// Poster returns a single frame of a video or animation, which the shell uses
// for file previews.
func Poster(ctx context.Context, path string, opts Options) (*image.RGBA, error) {
	source, err := Open(ctx, path, opts)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	frame, ok := source.Next()
	if !ok {
		return nil, fmt.Errorf("%w: no frame in %s", ErrNoSource, filepath.Base(path))
	}
	return frame.Image, nil
}

// Throttle keeps a source at a target frame rate.
type Throttle struct {
	source Source
	fps    int
	last   time.Time
}

// NewThrottle wraps a source with a frame rate limit.
func NewThrottle(source Source, fps int) *Throttle {
	if fps <= 0 {
		fps = 24
	}
	return &Throttle{source: source, fps: fps}
}

// Interval is the shortest time between two frames.
func (t *Throttle) Interval() time.Duration {
	return time.Duration(time.Second / time.Duration(t.fps))
}

// Due reports whether enough time passed for the next frame.
func (t *Throttle) Due(now time.Time) bool {
	return now.Sub(t.last) >= t.Interval()
}

// Take returns the next frame when it is time for one.
func (t *Throttle) Take(now time.Time) (Frame, bool) {
	if !t.Due(now) {
		return Frame{}, false
	}
	frame, ok := t.source.Next()
	if ok {
		t.last = now
	} else if t.source.Animated() && t.source != nil {
		if gifSource, isGIF := t.source.(*GIFSource); isGIF {
			gifSource.Seek()
			frame, ok = t.source.Next()
			t.last = now
		}
	}
	return frame, ok
}

// Source exposes the wrapped source.
func (t *Throttle) Source() Source { return t.source }

// Close closes the wrapped source.
func (t *Throttle) Close() error { return t.source.Close() }

// IsKnown reports whether the shell can show this kind.
func (k Kind) IsKnown() bool {
	switch k {
	case KindStill, KindGIF, KindVideo, KindAudio:
		return true
	}
	return false
}

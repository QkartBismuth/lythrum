// Package preview builds the small pictures the shell shows for files: a frame
// of a video, the cover of a song, the first page of a GIF, or a scaled photo.
// Results are cached on disk because producing them costs an ffmpeg run.
package preview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lythrum/internal/imagex"
	"lythrum/internal/media"
)

// Kind is what a preview shows.
type Kind string

// The kinds of preview the shell knows.
const (
	KindPhoto  Kind = "photo"
	KindVideo  Kind = "video"
	KindGIF    Kind = "gif"
	KindAudio  Kind = "audio"
	KindDoc    Kind = "document"
	KindIcon   Kind = "icon"
	KindBroken Kind = "broken"
)

// Preview is one ready to draw picture.
type Preview struct {
	Kind    Kind        `json:"kind"`
	Image   *image.RGBA `json:"-"`
	Width   int         `json:"width"`
	Height  int         `json:"height"`
	Label   string      `json:"label"`
	Source  string      `json:"source"`
	Missing bool        `json:"missing"`
}

// Options shape the requested picture.
type Options struct {
	// Width and Height are the size the shell wants.
	Width, Height int
	// Label is shown under the picture when it cannot be rendered.
	Label string
}

// Service produces previews with a cache in front of them.
type Service struct {
	dir    string
	mu     sync.Mutex
	memory map[string]*image.RGBA
	// ffmpeg is the binary used for video and audio, empty when missing.
	ffmpeg  string
	ffprobe string
}

// NewService prepares a preview service. An empty cacheDir keeps previews in
// memory only, which is what the tests use.
func NewService(cacheDir string) *Service {
	s := &Service{dir: cacheDir, memory: map[string]*image.RGBA{}}
	if s.dir != "" {
		_ = os.MkdirAll(s.dir, 0o755)
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		s.ffmpeg = path
	}
	if path, err := exec.LookPath("ffprobe"); err == nil {
		s.ffprobe = path
	}
	return s
}

// HasTools reports whether video previews can be produced.
func (s *Service) HasTools() bool { return s != nil && s.ffmpeg != "" }

// CacheKey identifies a preview by file and size.
func CacheKey(path string, opts Options) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%dx%d", path, opts.Width, opts.Height)))
	return hex.EncodeToString(sum[:])[:24]
}

// Cached returns a preview that is already built, without starting any work.
// The shell uses it while painting, because a slow ffmpeg run must not block
// the desktop.
func (s *Service) Cached(path string, opts Options) (*image.RGBA, bool) {
	if img := s.fromCache(path, opts); img != nil {
		return img, true
	}
	info := media.Probe(path)
	if img := s.fromDisk(path, opts, cacheKindOf(info.Kind)); img != nil {
		return img, true
	}
	return nil, false
}

// Produce builds a preview and stores it in the caches.
func (s *Service) Produce(ctx context.Context, path string, opts Options) *image.RGBA {
	info := media.Probe(path)
	img := s.render(ctx, path, info, opts)
	if img == nil {
		return nil
	}
	s.store(path, opts, img)
	return img
}

// For builds the preview of a file. It never fails: a file it cannot read
// still gets a labelled placeholder, which is what the tiles show.
func (s *Service) For(ctx context.Context, path string, opts Options) Preview {
	if opts.Width <= 0 {
		opts.Width = 256
	}
	if opts.Height <= 0 {
		opts.Height = 256
	}
	out := Preview{Source: path, Label: opts.Label, Width: opts.Width, Height: opts.Height}
	if out.Label == "" {
		out.Label = filepath.Base(path)
	}

	info := media.Probe(path)
	switch info.Kind {
	case media.KindStill:
		out.Kind = KindPhoto
	case media.KindGIF:
		out.Kind = KindGIF
	case media.KindVideo:
		out.Kind = KindVideo
	case media.KindAudio:
		out.Kind = KindAudio
	default:
		out.Kind = KindIcon
	}

	cacheKind := cacheKindOf(info.Kind)
	if img := s.fromCache(path, opts); img != nil {
		out.Image = img
		return out
	}
	if img := s.fromDisk(path, opts, cacheKind); img != nil {
		out.Image = img
		return out
	}

	img := s.render(ctx, path, info, opts)
	if img == nil {
		out.Missing = true
		return out
	}
	s.store(path, opts, img)
	out.Image = img
	return out
}

// render produces the picture for a file.
func (s *Service) render(ctx context.Context, path string, info media.Info, opts Options) *image.RGBA {
	switch info.Kind {
	case media.KindStill:
		return s.renderPicture(path, opts)
	case media.KindGIF:
		if img := s.fromDisk(path, opts, 0); img != nil {
			return img
		}
		return s.renderPicture(path, opts)
	case media.KindVideo:
		return s.renderVideo(ctx, path, opts)
	case media.KindAudio:
		return s.renderAudio(ctx, path, opts)
	}
	return nil
}

// renderPicture scales an ordinary picture.
func (s *Service) renderPicture(path string, opts Options) *image.RGBA {
	loader := imagex.NewLoader()
	img, err := loader.Load(path, opts.Width)
	if err != nil {
		return nil
	}
	return imagex.Fit(img, opts.Width, opts.Height)
}

// renderVideo grabs a frame with ffmpeg.
func (s *Service) renderVideo(ctx context.Context, path string, opts Options) *image.RGBA {
	if img := s.fromDisk(path, opts, 1); img != nil {
		return img
	}
	if !s.HasTools() {
		return nil
	}
	// One second in gives a frame that is not black.
	cmd := exec.CommandContext(ctx, s.ffmpeg,
		"-v", "error", "-ss", "1", "-i", path,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
			opts.Width, opts.Height, opts.Width, opts.Height),
		"-f", "image2pipe", "-vcodec", "png", "pipe:1")
	out, err := cmd.Output()
	if err != nil {
		// Some clips are shorter than the seek point.
		cmd = exec.CommandContext(ctx, s.ffmpeg,
			"-v", "error", "-i", path, "-frames:v", "1",
			"-vf", fmt.Sprintf("scale=%d:%d", opts.Width, opts.Height),
			"-f", "image2pipe", "-vcodec", "png", "pipe:1")
		out, err = cmd.Output()
		if err != nil {
			return nil
		}
	}
	img, err := decodePNG(out)
	if err != nil {
		return nil
	}
	return img
}

// renderAudio draws the cover of a song, or a placeholder with the title when
// the file carries no picture.
func (s *Service) renderAudio(ctx context.Context, path string, opts Options) *image.RGBA {
	if img := s.fromDisk(path, opts, 2); img != nil {
		return img
	}
	if !s.HasTools() {
		return nil
	}
	cmd := exec.CommandContext(ctx, s.ffmpeg,
		"-v", "error", "-i", path, "-an", "-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:%d", opts.Width, opts.Height),
		"-f", "image2pipe", "-vcodec", "png", "pipe:1")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	img, err := decodePNG(out)
	if err != nil {
		return nil
	}
	return img
}

// cacheKindOf maps a media kind onto the cache slot it uses.
func cacheKindOf(kind media.Kind) int {
	switch kind {
	case media.KindVideo:
		return 1
	case media.KindAudio:
		return 2
	}
	return 0
}

// cachePath is where a preview of this kind lives.
func (s *Service) cachePath(path string, opts Options, kind int) string {
	return filepath.Join(s.dir, fmt.Sprintf("%s-%d.png", CacheKey(path, opts), kind))
}

func (s *Service) fromDisk(path string, opts Options, kind int) *image.RGBA {
	if s.dir == "" {
		return nil
	}
	file := s.cachePath(path, opts, kind)
	info, err := os.Stat(file)
	if err != nil || info.Size() == 0 {
		return nil
	}
	if time.Since(info.ModTime()) > 30*24*time.Hour {
		return nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	img, err := decodePNG(raw)
	if err != nil {
		return nil
	}
	s.remember(CacheKey(path, opts), img)
	return img
}

func (s *Service) fromCache(path string, opts Options) *image.RGBA {
	key := CacheKey(path, opts)
	s.mu.Lock()
	defer s.mu.Unlock()
	img, ok := s.memory[key]
	if !ok {
		return nil
	}
	return img
}

func (s *Service) remember(key string, img *image.RGBA) {
	if img == nil {
		return
	}
	s.mu.Lock()
	if len(s.memory) > 64 {
		s.memory = map[string]*image.RGBA{}
	}
	s.memory[key] = img
	s.mu.Unlock()
}

func (s *Service) store(path string, opts Options, img *image.RGBA) {
	s.remember(CacheKey(path, opts), img)
	if s.dir == "" {
		return
	}
	kind := cacheKindOf(media.Probe(path).Kind)
	raw, err := encodePNG(img)
	if err != nil {
		return
	}
	_ = os.WriteFile(s.cachePath(path, opts, kind), raw, 0o644)
}

// Clear drops every cached preview.
func (s *Service) Clear() {
	s.mu.Lock()
	s.memory = map[string]*image.RGBA{}
	s.mu.Unlock()
	if s.dir == "" {
		return
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".png") {
			_ = os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
}

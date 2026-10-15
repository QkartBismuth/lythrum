package wm

import (
	"context"
	"image"
	"image/color"
	"os"
	"strings"
	"time"

	"lythrum/internal/config"
	"lythrum/internal/layout"
	"lythrum/internal/media"
)

// wallpapers decodes the desktop background. The picture itself belongs to the
// desktop layer, which paints it under the icons: a second window for the
// wallpaper could not be cleared when the desktop is panned, and the icons
// would leave their trails behind.
type wallpapers struct {
	wm       *WM
	surfaces []wallpaper
	ctx      context.Context
	cancel   context.CancelFunc
}

// wallpaper is one monitor worth of background.
type wallpaper struct {
	area     layout.Rect
	source   media.Source
	throttle *media.Throttle
	path     string
	dim      float64
	// static remembers a picture that only has to be uploaded once.
	static bool
	failed bool
}

func newWallpapers(w *WM) *wallpapers {
	ctx, cancel := context.WithCancel(context.Background())
	set := &wallpapers{wm: w, ctx: ctx, cancel: cancel}
	set.Reload()
	return set
}

// Reload rebuilds the sources after a configuration change.
func (s *wallpapers) Reload() {
	if s == nil || s.wm == nil {
		return
	}
	cfg := s.wm.cfg
	path := wallpaperPath(cfg)
	if path == s.currentPath() && len(s.surfaces) > 0 {
		s.applyDim(cfg)
		return
	}
	s.Close()
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err != nil {
		s.wm.log.Warn("no wallpaper", "path", path, "error", err)
		return
	}
	for i := range s.wm.monitors {
		m := s.wm.monitors[i]
		area := layout.Rect{X: m.X, Y: m.Y, W: m.W, H: m.H}
		surface, err := s.openSurface(path, area)
		if err != nil {
			s.wm.log.Warn("cannot open the wallpaper", "path", path, "error", err)
			return
		}
		s.surfaces = append(s.surfaces, *surface)
	}
	s.applyDim(cfg)
	s.Tick(true)
}

func (s *wallpapers) currentPath() string {
	if len(s.surfaces) == 0 {
		return ""
	}
	return s.surfaces[0].path
}

func (s *wallpapers) openSurface(path string, area layout.Rect) (*wallpaper, error) {
	opts := media.Options{
		Width: area.W,
		// A small buffer keeps video decoding cheap on a software machine.
		Height: area.H,
		FPS:    s.wm.cfg.WallpaperFPS,
		Loop:   true,
	}
	source, err := media.Open(s.ctx, path, opts)
	if err != nil {
		return nil, err
	}
	return &wallpaper{
		area:     area,
		source:   source,
		throttle: media.NewThrottle(source, opts.FPS),
		path:     path,
		static:   !source.Animated(),
	}, nil
}

func (s *wallpapers) applyDim(cfg config.Config) {
	for i := range s.surfaces {
		s.surfaces[i].dim = cfg.WallpaperDim
	}
}

// Active reports whether anything is still moving, which decides how fast the
// event loop has to wake up.
func (s *wallpapers) Active() bool {
	if s == nil {
		return false
	}
	for i := range s.surfaces {
		if !s.surfaces[i].static {
			return true
		}
	}
	return false
}

// Interval is the shortest wait between frames of an animated wallpaper.
func (s *wallpapers) Interval() time.Duration {
	if !s.Active() {
		return pollInterval
	}
	fps := s.wm.cfg.WallpaperFPS
	if fps <= 0 {
		fps = 12
	}
	d := time.Second / time.Duration(fps)
	if d < 16*time.Millisecond {
		d = 16 * time.Millisecond
	}
	return d
}

// Repaint hands the current picture to the desktop layer again, which is what
// the pannable desktop calls after the view has moved.
func (s *wallpapers) Repaint() {
	for i := range s.surfaces {
		s.surfaces[i].failed = false
	}
	s.Tick(true)
}

// Tick advances every wallpaper by one frame when it is time.
func (s *wallpapers) Tick(force bool) {
	if s == nil || len(s.surfaces) == 0 {
		return
	}
	now := time.Now()
	for i := range s.surfaces {
		surface := &s.surfaces[i]
		if surface.static {
			if !force || surface.failed {
				continue
			}
			frame, ok := surface.source.Next()
			if !ok {
				surface.failed = true
				continue
			}
			s.paint(surface, frame.Image)
			continue
		}
		if !force && !surface.throttle.Due(now) {
			continue
		}
		frame, ok := surface.throttle.Take(now)
		if !ok {
			continue
		}
		s.paint(surface, frame.Image)
	}
	if !s.Active() {
		s.wm.d.Flush()
	}
}

// paint dims the frame and hands it to the desktop layer, which draws it under
// the icons of that monitor.
func (s *wallpapers) paint(surface *wallpaper, img *image.RGBA) {
	if img == nil || s.wm.desktop == nil {
		return
	}
	if surface.dim > 0 {
		img = dimImage(img, surface.dim)
	}
	s.wm.desktop.SetWallpaper(surface.area, img)
}

// mod returns a non-negative remainder, which is what a tiled offset needs.
func mod(a, b int) int {
	if b == 0 {
		return 0
	}
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

// Shutdown stops the wallpaper players for good.
func (s *wallpapers) Shutdown() {
	s.Close()
	if s != nil && s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// Close releases every source and window but keeps the context, so a reload
// can open new sources.
func (s *wallpapers) Close() {
	if s == nil {
		return
	}
	for i := range s.surfaces {
		if surface := &s.surfaces[i]; surface.source != nil {
			_ = surface.source.Close()
		}
	}
	s.surfaces = nil
}

// wallpaperBackdrop fills the parts of a picture that are not transparent, and
// wallpaperBackdropPixel is the plain colour of a desktop without a wallpaper.
var wallpaperBackdrop = color.RGBA{R: 0x10, G: 0x10, B: 0x14, A: 0xff}

const wallpaperBackdropPixel = 0x00101014

// dimImage darkens a picture: zero leaves it alone, one turns it black. The
// setting is read the same way everywhere else, where wallpaper_dim is the
// share of the light that is taken away, not the share that is left.
func dimImage(src *image.RGBA, factor float64) *image.RGBA {
	if factor <= 0 {
		return src
	}
	if factor >= 1 {
		factor = 1
	}
	out := image.NewRGBA(src.Bounds())
	keep := uint8(float64(255) * (1 - factor))
	for i := 0; i+3 < len(src.Pix); i += 4 {
		out.Pix[i+0] = uint8(int(src.Pix[i+0]) * int(keep) / 255)
		out.Pix[i+1] = uint8(int(src.Pix[i+1]) * int(keep) / 255)
		out.Pix[i+2] = uint8(int(src.Pix[i+2]) * int(keep) / 255)
		out.Pix[i+3] = 255
	}
	return out
}

// wallpaperPath returns the file the shell should use as the background.
func wallpaperPath(cfg config.Config) string {
	if !cfg.WallpaperEnabled {
		return ""
	}
	path := strings.TrimSpace(cfg.WallpaperImage)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = home + path[1:]
		}
	}
	return path
}

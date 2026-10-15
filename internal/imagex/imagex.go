// Package imagex loads and scales the icons the start screen shows.
package imagex

import (
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// MaxSourceSize bounds how large an icon file may be before it is ignored, so
// a stray 50-megapixel PNG on the desktop cannot stall the window manager.
const MaxSourceSize = 32 << 20

// Extensions are the image formats that can be decoded.
var Extensions = []string{".png", ".xpm", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".ico"}

// HasImageExtension reports whether a path looks like an image file.
func HasImageExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, want := range Extensions {
		if ext == want {
			return true
		}
	}
	return false
}

// LoadIcon resolves an Icon= key to a file on disk. Absolute paths are used
// directly; a bare theme name is looked up in the user's icon directories.
// The second result reports whether the file exists.
func LoadIcon(key string, size int) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	if filepath.IsAbs(key) {
		if isFile(key) {
			return key, true
		}
		return "", false
	}

	// A bare name is an icon theme lookup; cover the common locations
	// without pulling in a full icon-theme loader.
	names := iconNames(key, size)
	var dirs []string
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		dirs = append(dirs, filepath.Join(dataHome, "icons"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".icons"))
	}
	for _, base := range []string{"/usr/share/pixmaps", "/usr/share/icons", "/usr/local/share/pixmaps"} {
		dirs = append(dirs, base)
	}
	for _, dir := range dirs {
		for _, name := range names {
			candidate := filepath.Join(dir, name)
			if isFile(candidate) {
				return candidate, true
			}
			if strings.HasSuffix(dir, "/pixmaps") {
				if isFile(candidate + ".png") {
					return candidate + ".png", true
				}
			}
		}
	}
	return "", false
}

// iconNames expands "firefox" into the file names an icon theme would use.
func iconNames(key string, size int) []string {
	out := []string{
		key + ".png",
		key + ".svg",
		key + ".xpm",
	}
	if size > 0 {
		for _, suffix := range []string{"48x48", "64x64", "128x128", "256x256"} {
			out = append(out, key+"_"+suffix+".png")
		}
	}
	return out
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Loader decodes and scales images, caching the results by path and size.
type Loader struct {
	mu    sync.Mutex
	cache map[string]*image.RGBA
	// inflight deduplicates concurrent loads of the same icon.
	inflight map[string]*sync.WaitGroup
}

// NewLoader creates an empty cache.
func NewLoader() *Loader {
	return &Loader{cache: make(map[string]*image.RGBA), inflight: make(map[string]*sync.WaitGroup)}
}

// Load returns the image at path scaled to fit a size×size box, preserving
// the aspect ratio. Results are cached; a failed load is cached as nil so a
// broken file is not re-read on every repaint.
func (l *Loader) Load(path string, size int) (*image.RGBA, error) {
	if path == "" || size < 1 {
		return nil, errors.New("imagex: nothing to load")
	}
	key := cacheKey(path, size)

	l.mu.Lock()
	if img, ok := l.cache[key]; ok {
		l.mu.Unlock()
		if img == nil {
			return nil, errors.New("imagex: " + path + " cannot be decoded")
		}
		return img, nil
	}
	if wg, ok := l.inflight[key]; ok {
		l.mu.Unlock()
		wg.Wait()
		l.mu.Lock()
		img := l.cache[key]
		l.mu.Unlock()
		if img == nil {
			return nil, errors.New("imagex: " + path + " cannot be decoded")
		}
		return img, nil
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	l.inflight[key] = wg
	l.mu.Unlock()

	img, err := loadFile(path, size)

	l.mu.Lock()
	l.cache[key] = img
	delete(l.inflight, key)
	l.mu.Unlock()
	wg.Done()

	if err != nil {
		return nil, err
	}
	return img, nil
}

func cacheKey(path string, size int) string {
	return path + "@" + itoa(size)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func loadFile(path string, size int) (*image.RGBA, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxSourceSize {
		return nil, errors.New("imagex: " + path + " is too large")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return Fit(src, size, size), nil
}

// Fit scales an image into a w×h box, keeping the aspect ratio and centring
// the result on a transparent background.
func Fit(src image.Image, w, h int) *image.RGBA {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw < 1 || sh < 1 {
		return dst
	}

	scale := min(float64(w)/float64(sw), float64(h)/float64(sh))
	tw := int(float64(sw) * scale)
	th := int(float64(sh) * scale)
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	if tw > w {
		tw = w
	}
	if th > h {
		th = h
	}
	offX := (w - tw) / 2
	offY := (h - th) / 2

	for y := 0; y < th; y++ {
		sy := src.Bounds().Min.Y + y*sh/th
		for x := 0; x < tw; x++ {
			sx := src.Bounds().Min.X + x*sw/tw
			r, g, b, a := src.At(sx, sy).RGBA()
			if a == 0 {
				continue
			}
			setPixel(dst, offX+x, offY+y, uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8))
		}
	}
	return dst
}

func setPixel(img *image.RGBA, x, y int, r, g, b, a uint8) {
	if !(image.Point{x, y}.In(img.Bounds())) {
		return
	}
	i := img.PixOffset(x, y)
	img.Pix[i] = r
	img.Pix[i+1] = g
	img.Pix[i+2] = b
	img.Pix[i+3] = a
}

// Tint returns the image with every opaque pixel replaced by c, which is how
// the start screen renders a glyph without needing an icon file.
func Tint(src *image.RGBA, c color.RGBA) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	for i := 0; i < len(src.Pix); i += 4 {
		a := src.Pix[i+3]
		if a == 0 {
			continue
		}
		out.Pix[i] = c.R
		out.Pix[i+1] = c.G
		out.Pix[i+2] = c.B
		out.Pix[i+3] = a
	}
	return out
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Counter is a tiny atomic counter used to pace background work.
type Counter struct{ n atomic.Uint64 }

// Add increases the counter and returns the new value.
func (c *Counter) Add() uint64 { return c.n.Add(1) }

// Value returns the current count.
func (c *Counter) Value() uint64 { return c.n.Load() }

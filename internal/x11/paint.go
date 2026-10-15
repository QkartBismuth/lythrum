package x11

/*
#cgo pkg-config: x11 xft
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>

static GC ly_create_gc(Display *dpy, Drawable d) {
	return XCreateGC(dpy, d, 0, NULL);
}

static void ly_free_gc(Display *dpy, GC gc) { XFreeGC(dpy, gc); }

static void ly_fill_triangle(Display *dpy, Drawable dst, GC gc,
                             int x0, int y0, int x1, int y1, int x2, int y2) {
	XPoint pts[3];
	pts[0].x = (short)x0; pts[0].y = (short)y0;
	pts[1].x = (short)x1; pts[1].y = (short)y1;
	pts[2].x = (short)x2; pts[2].y = (short)y2;
	XFillPolygon(dpy, dst, gc, pts, 3, Complex, CoordModeOrigin);
}

static void ly_fill_quad(Display *dpy, Drawable dst, GC gc,
                         int x0, int y0, int x1, int y1,
                         int x2, int y2, int x3, int y3) {
	XPoint pts[4];
	pts[0].x = (short)x0; pts[0].y = (short)y0;
	pts[1].x = (short)x1; pts[1].y = (short)y1;
	pts[2].x = (short)x2; pts[2].y = (short)y2;
	pts[3].x = (short)x3; pts[3].y = (short)y3;
	XFillPolygon(dpy, dst, gc, pts, 4, Complex, CoordModeOrigin);
}

// ly_put_rgba uploads a BGRX image. The pixels are copied into memory Xlib
// owns so that no Go allocation is ever handed to Xlib for longer than the
// call.
// ly_get_image reads pixels back from a drawable into a caller-provided
// BGRX buffer.
static int ly_get_image(Display *dpy, Drawable src, int x, int y, int w, int h,
                        unsigned char *out) {
	XImage *img = XGetImage(dpy, src, x, y, (unsigned int)w, (unsigned int)h,
	                        AllPlanes, ZPixmap);
	int i, count = w * h;
	if (!img)
		return 0;
	for (i = 0; i < count; i++) {
		unsigned long pixel = XGetPixel(img, i % w, i / w);
		out[i * 4 + 0] = (unsigned char)(pixel & 0xff);
		out[i * 4 + 1] = (unsigned char)((pixel >> 8) & 0xff);
		out[i * 4 + 2] = (unsigned char)((pixel >> 16) & 0xff);
		out[i * 4 + 3] = 0;
	}
	XDestroyImage(img);
	return 1;
}

// ly_capture_error is installed around the upload so the reason a server
// refuses an image can be reported back to Go.
static int ly_last_error = 0;

static int ly_capture_error(Display *dpy, XErrorEvent *e) {
	ly_last_error = e->error_code;
	return 0;
}

static int ly_put_rgba(Display *dpy, Drawable dst, GC gc, unsigned char *src,
                       int w, int h, int dx, int dy) {
	size_t bytes = (size_t)w * h * 4;
	unsigned char *buf = (unsigned char *)malloc(bytes);
	XImage *img;
	int rc;

	if (!buf)
		return -1;
	memcpy(buf, src, bytes);
	img = XCreateImage(dpy, DefaultVisual(dpy, DefaultScreen(dpy)),
	                   DefaultDepth(dpy, DefaultScreen(dpy)), ZPixmap, 0,
	                   (char *)buf, (unsigned int)w, (unsigned int)h, 32, 0);
	if (!img) {
		free(buf);
		return -2;
	}
	// XPutImage reports 0 even when the pixels arrive on some Xlib builds, so
	// the X protocol error is the only reliable failure signal. The slot has to
	// be cleared first: an error from an earlier request would otherwise make
	// every later upload look like a failure.
	XSetErrorHandler(ly_capture_error);
	ly_last_error = 0;
	rc = XPutImage(dpy, dst, gc, img, 0, 0, dx, dy, (unsigned int)w, (unsigned int)h);
	XSync(dpy, False);
	XDestroyImage(img);
	(void)rc;
	return ly_last_error ? -ly_last_error : 0;
}
*/
import "C"

import (
	"fmt"
	"image"
	"image/color"
	"unsafe"
)

// GC is a graphics context: the drawing state Xlib keeps for rectangles and
// text. It wraps a C pointer without exposing cgo types to callers.
type GC struct{ p unsafe.Pointer }

// Valid reports whether the context is usable.
func (g GC) Valid() bool { return g.p != nil }

func (g GC) c() C.GC { return C.GC(g.p) }

// Pixmap is an off-screen drawable, used to cache icons between repaints.
type Pixmap uint32

// CreateGC creates a graphics context for a drawable.
func (d *Display) CreateGC(w Window) (GC, error) {
	if d.dpy == nil {
		return GC{}, errClosed
	}
	gc := C.ly_create_gc(d.dpy, C.Drawable(w))
	if gc == nil {
		return GC{}, ErrBadDrawable
	}
	return GC{p: unsafe.Pointer(gc)}, nil
}

// FreeGC releases a graphics context.
func (d *Display) FreeGC(g GC) {
	if d.dpy != nil && g.Valid() {
		C.ly_free_gc(d.dpy, g.c())
	}
}

// SetForegroundOf sets the foreground colour of a graphics context.
func (d *Display) SetForegroundOf(g GC, pixel uint32) {
	if d.dpy != nil && g.Valid() {
		C.XSetForeground(d.dpy, g.c(), C.ulong(pixel))
	}
}

// SetBackgroundOf sets the background colour used by clearing operations.
func (d *Display) SetBackgroundOf(g GC, pixel uint32) {
	if d.dpy != nil && g.Valid() {
		C.XSetBackground(d.dpy, g.c(), C.ulong(pixel))
	}
}

// FillRectangle paints an opaque rectangle.
func (d *Display) FillRectangle(w Window, g GC, x, y, width, height int) {
	if d.dpy == nil || !g.Valid() || width <= 0 || height <= 0 {
		return
	}
	C.XFillRectangle(d.dpy, C.Drawable(w), g.c(),
		C.int(x), C.int(y), C.uint(width), C.uint(height))
}

// DrawRectangle strokes the outline of a rectangle.
func (d *Display) DrawRectangle(w Window, g GC, x, y, width, height int) {
	if d.dpy == nil || !g.Valid() {
		return
	}
	C.XDrawRectangle(d.dpy, C.Drawable(w), g.c(),
		C.int(x), C.int(y), C.uint(width-1), C.uint(height-1))
}

// FillPolygon fills a polygon given as absolute coordinates.
func (d *Display) FillPolygon(w Window, g GC, xs, ys []int) {
	n := len(xs)
	if d.dpy == nil || !g.Valid() || n < 3 || n != len(ys) || n > 3 {
		return
	}
	switch n {
	case 3:
		C.ly_fill_triangle(d.dpy, C.Drawable(w), g.c(),
			C.int(xs[0]), C.int(ys[0]), C.int(xs[1]), C.int(ys[1]), C.int(xs[2]), C.int(ys[2]))
	case 4:
		C.ly_fill_quad(d.dpy, C.Drawable(w), g.c(),
			C.int(xs[0]), C.int(ys[0]), C.int(xs[1]), C.int(ys[1]),
			C.int(xs[2]), C.int(ys[2]), C.int(xs[3]), C.int(ys[3]))
	}
}

// Flush sends all buffered drawing to the server.
func (d *Display) FlushDraw() { d.Flush() }

// CreatePixmap allocates an off-screen drawable of the given size.
func (d *Display) CreatePixmap(w, h int) (Pixmap, error) {
	if d.dpy == nil {
		return 0, errClosed
	}
	if w < 1 || h < 1 {
		return 0, ErrBadValue
	}
	pix := C.XCreatePixmap(d.dpy, C.Drawable(d.Root()), C.uint(w), C.uint(h), C.uint(d.Depth()))
	if pix == 0 {
		return 0, ErrBadDrawable
	}
	return Pixmap(uint32(pix)), nil
}

// FreePixmap releases an off-screen drawable.
func (d *Display) FreePixmap(p Pixmap) {
	if d.dpy != nil && p != 0 {
		C.XFreePixmap(d.dpy, C.Drawable(p))
	}
}

// CopyPixmap draws an off-screen drawable into a window.
func (d *Display) CopyPixmap(dst Window, g GC, src Pixmap, srcX, srcY, dstX, dstY, w, h int) {
	if d.dpy == nil || !g.Valid() || src == 0 || w <= 0 || h <= 0 {
		return
	}
	C.XCopyArea(d.dpy, C.Drawable(src), C.Drawable(dst), g.c(),
		C.int(srcX), C.int(srcY), C.uint(w), C.uint(h), C.int(dstX), C.int(dstY))
}

// UploadImage transfers an RGBA image to a drawable. X servers with 24-bit
// TrueColor visuals want 32 bits per pixel in BGRX order, which is what this
// conversion produces. Transparent pixels become black, so use
// UploadImageOver when the destination has its own colour underneath.
func (d *Display) UploadImage(dst Window, g GC, img *image.RGBA, x, y int) error {
	return d.UploadImageOver(dst, g, img, x, y, color.RGBA{A: 0})
}

// UploadImageOver draws an image onto a background colour, which is what the
// start screen needs for icons with transparent corners.
func (d *Display) UploadImageOver(dst Window, g GC, img *image.RGBA, x, y int, bg color.RGBA) error {
	if d.dpy == nil || !g.Valid() {
		return errClosed
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return ErrBadValue
	}
	if d.Depth() != 24 && d.Depth() != 32 {
		return fmt.Errorf("x11: cannot upload images with depth %d", d.Depth())
	}
	// The whole image travels in one request, so it cannot exceed the size
	// the server accepts.
	if width*height*4 > d.MaxRequestBytes()-4096 {
		return fmt.Errorf("x11: image %dx%d does not fit into a request", width, height)
	}
	buf := bgrxPixelsOver(img, bg)
	var pixels *C.uchar
	if len(buf) > 0 {
		pixels = (*C.uchar)(unsafe.Pointer(&buf[0]))
	}
	rc := C.ly_put_rgba(d.dpy, C.Drawable(dst), g.c(),
		pixels, C.int(width), C.int(height),
		C.int(x), C.int(y))
	switch {
	case rc == 0:
		return nil
	case rc == -1:
		return ErrBadAlloc
	default:
		name := errorNames[byte(-rc)]
		if name == "" {
			name = "unknown error"
		}
		return fmt.Errorf("x11: the server refused the image: %s", name)
	}
}

// bgrxPixelsOver converts RGBA into the BGRX layout TrueColor visuals expect,
// compositing the image onto bg so transparent corners keep the colour that
// was painted underneath.
func bgrxPixelsOver(img *image.RGBA, bg color.RGBA) []byte {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	out := make([]byte, w*h*4)
	i := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+bounds.Dx()*4]
		for x := 0; x < w; x++ {
			r, g, b, a := row[x*4], row[x*4+1], row[x*4+2], row[x*4+3]
			if a != 0xff {
				inv := 255 - int(a)
				r = uint8((int(r)*int(a) + int(bg.R)*inv) / 255)
				g = uint8((int(g)*int(a) + int(bg.G)*inv) / 255)
				b = uint8((int(b)*int(a) + int(bg.B)*inv) / 255)
			}
			out[i] = b
			out[i+1] = g
			out[i+2] = r
			out[i+3] = 0
			i += 4
		}
	}
	return out
}

// SolidImage builds a rectangle filled with one colour, which is how tile
// backgrounds are cached.
func SolidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x < w; x++ {
			row[x*4] = c.R
			row[x*4+1] = c.G
			row[x*4+2] = c.B
			row[x*4+3] = 0xff
		}
	}
	return img
}

// Capture reads a rectangle of a drawable into an RGBA image. It is what the
// screenshot helper uses to capture the screen.
func (d *Display) Capture(w Window, x, y, width, height int) (*image.RGBA, error) {
	if d.dpy == nil {
		return nil, errClosed
	}
	if width < 1 || height < 1 {
		return nil, ErrBadValue
	}
	buf := make([]C.uchar, width*height*4)
	ptr := (*C.uchar)(unsafe.Pointer(&buf[0]))
	rc := C.ly_get_image(d.dpy, C.Drawable(w), C.int(x), C.int(y),
		C.int(width), C.int(height), ptr)
	if rc == 0 {
		return nil, ErrBadDrawable
	}
	return rgbaFromBGRX(buf, width, height), nil
}

// rgbaFromBGRX turns the 32-bit little-endian BGRX data XGetImage returns into
// an RGBA image.
func rgbaFromBGRX(buf []C.uchar, width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		img.Pix[i*4+0] = uint8(buf[i*4+2])
		img.Pix[i*4+1] = uint8(buf[i*4+1])
		img.Pix[i*4+2] = uint8(buf[i*4+0])
		img.Pix[i*4+3] = 0xff
	}
	return img
}

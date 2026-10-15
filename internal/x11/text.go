package x11

import (
	"fmt"
	"image/color"
	"strings"
	"unsafe"
)

/*
#cgo pkg-config: x11 xft freetype2 fontconfig
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xft/Xft.h>
#include <ft2build.h>
#include FT_FREETYPE_H

// ly_font_open returns an Xft font for drawing, or NULL when fontconfig
// cannot resolve the pattern.
static XftFont *ly_font_open(Display *dpy, int screen, const char *pattern) {
	return XftFontOpenName(dpy, screen, pattern);
}

static void ly_font_close(Display *dpy, XftFont *font) {
	XftFontClose(dpy, font);
}

static XftDraw *ly_font_draw(Display *dpy, Drawable d, XftFont *font) {
	return XftDrawCreate(dpy, d, DefaultVisual(dpy, DefaultScreen(dpy)),
	                     DefaultColormap(dpy, DefaultScreen(dpy)));
}

static void ly_font_draw_free(Display *dpy, XftDraw *draw) {
	XftDrawDestroy(draw);
}

// ly_font_draw draws UTF-8 text with the font and colour given per call,
// which is what this Xft version exposes.
static void ly_font_text(XftDraw *draw, XftColor *color, XftFont *font,
                         const char *utf8, int x, int y) {
	XftDrawStringUtf8(draw, color, font, x, y, (const FcChar8 *)utf8, (int)strlen(utf8));
}

// ly_font_alloc_color turns 8-bit components into the 16-bit values Xft wants.
static void ly_font_alloc_color(Display *dpy, unsigned char r, unsigned char g,
                                unsigned char b, XftColor *out) {
	XRenderColor color;
	color.red = (unsigned short)(r * 257);
	color.green = (unsigned short)(g * 257);
	color.blue = (unsigned short)(b * 257);
	color.alpha = 0xffff;
	out->pixel = 0;
	XftColorAllocValue(dpy, DefaultVisual(dpy, DefaultScreen(dpy)),
	                   DefaultColormap(dpy, DefaultScreen(dpy)), &color, out);
}

// ly_font_face opens the same pattern through fontconfig and FreeType so that
// text can be measured with the very metrics it will be drawn with.
// A FreeType library outlives the faces opened from it, so it is created once
// and never torn down while a font is still open.
static FT_Library ly_ft = NULL;

static void ly_ft_init(void) {
	if (!ly_ft)
		FT_Init_FreeType(&ly_ft);
}

static FT_Face ly_font_face(const char *pattern) {
	FcPattern *pat = FcNameParse((const FcChar8 *)pattern);
	FcPattern *match;
	FcResult result;
	FT_Face face = NULL;
	FcChar8 *file = NULL;
	int index = 0;

	if (!pat)
		return NULL;
	FcConfigSubstitute(NULL, pat, FcMatchPattern);
	FcDefaultSubstitute(pat);
	match = FcFontMatch(NULL, pat, &result);
	if (!match) {
		FcPatternDestroy(pat);
		return NULL;
	}
	ly_ft_init();
	if (FcPatternGetString(match, FC_FILE, 0, &file) == FcResultMatch) {
		if (FcPatternGetInteger(match, FC_INDEX, 0, &index) != FcResultMatch)
			index = 0;
		if (!ly_ft || FT_New_Face(ly_ft, (const char *)file, index, &face) != 0)
			face = NULL;
	}
	FcPatternDestroy(match);
	FcPatternDestroy(pat);
	return face;
}

// ly_next_utf8 reads one character of a UTF-8 string and moves the cursor
// past it. Looking up every byte on its own would measure a two byte Cyrillic
// letter twice, which made every label with non ASCII text look far too wide.
static FT_ULong ly_next_utf8(const unsigned char **p) {
	const unsigned char *s = *p;
	unsigned char c0 = s[0];
	unsigned char c1 = s[1];
	unsigned char c2 = s[2];
	FT_ULong code;

	if (c0 < 0x80) {
		*p = s + 1;
		return c0;
	}
	if ((c0 & 0xE0) == 0xC0 && c1 && (c1 & 0xC0) == 0x80) {
		code = ((FT_ULong)(c0 & 0x1F) << 6) | (FT_ULong)(c1 & 0x3F);
		*p = s + 2;
		return code;
	}
	if ((c0 & 0xF0) == 0xE0 && c1 && c2 && (c1 & 0xC0) == 0x80 && (c2 & 0xC0) == 0x80) {
		code = ((FT_ULong)(c0 & 0x0F) << 12) | ((FT_ULong)(c1 & 0x3F) << 6) |
			(FT_ULong)(c2 & 0x3F);
		*p = s + 3;
		return code;
	}
	if ((c0 & 0xF8) == 0xF0 && c1 && c2 && s[3] && (c1 & 0xC0) == 0x80 &&
		(c2 & 0xC0) == 0x80 && (s[3] & 0xC0) == 0x80) {
		code = ((FT_ULong)(c0 & 0x07) << 18) | ((FT_ULong)(c1 & 0x3F) << 12) |
			((FT_ULong)(c2 & 0x3F) << 6) | (FT_ULong)(s[3] & 0x3F);
		*p = s + 4;
		return code;
	}
	// Not valid UTF-8: skip the byte so the loop always makes progress.
	*p = s + 1;
	return c0;
}

// ly_font_measure returns the advance width of a UTF-8 string in pixels plus
// the font height information, all with the face loaded at the pixel size the
// pattern asks for.
static void ly_font_measure(FT_Face face, int pixel_size, const char *utf8,
                            int *width, int *ascent, int *descent, int *height) {
	const unsigned char *p = (const unsigned char *)utf8;
	int w = 0, asc = 0, desc = 0;

	*width = 0;
	*ascent = 0;
	*descent = 0;
	*height = 0;
	if (!face)
		return;

	FT_Set_Pixel_Sizes(face, 0, (FT_UInt)(pixel_size > 0 ? pixel_size : 13));
	asc = face->size->metrics.ascender >> 6;
	desc = -(face->size->metrics.descender >> 6);

	while (*p) {
		FT_UInt index = FT_Get_Char_Index(face, ly_next_utf8(&p));
		if (FT_Load_Glyph(face, index, FT_LOAD_DEFAULT) == 0) {
			FT_Pos topY = face->glyph->metrics.horiBearingY;
			w += face->glyph->metrics.horiAdvance >> 6;
			if (topY > asc) {
				asc = topY >> 6;
			}
			if (-(topY >> 6) > desc) {
				desc = -(topY >> 6);
			}
		}
	}
	*width = w;
	*ascent = asc;
	*descent = desc;
	*height = asc + desc;
}

// ly_font_ascent_descent reports the face-wide vertical metrics.
static void ly_font_vmetrics(FT_Face face, int pixel_size, int *ascent, int *descent) {
	*ascent = 0;
	*descent = 0;
	if (!face)
		return;
	FT_Set_Pixel_Sizes(face, 0, (FT_UInt)(pixel_size > 0 ? pixel_size : 13));
	*ascent = face->size->metrics.ascender >> 6;
	*descent = -(face->size->metrics.descender >> 6);
}

static void ly_face_done(FT_Face face) {
	if (face)
		FT_Done_Face(face);
}
*/
import "C"

// Font draws and measures text. Drawing goes through Xft, measurement through
// FreeType on the same fontconfig pattern, so both agree on the metrics.
type Font struct {
	xft     *C.XftFont
	face    *C.FT_Face
	pattern string
	pixels  int
}

// FontDraw is a text drawing context bound to one window.
type FontDraw struct {
	draw  *C.XftDraw
	color *C.XftColor
	font  *Font
}

// OpenFont opens a fontconfig pattern such as "sans:size=13". pixelSize is the
// height in pixels used for measuring; pass 0 to derive it from the pattern.
func (d *Display) OpenFont(pattern string, pixelSize int) (*Font, error) {
	if d.dpy == nil {
		return nil, errClosed
	}
	if pixelSize <= 0 {
		pixelSize = PatternPixelSize(pattern)
	}
	cp := C.CString(pattern)
	defer C.free(unsafe.Pointer(cp))

	xft := C.ly_font_open(d.dpy, C.int(d.screen), cp)
	if xft == nil {
		return nil, fmt.Errorf("x11: cannot open font %q", pattern)
	}
	f := &Font{xft: xft, pattern: pattern, pixels: pixelSize}
	face := C.ly_font_face(cp)
	if face != nil {
		f.face = &face
	}
	return f, nil
}

// PatternPixelSize extracts size=N from a fontconfig pattern, defaulting to
// 13 when it is absent.
func PatternPixelSize(pattern string) int {
	const def = 13
	for _, part := range strings.Split(pattern, ":") {
		if v, ok := strings.CutPrefix(part, "size="); ok {
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
				return n
			}
		}
	}
	return def
}

// CloseFont releases the font.
func (d *Display) CloseFont(f *Font) {
	if f == nil {
		return
	}
	if f.face != nil {
		C.ly_face_done(*f.face)
		f.face = nil
	}
	if f.xft != nil && d.dpy != nil {
		C.ly_font_close(d.dpy, f.xft)
		f.xft = nil
	}
}

// Pattern returns the fontconfig pattern.
func (f *Font) Pattern() string {
	if f == nil {
		return ""
	}
	return f.pattern
}

// Metrics measures a UTF-8 string, returning its width in pixels plus the
// ascent, descent and total height of the font.
func (f *Font) Metrics(s string) (width, ascent, descent, height int) {
	if f == nil {
		return 0, 0, 0, 0
	}
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	var w, a, dsc, h C.int
	if f.face != nil {
		C.ly_font_measure(*f.face, C.int(f.pixels), cs, &w, &a, &dsc, &h)
	}
	if w == 0 && len(s) > 0 {
		// No FreeType face: fall back to a rough estimate so layout still
		// looks sane.
		w = C.int(len([]rune(s)) * f.pixels * 11 / 20)
		a = C.int(f.pixels * 4 / 5)
		dsc = C.int(f.pixels / 5)
		h = a + dsc
	}
	return int(w), int(a), int(dsc), int(h)
}

// Width returns the width of a string in pixels.
func (f *Font) Width(s string) int {
	w, _, _, _ := f.Metrics(s)
	return w
}

// Height returns the font height in pixels.
func (f *Font) Height() int {
	_, a, d, h := f.Metrics("Ay")
	if h > 0 {
		return h
	}
	return a + d
}

// LineHeight returns a comfortable distance between two baselines.
func (f *Font) LineHeight() int {
	h := f.Height()
	if h <= 0 {
		return 0
	}
	return h * 3 / 2
}

// Truncate shortens a string until it fits into maxWidth pixels, appending an
// ellipsis when anything was cut.
func (f *Font) Truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if f.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for n := len(runes) - 1; n > 0; n-- {
		candidate := string(runes[:n]) + "…"
		if f.Width(candidate) <= maxWidth {
			return candidate
		}
	}
	return ""
}

// NewFontDraw creates a drawing context for a window. The font must outlive
// it.
func (d *Display) NewFontDraw(w Window, f *Font) (*FontDraw, error) {
	if d.dpy == nil || f == nil || f.xft == nil {
		return nil, errClosed
	}
	draw := C.ly_font_draw(d.dpy, C.Drawable(w), f.xft)
	if draw == nil {
		return nil, ErrBadDrawable
	}
	color := (*C.XftColor)(C.malloc(C.sizeof_XftColor))
	if color == nil {
		C.ly_font_draw_free(d.dpy, draw)
		return nil, ErrBadAlloc
	}
	return &FontDraw{draw: draw, color: color, font: f}, nil
}

// Close releases the drawing context.
func (fd *FontDraw) Close(d *Display) {
	if fd == nil {
		return
	}
	if fd.draw != nil {
		C.ly_font_draw_free(d.dpy, fd.draw)
		fd.draw = nil
	}
	if fd.color != nil {
		C.free(unsafe.Pointer(fd.color))
		fd.color = nil
	}
}

// SetColor selects the text colour.
func (fd *FontDraw) SetColor(d *Display, c color.RGBA) {
	if fd == nil || fd.draw == nil {
		return
	}
	C.ly_font_alloc_color(d.dpy, C.uchar(c.R), C.uchar(c.G), C.uchar(c.B), fd.color)
}

// DrawText draws a UTF-8 string with its baseline origin at (x, y).
func (fd *FontDraw) DrawText(d *Display, x, y int, s string) {
	if fd == nil || fd.draw == nil || s == "" {
		return
	}
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	C.ly_font_text(fd.draw, fd.color, fd.font.xft, cs, C.int(x), C.int(y))
}

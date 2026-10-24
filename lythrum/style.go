package main

/*
#include <X11/Xlib.h>
#include <X11/cursorfont.h>
#include <X11/Xft/Xft.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// style is a preset of the look of the shell. It carries the palette, the
// metrics of the frames, the look of the icons, the sound profile and the
// style sheet of the widgets, so that a single choice changes the whole
// system.
type style struct {
	Name    string
	Title   string
	Note    string
	Palette palette
	Metrics metrics
	Icons   string // flat, outline, classic, glass
	Cursor  string // the shape of the pointer: fleur, left_ptr, hand2…
	Sound   soundStyle
	Widget  string // the file of the style sheet of the widgets
}

// palette holds every colour the shell draws with.
type palette struct {
	Bg        color
	GridMinor color
	GridMajor color
	Axis      color
	Hud       color
	Text      color
	Paper     color
	Fold      color
	SelBg     color
	SelBorder color
	Folder    color

	WinTitleBg color
	WinTitleOn color
	WinTitleFg color
	WinGrip    color
	WinCloseBg color
	WinCloseFg color

	MenuBg     color
	MenuBorder color
	Panel      color
	App        color
	BadgeBg    color
	BadgeFg    color
}

// metrics holds the sizes and the look of the frames.
type metrics struct {
	TitleH    int  // the height of the title bar
	Radius    int  // the radius of the corners, in pixels
	Bevel     bool // the classic raised frames
	Gradient  bool // the title bar is painted with a gradient
	Glass     bool // the panels are painted like glass
	FontSize  int
	IconShape int // the size of the shortcut tile
}

// soundStyle describes the sounds of the shell. The tones are synthesised by
// the shell itself, so a style needs no sound files.
type soundStyle struct {
	Wave   string // sine, square, triangle
	Click  int    // the frequency in hertz
	Drop   int
	Open   int
	Close  int
	Volume float64
}

// The metrics of the shell, set by the active style.
var (
	winTitleH      = 22
	styleRadius    = 0
	styleBevel     = false
	styleGradient  = false
	styleGlass     = false
	styleIconShape = "flat"
	styleCursor    = "fleur"
	styleFontSize  = 10
	styleAccent    = color{0x4c, 0x6e, 0xf5}
	styleSound     = soundStyle{Wave: "sine", Click: 880, Drop: 660, Open: 990,
		Close: 520, Volume: 0.5}
)

// styleNames lists the built-in styles in the order they are shown.
var styleNames = []string{"metro", "winui3", "material3", "win95", "glass"}

// builtinStyles returns the styles that ship with the shell.
func builtinStyles() []style {
	return []style{
		metroStyle(),
		winUI3Style(),
		material3Style(),
		win95Style(),
		glassStyle(),
	}
}

// styleByName returns a style by its name.
func styleByName(name string) style {
	for _, s := range builtinStyles() {
		if s.Name == name {
			return s
		}
	}
	return metroStyle()
}

// metroStyle is the flat, square look: one accent colour, no shadows, sharp
// corners.
func metroStyle() style {
	p := palette{
		Bg:        color{0x14, 0x16, 0x1a},
		GridMinor: color{0x22, 0x26, 0x2e},
		GridMajor: color{0x30, 0x36, 0x42},
		Axis:      color{0x8a, 0x4a, 0x4a},
		Hud:       color{0xc8, 0xcc, 0xd4},
		Text:      color{0xe8, 0xec, 0xf2},
		Paper:     color{0xd8, 0xdc, 0xe4},
		Fold:      color{0x91, 0x99, 0xa6},
		SelBg:     color{0x24, 0x2b, 0x38},
		SelBorder: color{0x00, 0x78, 0xd7},
		Folder:    color{0xe0, 0xa8, 0x2e},

		WinTitleBg: color{0x22, 0x25, 0x2b},
		WinTitleOn: color{0x00, 0x78, 0xd7},
		WinTitleFg: color{0xdd, 0xe3, 0xee},
		WinGrip:    color{0x00, 0x78, 0xd7},
		WinCloseBg: color{0x22, 0x25, 0x2b},
		WinCloseFg: color{0xe0, 0x6a, 0x6a},

		MenuBg:     color{0x20, 0x25, 0x2f},
		MenuBorder: color{0x46, 0x4e, 0x5e},
		Panel:      color{0x16, 0x19, 0x20},
		App:        color{0x00, 0x78, 0xd7},
		BadgeBg:    color{0x10, 0x14, 0x1c},
		BadgeFg:    color{0xe8, 0xec, 0xf2},
	}
	return style{
		Name:    "metro",
		Title:   "Metro",
		Note:    "Плоские плитки, один акцентный цвет, прямые углы",
		Palette: p,
		Metrics: metrics{TitleH: 22, FontSize: 10},
		Icons:   "flat",
		Cursor:  "fleur",
		Sound: soundStyle{Wave: "sine", Click: 880, Drop: 660, Open: 990,
			Close: 520, Volume: 0.5},
		Widget: "metro.css",
	}
}

// winUI3Style is the soft, rounded look of the modern Windows.
func winUI3Style() style {
	p := palette{
		Bg:        color{0x1b, 0x1d, 0x22},
		GridMinor: color{0x25, 0x28, 0x2f},
		GridMajor: color{0x33, 0x37, 0x40},
		Axis:      color{0x6a, 0x4a, 0x4a},
		Hud:       color{0xcf, 0xd4, 0xdc},
		Text:      color{0xf2, 0xf4, 0xf8},
		Paper:     color{0xe2, 0xe6, 0xee},
		Fold:      color{0x9a, 0xa2, 0xb0},
		SelBg:     color{0x2b, 0x31, 0x3c},
		SelBorder: color{0x00, 0x67, 0xc0},
		Folder:    color{0xf0, 0xb8, 0x3c},

		WinTitleBg: color{0x2a, 0x2e, 0x36},
		WinTitleOn: color{0x00, 0x67, 0xc0},
		WinTitleFg: color{0xe8, 0xec, 0xf4},
		WinGrip:    color{0x00, 0x67, 0xc0},
		WinCloseBg: color{0x2a, 0x2e, 0x36},
		WinCloseFg: color{0xe0, 0x6a, 0x6a},

		MenuBg:     color{0x26, 0x2a, 0x32},
		MenuBorder: color{0x50, 0x57, 0x63},
		Panel:      color{0x1f, 0x22, 0x29},
		App:        color{0x00, 0x67, 0xc0},
		BadgeBg:    color{0x15, 0x18, 0x1e},
		BadgeFg:    color{0xf0, 0xf3, 0xf8},
	}
	return style{
		Name:    "winui3",
		Title:   "WinUI 3",
		Note:    "Мягкие панели, скруглённые окна, тонкие границы",
		Palette: p,
		Metrics: metrics{TitleH: 26, Radius: 8, FontSize: 10},
		Icons:   "flat",
		Cursor:  "fleur",
		Sound: soundStyle{Wave: "sine", Click: 660, Drop: 520, Open: 880,
			Close: 440, Volume: 0.45},
		Widget: "winui3.css",
	}
}

// material3Style is the tonal look with the large corners.
func material3Style() style {
	p := palette{
		Bg:        color{0x1c, 0x1b, 0x1f},
		GridMinor: color{0x2a, 0x28, 0x2e},
		GridMajor: color{0x3a, 0x37, 0x40},
		Axis:      color{0x8a, 0x5a, 0x5a},
		Hud:       color{0xe6, 0xe0, 0xe9},
		Text:      color{0xe6, 0xe1, 0xe5},
		Paper:     color{0xe6, 0xe0, 0xe9},
		Fold:      color{0x9a, 0x93, 0x9f},
		SelBg:     color{0x4f, 0x45, 0x7a},
		SelBorder: color{0xd0, 0xbc, 0xff},
		Folder:    color{0xf2, 0xb8, 0x4b},

		WinTitleBg: color{0x2b, 0x29, 0x2f},
		WinTitleOn: color{0x67, 0x50, 0xa4},
		WinTitleFg: color{0xe6, 0xe1, 0xe5},
		WinGrip:    color{0xd0, 0xbc, 0xff},
		WinCloseBg: color{0x2b, 0x29, 0x2f},
		WinCloseFg: color{0xf2, 0xb8, 0x5b},

		MenuBg:     color{0x2b, 0x29, 0x2f},
		MenuBorder: color{0x49, 0x45, 0x4f},
		Panel:      color{0x24, 0x23, 0x28},
		App:        color{0xcf, 0xa9, 0xff},
		BadgeBg:    color{0x14, 0x13, 0x17},
		BadgeFg:    color{0xe6, 0xe1, 0xe5},
	}
	return style{
		Name:    "material3",
		Title:   "Material 3",
		Note:    "Тональные цвета, крупные скругления, контейнеры",
		Palette: p,
		Metrics: metrics{TitleH: 28, Radius: 16, FontSize: 11},
		Icons:   "outline",
		Cursor:  "fleur",
		Sound: soundStyle{Wave: "triangle", Click: 740, Drop: 590, Open: 880,
			Close: 460, Volume: 0.45},
		Widget: "material3.css",
	}
}

// win95Style is the classic look: the grey panels, the raised frames and the
// blue title bar.
func win95Style() style {
	p := palette{
		Bg:        color{0x00, 0x80, 0x80},
		GridMinor: color{0x00, 0x6e, 0x6e},
		GridMajor: color{0x00, 0x5c, 0x5c},
		Axis:      color{0x80, 0x00, 0x00},
		Hud:       color{0x00, 0x00, 0x00},
		Text:      color{0x00, 0x00, 0x00},
		Paper:     color{0xff, 0xff, 0xff},
		Fold:      color{0x80, 0x80, 0x80},
		SelBg:     color{0x00, 0x00, 0x80},
		SelBorder: color{0x00, 0x00, 0x00},
		Folder:    color{0xff, 0xd7, 0x4b},

		WinTitleBg: color{0x00, 0x00, 0x80},
		WinTitleOn: color{0x10, 0x84, 0xd0},
		WinTitleFg: color{0xff, 0xff, 0xff},
		WinGrip:    color{0x00, 0x00, 0x80},
		WinCloseBg: color{0xc0, 0xc0, 0xc0},
		WinCloseFg: color{0x00, 0x00, 0x00},

		MenuBg:     color{0xc0, 0xc0, 0xc0},
		MenuBorder: color{0x00, 0x00, 0x00},
		Panel:      color{0xc0, 0xc0, 0xc0},
		App:        color{0x00, 0x00, 0x80},
		BadgeBg:    color{0x00, 0x00, 0x00},
		BadgeFg:    color{0xff, 0xff, 0xff},
	}
	return style{
		Name:    "win95",
		Title:   "Windows 95",
		Note:    "Серые панели, объёмные рамки, классические значки",
		Palette: p,
		Metrics: metrics{TitleH: 20, Bevel: true, Gradient: true, FontSize: 10},
		Icons:   "classic",
		Cursor:  "left_ptr",
		Sound: soundStyle{Wave: "square", Click: 1200, Drop: 900, Open: 1500,
			Close: 700, Volume: 0.5},
		Widget: "win95.css",
	}
}

// glassStyle is the translucent look: the panels float above the wallpaper.
func glassStyle() style {
	p := palette{
		Bg:        color{0x1a, 0x1d, 0x24},
		GridMinor: color{0x28, 0x2c, 0x36},
		GridMajor: color{0x36, 0x3b, 0x47},
		Axis:      color{0x7a, 0x5a, 0x6a},
		Hud:       color{0xe4, 0xe8, 0xf0},
		Text:      color{0xf4, 0xf7, 0xfc},
		Paper:     color{0xe8, 0xec, 0xf4},
		Fold:      color{0xa4, 0xac, 0xba},
		SelBg:     color{0x3a, 0x44, 0x58},
		SelBorder: color{0x8a, 0xc0, 0xff},
		Folder:    color{0xf6, 0xc4, 0x52},

		WinTitleBg: color{0x33, 0x3a, 0x48},
		WinTitleOn: color{0x46, 0x54, 0x6a},
		WinTitleFg: color{0xf0, 0xf4, 0xfa},
		WinGrip:    color{0x8a, 0xc0, 0xff},
		WinCloseBg: color{0x33, 0x3a, 0x48},
		WinCloseFg: color{0xff, 0x8a, 0x8a},

		MenuBg:     color{0x2e, 0x34, 0x42},
		MenuBorder: color{0x7a, 0x86, 0x9c},
		Panel:      color{0x26, 0x2b, 0x36},
		App:        color{0x8a, 0xc0, 0xff},
		BadgeBg:    color{0x18, 0x1c, 0x24},
		BadgeFg:    color{0xf0, 0xf4, 0xfa},
	}
	return style{
		Name:    "glass",
		Title:   "Liquid Glass",
		Note:    "Прозрачные панели, мягкие блики, крупные радиусы",
		Palette: p,
		Metrics: metrics{TitleH: 28, Radius: 20, Glass: true, FontSize: 11},
		Icons:   "glass",
		Cursor:  "hand2",
		Sound: soundStyle{Wave: "sine", Click: 520, Drop: 400, Open: 700,
			Close: 340, Volume: 0.35},
		Widget: "glass.css",
	}
}

// applyStyle paints the whole shell in the colours of a style.
func applyStyle(s style) {
	p := s.Palette
	bg = p.Bg
	gridMinor = p.GridMinor
	gridMajor = p.GridMajor
	axisCol = p.Axis
	hudCol = p.Hud
	textCol = p.Text
	paperCol = p.Paper
	foldCol = p.Fold
	selBg = p.SelBg
	selBorder = p.SelBorder
	folderCol = p.Folder

	winTitleBg = p.WinTitleBg
	winTitleOn = p.WinTitleOn
	winTitleFg = p.WinTitleFg
	winGripCol = p.WinGrip
	winCloseBg = p.WinCloseBg
	winCloseFg = p.WinCloseFg

	menuBg = p.MenuBg
	menuBorder = p.MenuBorder
	panelBg = p.Panel
	appGlyph = p.App
	badgeBg = p.BadgeBg
	badgeFg = p.BadgeFg

	winTitleH = s.Metrics.TitleH
	styleRadius = s.Metrics.Radius
	styleBevel = s.Metrics.Bevel
	styleGradient = s.Metrics.Gradient
	styleGlass = s.Metrics.Glass
	styleFontSize = s.Metrics.FontSize
	styleIconShape = s.Icons
	styleCursor = s.Cursor
	styleSound = s.Sound
	styleAccent = p.SelBorder
}

// styleSummary describes a style in one line, for the settings page.
func styleSummary(name string) string {
	s := styleByName(name)
	return fmt.Sprintf("%s — %s", s.Title, s.Note)
}

// styleWidgetCSS returns the style sheet of the widgets of a style: the
// layout of the widgets and the colours of the style.
func styleWidgetCSS(name string) string {
	s := styleByName(name)
	if s.Widget == "" {
		return ""
	}
	layout, err := readDataFile("styles/widgets.css")
	if err != nil {
		return ""
	}
	colors, err := readDataFile("styles/" + s.Widget)
	if err != nil {
		return string(layout)
	}
	return string(layout) + "\n" + string(colors)
}

// styleDescription lists the styles for the settings app.
func styleList() []string {
	return append([]string{}, styleNames...)
}

// applyStyleByName switches the shell to a style and remembers the choice.
func (v *view) applyStyleByName(name string) {
	s := styleByName(name)
	applyStyle(s)
	v.cfg.Style = s.Name
	// The shape of the pointer belongs to the style.
	v.cfg.Cursor.Shape = s.Cursor
	v.cfg.save()
	v.xcolors = map[color]*C.XftColor{}
	v.reopenFont()
	v.applyCursor()
	v.redraw = true
}

// reopenFont loads the font of the current style.
func (v *view) reopenFont() {
	if v.dpy == nil {
		return
	}
	if v.xftFont != nil {
		C.XftFontClose(v.dpy, v.xftFont)
		v.xftFont = nil
	}
	name := fmt.Sprintf("sans:size=%d", styleFontSize)
	v.xftFont = C.XftFontOpenName(v.dpy, v.screen, C.CString(name))
	if v.xftFont == nil {
		v.xftFont = C.XftFontOpenName(v.dpy, v.screen, C.CString("sans:size=10"))
	}
}

// cursorShapes are the cursors the shell offers.
var cursorShapes = []struct {
	name  string
	glyph int
}{
	{"fleur", 152},   // XC_fleur
	{"left_ptr", 68}, // XC_left_ptr
	{"hand2", 60},    // XC_hand2
	{"xcross", 34},   // XC_xcross
	{"question", 40}, // XC_question_arrow
	{"watch", 150},   // XC_watch
}

// applyCursor puts the cursor of the settings on the board.
func (v *view) applyCursor() {
	if v.dpy == nil || v.win == 0 {
		return
	}
	glyph := 152 // XC_fleur
	shape := v.cfg.Cursor.Shape
	if shape == "" {
		// The shape of the active style is the default.
		shape = styleCursor
	}
	for _, c := range cursorShapes {
		if c.name == shape {
			glyph = c.glyph
			break
		}
	}
	if cur := C.XCreateFontCursor(v.dpy, C.uint(glyph)); cur != 0 {
		C.XDefineCursor(v.dpy, v.win, cur)
		C.XFlush(v.dpy)
	}
}

// dataFile looks for a file of the data folder of the shell. The folder is
// looked for next to the program, in the usual places of an installation and
// in the working directory, so the shell works from a source checkout as well
// as from an installation.
func dataFile(name string) (string, error) {
	candidates := []string{
		filepath.Join("data", name),
		filepath.Join("lythrum", "data", name),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "data", name),
			filepath.Join(dir, "..", "data", name),
			filepath.Join(dir, "..", "share", "lythrum", "data", name),
			filepath.Join(dir, "..", "..", "share", "lythrum", "data", name))
	}
	candidates = append(candidates,
		filepath.Join("/usr/local/share/lythrum/data", name),
		filepath.Join("/usr/share/lythrum/data", name))
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".local", "share", "lythrum", "data", name))
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil {
			return string(data), nil
		}
	}
	return "", os.ErrNotExist
}

// readDataFile reads a file of the data folder of the shell.
func readDataFile(name string) ([]byte, error) {
	data, err := dataFile(name)
	if err != nil {
		return nil, err
	}
	return []byte(data), nil
}

// runCommand runs a shell command and returns its output. The settings app
// uses it to drive the system services.
func runCommand(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// lighten returns a colour that is closer to the white.
func lighten(c color, f float64) color {
	mix := func(a, b byte) byte {
		return byte(float64(a) + (255-float64(a))*f)
	}
	return color{mix(c.r, 0), mix(c.g, 0), mix(c.b, 0)}
}

// darken returns a colour that is closer to the black.
func darken(c color, f float64) color {
	mix := func(a byte) byte {
		return byte(float64(a) * (1 - f))
	}
	return color{mix(c.r), mix(c.g), mix(c.b)}
}

// mix blends two colours: t is the share of the second one.
func mix(a, b color, t float64) color {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	blend := func(x, y byte) byte {
		return byte(float64(x) + (float64(y)-float64(x))*t)
	}
	return color{blend(a.r, b.r), blend(a.g, b.g), blend(a.b, b.b)}
}

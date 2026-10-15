package wm

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"lythrum/internal/desk"
	"lythrum/internal/imagex"
	"lythrum/internal/layout"
	"lythrum/internal/x11"
)

// desktopLayer draws the icons of the desktop directory over the wallpaper.
// The window of every monitor leaves its background unpainted, so the wallpaper
// stays visible, and the layer sits below the windows, so a maximized client
// covers the icons.
//
// X has no alpha blending between windows, so an icon cannot float on the
// picture directly: every icon is composited by hand onto its own plate, the
// same way a window manager with painted desktop icons has to do it.
type desktopLayer struct {
	wm *WM

	surfaces []*desktopSurface
	entries  []desk.Entry
	// rects are the icon slots in canvas coordinates, shared by every monitor.
	rects  []layout.Rect
	icons  *imagex.Loader
	font   *x11.Font
	colors gridColors
	plate  color.RGBA
	dir    string

	selection int
	lastIndex int
	lastClick time.Time
}

// desktopSurface is one monitor worth of desktop: the wallpaper and the icons
// on top of it.
type desktopSurface struct {
	win  x11.Window
	gc   x11.GC
	text *x11.FontDraw
	// area is the monitor in root coordinates: it says which part of the
	// desktop this surface shows.
	area layout.Rect
	// paper is the background picture of this monitor, already dimmed.
	paper *image.RGBA
	// rects are the icon slots in canvas coordinates. They stay where they
	// were put when the desktop is panned.
	rects []layout.Rect
}

// newDesktopLayer builds the desktop surface and places the first icons.
func newDesktopLayer(w *WM) *desktopLayer {
	d := &desktopLayer{
		wm:     w,
		icons:  imagex.NewLoader(),
		colors: gridPalette(w.cfg),
		plate:  color.RGBA{R: 0x16, G: 0x1b, B: 0x24, A: 0xff},
		dir:    desk.Dir(),
	}
	font, err := w.d.OpenFont("sans:size=13", 17)
	if err != nil {
		w.log.Warn("no usable font for the desktop icons", "error", err)
	} else {
		d.font = font
	}
	d.build()
	d.scan()
	w.log.Info("desktop", "icons", w.cfg.DesktopIcons, "dir", d.dir, "entries", len(d.entries))
	return d
}

// build creates one window per monitor. The surface carries the wallpaper as
// well, so it exists even when the icons are switched off.
func (d *desktopLayer) build() {
	w := d.wm
	d.destroy()
	if w.d == nil {
		return
	}
	for i := range w.monitors {
		area := w.workArea(i)
		s := &desktopSurface{area: area}
		s.win, s.gc = d.makeWindow(area)
		if s.win == 0 {
			d.destroy()
			return
		}
		if s.gc.Valid() {
			if draw, err := w.d.NewFontDraw(s.win, d.font); err == nil {
				s.text = draw
			}
		}
		d.surfaces = append(d.surfaces, s)
	}
	d.restack()
}

func (d *desktopLayer) makeWindow(area layout.Rect) (x11.Window, x11.GC) {
	w := d.wm
	win, err := w.d.CreateWindow(x11.CreateWindowParams{
		Parent: w.d.Root(),
		X:      area.X,
		Y:      area.Y,
		Width:  uint(maxInt(area.W, 1)),
		Height: uint(maxInt(area.H, 1)),
		Class:  x11.ClassInputOutput,
		// Nothing is painted unless an icon asks for it, so the wallpaper
		// shows through the whole window.
		BackNone:         true,
		OverrideRedirect: true,
		// Pointer motion has to be asked for: while a button is held the
		// server only reports motion to the window the press went to, and
		// without it the desktop would not be pannable by dragging.
		EventMask: x11.ExposureMask | x11.ButtonPressMask |
			x11.PointerMotionMask | x11.StructureNotifyMask,
	})
	if err != nil || win == 0 {
		return 0, x11.GC{}
	}
	gc, err := w.d.CreateGC(win)
	if err != nil {
		w.log.Warn("cannot create a graphics context for the desktop icons", "error", err)
	}
	w.d.MapRaised(win)
	w.d.SetUTF8(win, x11.A.NETWMName, "lythrum-desktop")
	return win, gc
}

// restack puts the desktop at the bottom of the stacking order, under every
// window.
func (d *desktopLayer) restack() {
	w := d.wm
	if w == nil || w.d == nil {
		return
	}
	for _, s := range d.surfaces {
		if s.win != 0 {
			w.d.Restack(s.win, 0, x11.Below)
		}
	}
	w.d.Sync()
}

// SetWallpaper stores the background picture of a monitor. The wallpaper player
// decodes it; the desktop surface is what puts it on the screen.
func (d *desktopLayer) SetWallpaper(area layout.Rect, img *image.RGBA) {
	if d == nil || img == nil {
		return
	}
	for _, s := range d.surfaces {
		if s.area != area {
			continue
		}
		s.paper = img
		d.paintSurface(s)
		return
	}
}

// destroy releases every window, which the shell does on shutdown and when the
// layer is rebuilt.
func (d *desktopLayer) destroy() {
	if d == nil || d.wm == nil || d.wm.d == nil {
		d.surfaces = nil
		return
	}
	for _, s := range d.surfaces {
		if s.text != nil {
			s.text.Close(d.wm.d)
		}
		if s.gc.Valid() {
			d.wm.d.FreeGC(s.gc)
		}
		if s.win != 0 {
			d.wm.d.DestroyWindow(s.win)
		}
	}
	d.surfaces = nil
}

// Shutdown closes the layer for good.
func (d *desktopLayer) Shutdown() {
	if d == nil {
		return
	}
	d.destroy()
	if d.font != nil && d.wm != nil && d.wm.d != nil {
		d.wm.d.CloseFont(d.font)
		d.font = nil
	}
}

// scan re-reads the desktop directory. Only the entries that live there are
// icons; the launchers and the home files belong to the start screen.
func (d *desktopLayer) scan() {
	if d == nil || d.wm == nil {
		return
	}
	d.dir = desk.Dir()
	own := make([]desk.Entry, 0, 16)
	for _, e := range d.wm.deskEntries() {
		if e.Kind == desk.KindLive || !underDir(e.Path, d.dir) {
			continue
		}
		own = append(own, e)
	}
	changed := signature(d.entries) != signature(own)
	d.entries = own
	if d.selection >= len(own) {
		d.selection = maxInt(len(own)-1, 0)
	}
	if changed && len(d.surfaces) > 0 {
		// Captions of the icons that are gone cannot be erased, because the
		// window has no background to clear it with. New windows start clean.
		d.build()
	}
	d.layout()
	d.repaint()
}

// signature identifies the icon set, so a rescan can tell whether anything on
// screen really changed.
func signature(entries []desk.Entry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Path)
		b.WriteByte(0)
	}
	return b.String()
}

// underDir reports whether a path sits directly inside dir.
func underDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	base, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return filepath.Dir(abs) == base
}

// paperFor fills the monitor with the background picture, or with the plain
// backdrop when there is no wallpaper. The picture is tiled so that panning
// shows another part of it instead of stretching it.
func (d *desktopLayer) paperFor(s *desktopSurface) {
	d.wm.d.SetForegroundOf(s.gc, wallpaperBackdropPixel)
	d.wm.d.FillRectangle(s.win, s.gc, 0, 0, s.area.W, s.area.H)
	if s.paper == nil {
		return
	}
	w, h := s.paper.Bounds().Dx(), s.paper.Bounds().Dy()
	if w <= 0 || h <= 0 {
		return
	}
	off := d.wm.canvas.Offset()
	dx := -mod(off.X, w)
	dy := -mod(off.Y, h)
	for y := dy; y < s.area.H; y += h {
		for x := dx; x < s.area.W; x += w {
			if err := d.wm.d.UploadImageOver(s.win, s.gc, s.paper, x, y, wallpaperBackdrop); err != nil {
				d.wm.log.Debug("cannot draw the wallpaper", "at", fmt.Sprintf("%+d%+d", s.area.X, s.area.Y), "error", err)
				return
			}
		}
	}
}

// canvasArea is where the icons live: a fixed square of the desktop, so that
// panning the view does not drag a new grid of icons along with it.
var canvasArea = layout.Rect{W: 8192, H: 8192}

// layout places the icon slots once, in canvas coordinates.
func (d *desktopLayer) layout() {
	d.rects = layout.DesktopIcons(canvasArea, len(d.entries), layout.DefaultDesktopIconOptions())
	for _, s := range d.surfaces {
		s.rects = d.rects
	}
}

// repaint draws every icon again.
func (d *desktopLayer) repaint() {
	if d == nil || d.wm == nil || d.wm.d == nil || len(d.surfaces) == 0 {
		return
	}
	for _, s := range d.surfaces {
		d.paintSurface(s)
	}
	d.wm.d.Flush()
}

// paintSurface draws the whole desktop of one monitor: the wallpaper of that
// part of the canvas first, the icons on top. The rest of the desktop stays
// empty, which is what makes it feel endless.
func (d *desktopLayer) paintSurface(s *desktopSurface) {
	if !s.gc.Valid() {
		return
	}
	d.paperFor(s)
	if !d.wm.cfg.DesktopIcons {
		d.wm.d.Sync()
		return
	}
	opts := layout.DefaultDesktopIconOptions()
	for i, rect := range s.rects {
		if i >= len(d.entries) {
			break
		}
		root := d.wm.canvas.ToRootRect(rect)
		if !root.Intersects(s.area) {
			continue
		}
		d.paintIcon(s, root, i, opts)
	}
	d.wm.d.Sync()
}

// paintIcon draws one icon with its caption.
//
// Every slot is drawn the same way every time: the plate is opaque, so a
// repaint overwrites what was there before, and the selection is a frame drawn
// inside the plate. Anything else would leave the pixels of the previous
// selection on screen, because a window with no background cannot be erased.
func (d *desktopLayer) paintIcon(s *desktopSurface, rect layout.Rect, index int, opts layout.DesktopIconOptions) {
	entry := d.entries[index]
	x := rect.X - s.area.X
	y := rect.Y - s.area.Y
	icon := opts.Icon
	if icon > rect.W-8 {
		icon = rect.W - 8
	}
	ix := x + (rect.W-icon)/2
	iy := y + 6

	if img := d.iconFor(entry, icon); img != nil {
		if err := d.wm.d.UploadImage(s.win, s.gc, img, ix, iy); err != nil {
			d.wm.log.Debug("cannot draw a desktop icon", "entry", entry.Name, "error", err)
		}
	}
	if index == d.selection {
		d.paintSelection(s, ix, iy, icon)
	}

	if s.text == nil || d.font == nil {
		return
	}
	lineHeight := d.font.LineHeight()
	if lineHeight <= 0 {
		lineHeight = 15
	}
	lines := desk.Label(entry.Name, 2, maxInt((rect.W-20)/8, 4))
	baseline := y + rect.H - 6 - (len(lines)-1)*lineHeight
	for _, line := range lines {
		if baseline > y+rect.H-2 || baseline < y+icon {
			break
		}
		w := d.font.Width(line)
		s.text.SetColor(d.wm.d, d.colors.Text)
		s.text.DrawText(d.wm.d, x+(rect.W-w)/2, baseline, line)
		baseline += lineHeight
	}
}

// paintSelection marks the chosen icon with a frame inside its plate.
func (d *desktopLayer) paintSelection(s *desktopSurface, x, y, size int) {
	const thickness = 3
	d.wm.d.SetForegroundOf(s.gc, d.pixel(d.colors.Accent))
	d.wm.d.FillRectangle(s.win, s.gc, x, y, size, thickness)
	d.wm.d.FillRectangle(s.win, s.gc, x, y+size-thickness, size, thickness)
	d.wm.d.FillRectangle(s.win, s.gc, x, y, thickness, size)
	d.wm.d.FillRectangle(s.win, s.gc, x+size-thickness, y, thickness, size)
}

// iconFor returns the picture of an icon: the launcher icon, a thumbnail for
// files that carry one, and a coloured block otherwise. Everything is
// composited onto the plate, because the window itself cannot blend.
func (d *desktopLayer) iconFor(entry desk.Entry, size int) *image.RGBA {
	if size > d.wm.d.MaxImageSide() {
		size = d.wm.d.MaxImageSide()
	}
	var img *image.RGBA
	switch {
	case d.wm.queue != nil && previewWorthy(entry):
		img = d.wm.queue.Get(entry.Path, maxInt(size, 64))
	case hasIconName(entry):
		if path, ok := imagex.LoadIcon(entry.Icon, size); ok {
			if loaded, err := d.icons.Load(path, size); err == nil {
				img = loaded
			}
		}
	case imagex.HasImageExtension(entry.Path):
		if loaded, err := d.icons.Load(entry.Path, size); err == nil {
			img = loaded
		}
	}
	if img == nil {
		img = glyphFor(entry, size, d.colors)
	}
	return onPlate(img, size, d.plate)
}

// hasIconName reports whether a launcher names an icon at all.
func hasIconName(entry desk.Entry) bool {
	return entry.Icon != "" && !strings.ContainsAny(entry.Icon, "/ ")
}

func (d *desktopLayer) pixel(c color.RGBA) uint32 {
	return d.wm.d.PixelFromRGB(uint32(c.R), uint32(c.G), uint32(c.B))
}

// Reload rebuilds the layer after a settings change: the icons may be switched
// off, and the monitors may have moved.
func (d *desktopLayer) Reload() {
	if d == nil || d.wm == nil {
		return
	}
	d.colors = gridPalette(d.wm.cfg)
	// The surface carries the wallpaper as well, so it stays even when the
	// icons are switched off.
	if len(d.surfaces) == 0 {
		d.build()
	} else {
		d.restack()
	}
	d.scan()
}

// surfaceFor reports the monitor a window belongs to.
func (d *desktopLayer) surfaceFor(win x11.Window) *desktopSurface {
	for _, s := range d.surfaces {
		if s.win == win {
			return s
		}
	}
	return nil
}

// HandleExpose repaints one monitor after it was uncovered.
func (d *desktopLayer) HandleExpose(win x11.Window) {
	if d == nil {
		return
	}
	s := d.surfaceFor(win)
	if s == nil {
		return
	}
	d.paintSurface(s)
	d.wm.d.Sync()
}

// HandleButton selects an icon and opens it on a double click. It reports
// whether the press landed on an icon: a press on the empty desktop belongs to
// the pan, and dropping the selection is left to the release.
func (d *desktopLayer) HandleButton(rootX, rootY int) bool {
	if d == nil || len(d.surfaces) == 0 {
		return false
	}
	idx := -1
	cx, cy := d.wm.canvas.ToCanvas(rootX, rootY)
	for _, s := range d.surfaces {
		if !s.area.Contains(rootX, rootY) {
			continue
		}
		idx = layout.IconHit(s.rects, cx, cy)
		break
	}
	if idx < 0 {
		return false
	}
	now := time.Now()
	double := idx == d.lastIndex && now.Sub(d.lastClick) < 400*time.Millisecond
	d.lastIndex, d.lastClick = idx, now
	if idx != d.selection {
		d.selection = idx
		d.repaint()
	}
	if !double {
		return true
	}
	entry := d.entries[idx]
	d.wm.log.Info("desktop icon opened", "entry", entry.Name, "path", entry.Path)
	go func() {
		if err := entry.Launch(); err != nil {
			d.wm.log.Warn("cannot open a desktop entry", "entry", entry.Name, "error", err)
		}
	}()
	return true
}

// Deselect drops the selection, which a click on the empty desktop does.
func (d *desktopLayer) Deselect() {
	if d == nil || d.selection < 0 {
		return
	}
	d.selection = -1
	d.repaint()
}

// Status describes the layer for the IPC layer.
func (d *desktopLayer) Status() map[string]string {
	if d == nil {
		return map[string]string{}
	}
	return map[string]string{
		"enabled":  fmt.Sprintf("%t", len(d.surfaces) > 0),
		"dir":      d.dir,
		"entries":  fmt.Sprintf("%d", len(d.entries)),
		"selected": fmt.Sprintf("%d", d.selection),
	}
}

func (d *desktopLayer) String() string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "desktop icons: %s\n", map[bool]string{true: "on", false: "off"}[len(d.surfaces) > 0])
	fmt.Fprintf(&b, "desktop dir: %s\n", d.dir)
	fmt.Fprintf(&b, "icons: %d\n", len(d.entries))
	return b.String()
}

// glyphFor draws the placeholder icon: a rounded block coloured by kind.
func glyphFor(entry desk.Entry, size int, colors gridColors) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	colour := colors.GlyphFile
	switch entry.Kind {
	case desk.KindApp:
		colour = colors.GlyphApp
	case desk.KindDir:
		colour = colors.GlyphDir
	case desk.KindShortcut:
		colour = colors.GlyphLink
	}
	radius := size / 5
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !insideRounded(x, y, size, size, radius) {
				continue
			}
			shade := colour
			if y < size/8 {
				shade = lighten(colour, 0.25)
			}
			img.Set(x, y, color.RGBA{shade.R, shade.G, shade.B, 0xff})
		}
	}
	return img
}

// onPlate composites an icon onto an opaque plate. The corners cannot stay
// transparent, because a plain TrueColor visual has no alpha channel and would
// paint them black.
func onPlate(icon *image.RGBA, size int, plate color.RGBA) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// A lighter top edge gives the plate a little depth.
			shade := plate
			if y < size/8 {
				shade = lighten(plate, 0.15)
			}
			out.Set(x, y, color.RGBA{R: shade.R, G: shade.G, B: shade.B, A: 0xff})
		}
	}
	// The icon keeps its own proportions and sits in the middle of the plate.
	w, h := icon.Bounds().Dx(), icon.Bounds().Dy()
	if w <= 0 || h <= 0 {
		return out
	}
	ox, oy := (size-w)/2, (size-h)/2
	for y := 0; y < h; y++ {
		dy := y + oy
		if dy < 0 || dy >= size {
			continue
		}
		for x := 0; x < w; x++ {
			dx := x + ox
			if dx < 0 || dx >= size {
				continue
			}
			c := icon.RGBAAt(x, y)
			if c.A == 0 {
				continue
			}
			out.SetRGBA(dx, dy, blend(c, out.RGBAAt(dx, dy)))
		}
	}
	return out
}

// blend puts src over dst with the alpha of src.
func blend(src, dst color.RGBA) color.RGBA {
	a := uint32(src.A)
	mix := func(s, d uint8) uint8 {
		return uint8((uint32(s)*a + uint32(d)*(255-a)) / 255)
	}
	return color.RGBA{R: mix(src.R, dst.R), G: mix(src.G, dst.G), B: mix(src.B, dst.B), A: 0xff}
}

package wm

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"lythrum/internal/config"
	"lythrum/internal/desk"
	"lythrum/internal/imagex"
	"lythrum/internal/layout"
	"lythrum/internal/x11"
)

// startScreen is the Windows 8 style tile grid that covers the whole work
// area. It is an override-redirect overlay above the tiled desktop, so the
// windows keep their geometry while it is open.
type startScreen struct {
	wm *WM

	overlays  []*overlay
	entries   []desk.Entry
	icons     *imagex.Loader
	page      int
	selection int
	open      bool
	groups    []desk.Group
	// currentX/currentY track the pointer inside the selection so keyboard
	// movement can start from the right tile.
	currentX, currentY int
	opened             time.Time
	lastClick          time.Time
	lastIndex          int

	font     *x11.Font
	colors   gridColors
	dir      string
	scanErr  error
	hasItems bool
}

// overlay is one window per monitor.
type overlay struct {
	screen *startScreen
	win    x11.Window
	gc     x11.GC
	text   *x11.FontDraw
	area   layout.Rect
	grid   layout.GroupGrid
}

// gridColors is the palette of the start screen.
type gridColors struct {
	Background color.RGBA
	Tile       color.RGBA
	TileHover  color.RGBA
	Selection  color.RGBA
	Accent     color.RGBA
	Text       color.RGBA
	TextDim    color.RGBA
	GlyphApp   color.RGBA
	GlyphDir   color.RGBA
	GlyphFile  color.RGBA
	GlyphLink  color.RGBA
}

// gridPalette reads the start screen colours from the configuration, falling
// back to the built-in palette for anything unset or malformed.
func gridPalette(cfg config.Config) gridColors {
	def := gridColors{
		Background: color.RGBA{0x1c, 0x1f, 0x26, 0xff},
		Tile:       color.RGBA{0x2e, 0x34, 0x40, 0xff},
		TileHover:  color.RGBA{0x3b, 0x42, 0x51, 0xff},
		Selection:  color.RGBA{0x43, 0x4c, 0x5e, 0xff},
		Accent:     color.RGBA{0x88, 0xc0, 0xd0, 0xff},
		Text:       color.RGBA{0xec, 0xef, 0xf4, 0xff},
		TextDim:    color.RGBA{0x8f, 0x9a, 0xad, 0xff},
		GlyphApp:   color.RGBA{0x88, 0xc0, 0xd0, 0xff},
		GlyphDir:   color.RGBA{0xa3, 0xbe, 0x8c, 0xff},
		GlyphFile:  color.RGBA{0xbf, 0x95, 0x6b, 0xff},
		GlyphLink:  color.RGBA{0xb4, 0x8f, 0xd0, 0xff},
	}
	over := func(spec string, fallback color.RGBA) color.RGBA {
		if spec == "" {
			return fallback
		}
		r, g, b, err := x11.HexColor(spec)
		if err != nil {
			return fallback
		}
		return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}
	}
	def.Background = over(cfg.GridBackground, def.Background)
	def.Tile = over(cfg.GridTile, def.Tile)
	def.Selection = over(cfg.GridSelection, def.Selection)
	def.Accent = over(cfg.GridAccent, def.Accent)
	def.Text = over(cfg.GridText, def.Text)
	def.TextDim = over(cfg.GridMuted, def.TextDim)
	return def
}

// newStartScreen prepares the overlay, loading the font but creating no
// windows yet.
func (w *WM) newStartScreen() *startScreen {
	w.widgets = w.newWidgetRegistry()
	w.live = newLiveViews(w, w.cfg.GridLiveLimit, w.cfg.GridLiveFPS)
	s := &startScreen{
		wm:     w,
		icons:  imagex.NewLoader(),
		colors: gridPalette(w.cfg),
		dir:    desk.Dir(),
	}
	s.scan()

	font, err := w.d.OpenFont("sans:size=13", 17)
	if err != nil {
		w.log.Warn("no usable font for the start screen", "error", err)
	} else {
		s.font = font
	}
	return s
}

// Open shows the start screen and grabs the input so it behaves modally.
func (s *startScreen) Open() error {
	if s.open {
		return nil
	}
	s.scan()
	if err := s.build(); err != nil {
		s.Close()
		return err
	}
	s.open = true
	s.opened = time.Now()
	s.page = 0
	s.selection = 0
	if err := s.grab(); err != nil {
		s.wm.log.Debug("the start screen could not grab the input", "error", err)
	}
	s.wm.focusOverlay()
	s.repaint()
	s.wm.log.Info("start screen opened", "entries", len(s.entries), "dir", s.dir)
	return nil
}

// Close hides the start screen and gives the input back.
func (s *startScreen) Close() {
	if !s.open {
		return
	}
	s.ungrab()
	for _, o := range s.overlays {
		if o.win != 0 {
			s.wm.d.UnmapWindow(o.win)
		}
	}
	s.overlays = nil
	s.open = false
	s.wm.restoreFocus()
	s.wm.log.Info("start screen closed")
}

// Toggle flips the start screen.
func (s *startScreen) Toggle() error {
	if s.open {
		s.Close()
		return nil
	}
	return s.Open()
}

// scan re-reads the desktop directory.
func (s *startScreen) scan() {
	s.dir = desk.Dir()
	entries, err := desk.Entries(s.dir)
	if err != nil {
		s.scanErr = err
		s.wm.log.Warn("cannot read the desktop directory", "dir", s.dir, "error", err)
		entries = nil
	} else {
		s.scanErr = nil
	}
	entries = s.addWidgetEntries(entries)
	s.groups, s.entries = desk.Grouped(entries, s.wm.cfg.GridGroups)
	s.hasItems = len(entries) > 0
	if s.scanErr == nil && len(s.entries) == 0 {
		s.wm.log.Info("no desktop entries", "dir", s.dir)
	}
}

// build creates one overlay window per monitor and lays out the tiles.
func (s *startScreen) build() error {
	w := s.wm
	if s.overlays == nil {
		for i := range w.monitors {
			o := &overlay{screen: s, area: w.workArea(i)}
			o.win, o.gc = s.makeOverlay(o.area)
			if o.win == 0 {
				s.destroyOverlays()
				return fmt.Errorf("wm: cannot create the start screen window")
			}
			if o.gc.Valid() {
				if draw, err := w.d.NewFontDraw(o.win, s.font); err == nil {
					o.text = draw
				} else {
					w.log.Debug("no text drawing context for the start screen", "error", err)
				}
			}
			s.overlays = append(s.overlays, o)
		}
	}
	s.layout()
	return nil
}

func (s *startScreen) destroyOverlays() {
	for _, o := range s.overlays {
		if o.text != nil {
			o.text.Close(s.wm.d)
		}
		if o.gc.Valid() {
			s.wm.d.FreeGC(o.gc)
		}
		if o.win != 0 {
			s.wm.d.DestroyWindow(o.win)
		}
	}
	s.overlays = nil
}

// layout recomputes the grouped tile geometry of every overlay.
func (s *startScreen) layout() {
	opts := layout.DefaultGroupOptions()
	opts.Gap = s.wm.cfg.GridGap
	opts.Border = s.wm.cfg.GridBorder
	opts.Cell = s.wm.cfg.GridMaxCell
	opts.Large = s.wm.cfg.GridLarge
	if s.wm.cfg.GridLargeMin > 0 {
		opts.LargeMin = s.wm.cfg.GridLargeMin
	}
	if s.wm.cfg.GridHeaderHeight > 0 {
		opts.HeaderHeight = s.wm.cfg.GridHeaderHeight
	}
	specs := make([]layout.GroupSpec, 0, len(s.groups))
	for _, g := range s.groups {
		specs = append(specs, layout.GroupSpec{Name: g.Title, First: g.First, Count: g.Count})
	}
	for _, o := range s.overlays {
		o.grid = layout.ComputeGroups(o.area, specs, opts)
		if s.page >= o.grid.PageCount() {
			s.page = o.grid.PageCount() - 1
		}
	}
}

// makeOverlay creates the overlay window covering one whole monitor.
func (s *startScreen) makeOverlay(area layout.Rect) (x11.Window, x11.GC) {
	w := s.wm
	win, err := w.d.CreateWindow(x11.CreateWindowParams{
		Parent:           w.d.Root(),
		X:                area.X,
		Y:                area.Y,
		Width:            uint(maxInt(area.W, 1)),
		Height:           uint(maxInt(area.H, 1)),
		Class:            x11.ClassInputOutput,
		BackPixel:        s.pixel(s.colors.Background),
		OverrideRedirect: true,
		EventMask:        x11.ExposureMask | x11.ButtonPressMask | x11.ButtonReleaseMask | x11.PointerMotionMask | x11.KeyPressMask | x11.LeaveWindowMask | x11.StructureNotifyMask,
	})
	if err != nil || win == 0 {
		return 0, x11.GC{}
	}
	gc, err := w.d.CreateGC(win)
	if err != nil {
		w.log.Warn("cannot create a graphics context for the start screen", "error", err)
	}
	w.d.MapRaised(win)
	return win, gc
}

// grab makes the start screen modal.
func (s *startScreen) grab() error {
	w := s.wm
	var firstErr error
	for _, o := range s.overlays {
		mask := x11.ButtonPressMask | x11.ButtonReleaseMask | x11.PointerMotionMask | x11.KeyPressMask
		if err := w.d.GrabInput(o.win, mask, 0); err != nil && firstErr == nil {
			firstErr = err
		}
		w.d.RaiseWindow(o.win)
	}
	return firstErr
}

func (s *startScreen) ungrab() {
	s.wm.d.UngrabInput()
}

// repaint draws every overlay. Without a display there is nothing to draw,
// which keeps the navigation logic usable in tests.
func (s *startScreen) repaint() {
	if s.wm == nil || s.wm.d == nil {
		return
	}
	for _, o := range s.overlays {
		s.paintOverlay(o)
	}
	s.wm.d.Sync()
}

// paintOverlay draws the background, the group bands, the tiles and the page
// dots.
func (s *startScreen) paintOverlay(o *overlay) {
	d := s.wm.d
	if !o.gc.Valid() {
		return
	}
	d.SetForegroundOf(o.gc, s.pixel(s.colors.Background))
	d.FillRectangle(o.win, o.gc, 0, 0, o.area.W, o.area.H)

	for _, band := range o.grid.Page(s.page) {
		s.paintHeader(o, band)
		for _, tile := range band.Tiles {
			s.paintTile(o, tile)
		}
	}
	s.paintPages(o)
	d.Flush()
}

// rebuildPalette re-reads the colours and the tile sizes after a settings
// change, then lays the tiles out again.
func (s *startScreen) rebuildPalette() {
	s.colors = gridPalette(s.wm.cfg)
	if s.font != nil {
		s.wm.d.CloseFont(s.font)
	}
	font, err := s.wm.d.OpenFont("sans:size=13", 17)
	if err != nil {
		s.font = nil
	} else {
		s.font = font
	}
	for _, o := range s.overlays {
		if o.text != nil {
			o.text.Close(s.wm.d)
			o.text = nil
		}
		if o.gc.Valid() {
			if draw, err := s.wm.d.NewFontDraw(o.win, s.font); err == nil {
				o.text = draw
			}
		}
	}
	s.scan()
	s.layout()
	if s.open {
		s.repaint()
	}
}

// snapshot describes the start screen so the shell can be inspected from the
// outside, which is how the layout is checked while developing.
func (s *startScreen) snapshot() map[string]any {
	groups := make([]map[string]any, 0, len(s.groups))
	for _, g := range s.groups {
		groups = append(groups, map[string]any{
			"key": g.Key, "title": g.Title, "first": g.First, "count": g.Count,
		})
	}
	pages := []int{}
	tiles := []int{}
	for _, o := range s.overlays {
		pages = append(pages, o.grid.PageCount())
		tiles = append(tiles, len(o.grid.Tiles(s.page)))
	}
	return map[string]any{
		"open": s.open, "page": s.page, "entries": len(s.entries),
		"groups": groups, "pages": pages, "tilesOnPage": tiles,
		"overlayArea": s.areaOfOverlay(),
		"live":        s.wm.live.describe(),
	}
}

// areaOfOverlay reports the size of the first overlay, which is what the
// layout works with.
func (s *startScreen) areaOfOverlay() layout.Rect {
	if len(s.overlays) == 0 {
		return layout.Rect{}
	}
	return s.overlays[0].area
}

// tickLive repaints the live tiles. WebKit delivers frames on its own thread,
// so the shell refreshes the tiles it can see from the event loop instead of
// waiting for an X event.
func (s *startScreen) tickLive() {
	if s == nil || !s.open || s.wm == nil || s.wm.live == nil {
		return
	}
	for _, o := range s.overlays {
		if !o.gc.Valid() {
			continue
		}
		for _, band := range o.grid.Page(s.page) {
			for _, tile := range band.Tiles {
				if tile.Index < len(s.entries) && s.entries[tile.Index].Widget != "" {
					s.paintTile(o, tile)
				}
			}
		}
	}
	s.wm.d.Flush()
}

// paintHeader draws the heading of a group band.
func (s *startScreen) paintHeader(o *overlay, band layout.Band) {
	if o.text == nil || s.font == nil || band.Name == "" {
		return
	}
	d := s.wm.d
	x := band.Tiles[0].Rect.X
	bottom := band.Tiles[0].Rect.Y
	// The heading sits just above the first row of tiles.
	base := bottom - s.font.Height()/3
	o.text.SetColor(d, s.colors.Accent)
	o.text.DrawText(d, x, base, s.font.Truncate(band.Name, o.area.W-2*o.area.X))

	// A short rule under the heading keeps the bands readable.
	if s.wm.cfg.GridHeaderHeight >= 12 {
		d.SetForegroundOf(o.gc, s.pixel(dim(s.colors.Accent, 0.75)))
		d.FillRectangle(o.win, o.gc, x, bottom-6, minInt(s.wm.cfg.GridHeaderHeight*6, o.area.W-x-o.area.X), 1)
	}
}

// paintTile draws one tile: background, icon and a wrapped caption.
func (s *startScreen) paintTile(o *overlay, tile layout.Tile) {
	d := s.wm.d
	cell := layout.Rect{X: tile.Rect.X - o.area.X, Y: tile.Rect.Y - o.area.Y, W: tile.Rect.W, H: tile.Rect.H}
	if tile.Index >= len(s.entries) {
		return
	}
	entry := s.entries[tile.Index]
	selected := tile.Index == s.selection

	fill := s.colors.Tile
	if selected {
		fill = s.colors.Selection
	}
	d.SetForegroundOf(o.gc, s.pixel(fill))
	d.FillRectangle(o.win, o.gc, cell.X, cell.Y, cell.W, cell.H)

	// A live tile shows the widget page itself instead of an icon.
	if entry.Widget != "" {
		if frame, ok := s.wm.live.Show(entry.Widget, cell); ok {
			if err := d.UploadImageOver(o.win, o.gc, frame.Image(), cell.X, cell.Y, fill); err != nil {
				s.wm.log.Warn("cannot draw a live tile", "widget", entry.Widget,
					"size", fmt.Sprintf("%dx%d", cell.W, cell.H),
					"frame", fmt.Sprintf("%dx%d", frame.Width, frame.Height), "error", err)
			}
			if selected {
				d.SetForegroundOf(o.gc, s.pixel(s.colors.Accent))
				d.FillRectangle(o.win, o.gc, cell.X, cell.Y, cell.W, 3)
				d.FillRectangle(o.win, o.gc, cell.X, cell.Bottom()-3, cell.W, 3)
				d.FillRectangle(o.win, o.gc, cell.X, cell.Y, 3, cell.H)
				d.FillRectangle(o.win, o.gc, cell.Right()-3, cell.Y, 3, cell.H)
			}
			return
		}
	}

	if selected {
		d.SetForegroundOf(o.gc, s.pixel(s.colors.Accent))
		d.FillRectangle(o.win, o.gc, cell.X, cell.Y, cell.W, 3)
		d.FillRectangle(o.win, o.gc, cell.X, cell.Bottom()-3, cell.W, 3)
		d.FillRectangle(o.win, o.gc, cell.X, cell.Y, 3, cell.H)
		d.FillRectangle(o.win, o.gc, cell.Right()-3, cell.Y, 3, cell.H)
	}

	// The icon sits above the caption; both share the tile with a small
	// margin so nothing is clipped by the neighbour.
	const margin = 10
	lineHeight := s.font.LineHeight()
	if lineHeight <= 0 {
		lineHeight = 15
	}
	// A large tile shows one caption line at its bottom, like the focal tiles
	// of the original start screen.
	maxLines := 2
	if tile.Large {
		maxLines = 1
	}
	lines := s.captionLines(entry.Name, cell.W-2*margin, maxLines)
	iconSize := minInt(cell.W*2/5, cell.H*2/5)
	if iconSize > 96 {
		iconSize = 96
	}
	captionHeight := len(lines) * lineHeight
	available := cell.H - 2*margin
	if tile.Large {
		// Leave room for the caption at the bottom of the tile.
		available -= captionHeight
	}
	iconSize = minInt(iconSize, available-captionHeight)
	iconSize = minInt(iconSize, available)
	iconSize = minInt(iconSize, maxInt(s.wm.d.MaxImageSide(), 16))
	if iconSize < 12 {
		iconSize = minInt(12, available)
	}

	iconX := cell.X + (cell.W-iconSize)/2
	iconY := cell.Y + margin + maxInt((available-iconSize-captionHeight)/2, 0)
	if tile.Large {
		iconY = cell.Y + maxInt((cell.H-captionHeight-iconSize)/2, margin)
	}
	if img := s.iconFor(entry, iconSize); img != nil {
		if err := d.UploadImageOver(o.win, o.gc, img, iconX, iconY, fill); err != nil {
			s.wm.log.Debug("cannot draw a tile icon", "tile", entry.Name, "error", err)
		}
	}

	if o.text == nil || s.font == nil || len(lines) == 0 {
		return
	}
	o.text.SetColor(d, s.colors.Text)
	baseline := iconY + iconSize + lineHeight - 3
	if tile.Large {
		baseline = cell.Y + cell.H - margin - 2
	}
	for _, line := range lines {
		if baseline > cell.Bottom()-6 {
			break
		}
		w := s.font.Width(line)
		o.text.DrawText(d, cell.X+(cell.W-w)/2, baseline, line)
		baseline += lineHeight
	}
}

// captionLines wraps a tile caption to the pixel width of a tile. The
// character-count estimate of desk.Label is only a starting point: the real
// font decides how much actually fits.
func (s *startScreen) captionLines(name string, maxWidth, maxLines int) []string {
	if maxLines < 1 {
		maxLines = 1
	}
	if s.font == nil || maxWidth <= 8 {
		return desk.Label(name, maxLines, maxInt(maxWidth/8, 4))
	}
	for n := maxInt(maxWidth/6, 10); n >= 6; n -= 2 {
		lines := desk.Label(name, maxLines, n)
		if s.widest(lines) <= maxWidth {
			return lines
		}
	}
	return []string{s.font.Truncate(name, maxWidth)}
}

// widest returns the width of the longest line.
func (s *startScreen) widest(lines []string) int {
	if s.font == nil {
		return 0
	}
	maxWidth := 0
	for _, l := range lines {
		if w := s.font.Width(l); w > maxWidth {
			maxWidth = w
		}
	}
	return maxWidth
}

// paintPages draws the page dots at the bottom of the screen.
func (s *startScreen) paintPages(o *overlay) {
	if o.grid.Pages < 2 {
		return
	}
	d := s.wm.d
	const (
		inactive = 6
		active   = 12
		gap      = 14
	)
	total := 0
	for i := 0; i < o.grid.Pages; i++ {
		size := inactive
		if i == s.page {
			size = active
		}
		total += size
		if i > 0 {
			total += gap
		}
	}
	x := o.area.X + (o.area.W-total)/2
	y := o.area.Bottom() - active - 12
	for i := 0; i < o.grid.Pages; i++ {
		c := s.colors.TextDim
		size := inactive
		if i == s.page {
			c = s.colors.Accent
			size = active
		}
		d.SetForegroundOf(o.gc, s.pixel(c))
		d.FillRectangle(o.win, o.gc, x, y, size, size)
		x += size + gap
	}
}

// iconFor returns the tile icon: the launcher or image icon when one can be
// resolved, and a coloured letter otherwise.
func (s *startScreen) iconFor(entry desk.Entry, size int) *image.RGBA {
	// Files that carry a picture, a video or a sound get a real preview. The
	// queue builds it in the background, so a tile never waits for ffmpeg.
	if s.wm != nil && s.wm.queue != nil && previewWorthy(entry) {
		side := size
		if side < 24 {
			side = 24
		}
		if side > 256 {
			side = 256
		}
		if img := s.wm.queue.Get(entry.Path, side); img != nil {
			return img
		}
	}
	if path, ok := imagex.LoadIcon(entry.Icon, size); ok {
		if img, err := s.icons.Load(path, size); err == nil {
			return img
		}
	}
	if imagex.HasImageExtension(entry.Path) {
		if img, err := s.icons.Load(entry.Path, size); err == nil {
			return img
		}
	}
	return s.glyph(entry, size)
}

// previewWorthy reports whether a tile is worth a real preview: anything that
// is not a launcher or a folder.
func previewWorthy(entry desk.Entry) bool {
	switch entry.Kind {
	case desk.KindApp, desk.KindShortcut, desk.KindDir, desk.KindLive:
		return false
	}
	return entry.Path != ""
}

// glyph draws the placeholder tile icon: a rounded block with the first
// letter of the name, coloured by kind.
func (s *startScreen) glyph(entry desk.Entry, size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	colour := s.colors.GlyphFile
	switch entry.Kind {
	case desk.KindApp:
		colour = s.colors.GlyphApp
	case desk.KindDir:
		colour = s.colors.GlyphDir
	case desk.KindShortcut:
		colour = s.colors.GlyphLink
	}

	// A rounded square with a soft top-light edge.
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

func insideRounded(x, y, w, h, r int) bool {
	if r < 1 {
		return true
	}
	cx := minInt(minInt(x, w-1-x), r)
	cy := minInt(minInt(y, h-1-y), r)
	if cx < r && cy < r {
		dx, dy := r-cx, r-cy
		return dx*dx+dy*dy <= r*r
	}
	return true
}

// dim darkens a colour towards black.
func dim(c color.RGBA, f float64) color.RGBA {
	mix := func(v uint8) uint8 { return uint8(float64(v) * f) }
	return color.RGBA{R: mix(c.R), G: mix(c.G), B: mix(c.B), A: c.A}
}

func lighten(c color.RGBA, f float64) color.RGBA {
	mix := func(v uint8) uint8 {
		return uint8(float64(v) + (255-float64(v))*f)
	}
	return color.RGBA{R: mix(c.R), G: mix(c.G), B: mix(c.B), A: c.A}
}

func (s *startScreen) pixel(c color.RGBA) uint32 {
	return s.wm.d.PixelFromRGB(uint32(c.R), uint32(c.G), uint32(c.B))
}

// EntryAt returns the entry under a root coordinate.
func (s *startScreen) EntryAt(x, y int) (desk.Entry, int, bool) {
	for _, o := range s.overlays {
		if !o.area.Contains(x, y) {
			continue
		}
		tile := layout.Hit(o.grid.Tiles(s.page), x, y)
		if tile == nil || tile.Index >= len(s.entries) {
			return desk.Entry{}, 0, false
		}
		return s.entries[tile.Index], tile.Index, true
	}
	return desk.Entry{}, 0, false
}

// Select moves the selection by delta, wrapping around the whole list.
func (s *startScreen) Select(delta int) {
	total := len(s.entries)
	if total == 0 {
		return
	}
	s.selectTile(layout.Next(s.selection, delta, total))
}

// MoveSelection moves the selection one tile in a direction, following the
// grouped layout so that large tiles and page breaks behave the way the eye
// expects.
func (s *startScreen) MoveSelection(dx, dy int) {
	if len(s.entries) == 0 || len(s.overlays) == 0 {
		return
	}
	g := s.overlays[0].grid
	tiles := g.Tiles(s.page)
	var current *layout.Tile
	for i := range tiles {
		if tiles[i].Index == s.selection {
			current = &tiles[i]
			break
		}
	}
	if current == nil {
		// The selection lives on another page or is gone: start from the
		// tile under the pointer when there is one.
		if hit := layout.Hit(tiles, s.overlays[0].area.X+s.currentX, s.overlays[0].area.Y+s.currentY); hit != nil {
			current = hit
		} else {
			s.selectTile(0)
			return
		}
	}
	if next := layout.Move(tiles, current, dx, dy); next != nil {
		s.selectTile(next.Index)
		return
	}
	// At the edge of the page: continue on the next or previous one.
	if dy == 0 {
		return
	}
	page := s.page
	if dy < 0 {
		page--
	} else {
		page++
	}
	if page < 0 {
		page = g.PageCount() - 1
	}
	if page >= g.PageCount() {
		page = 0
	}
	next := g.Tiles(page)
	if len(next) == 0 {
		return
	}
	pick := 0
	if dy < 0 {
		pick = len(next) - 1
	}
	s.page = page
	s.selectTile(next[pick].Index)
}

func (s *startScreen) selectTile(idx int) {
	if idx < 0 || idx >= len(s.entries) {
		return
	}
	s.selection = idx
	s.page = s.pageFor(idx)
	s.layout()
	s.currentX, s.currentY = 0, 0
	s.repaint()
}

// pageFor returns the page holding an entry.
func (s *startScreen) pageFor(index int) int {
	if len(s.overlays) == 0 {
		return 0
	}
	page := s.overlays[0].grid.PageOf(index)
	if pages := s.overlays[0].grid.PageCount(); page >= pages {
		page = pages - 1
	}
	if page < 0 {
		page = 0
	}
	return page
}

// Page switches pages by delta.
func (s *startScreen) Page(delta int) {
	if len(s.overlays) == 0 {
		return
	}
	g := s.overlays[0].grid
	pages := g.PageCount()
	if pages == 0 {
		return
	}
	s.page = ((s.page+delta)%pages + pages) % pages
	tiles := g.Tiles(s.page)
	if len(tiles) > 0 {
		s.selection = tiles[0].Index
	}
	s.currentX, s.currentY = 0, 0
	s.repaint()
}

// OpenSelected launches the selected entry and closes the start screen.
func (s *startScreen) OpenSelected() {
	if s.selection < 0 || s.selection >= len(s.entries) {
		return
	}
	entry := s.entries[s.selection]
	if entry.Widget != "" {
		// A live tile opens across the screen, the way the focal tiles of the
		// original start screen did.
		s.Close()
		if err := s.wm.openWidgetFullscreen(entry.Widget); err != nil {
			s.wm.log.Warn("cannot open a widget", "widget", entry.Widget, "error", err)
		}
		return
	}
	s.Close()
	go func() {
		if err := entry.Launch(); err != nil {
			s.wm.log.Warn("cannot open a desktop entry", "entry", entry.Name, "error", err)
		}
	}()
}

// HandleKey feeds a key press to the start screen and reports whether it was
// consumed.
func (s *startScreen) HandleKey(e x11.KeyPress, binding x11.Binding) bool {
	sym := x11.KeySymName(binding.Sym)
	switch sym {
	case "Escape":
		s.Close()
		return true
	case "Return", "KP_Enter", "space":
		s.OpenSelected()
		return true
	case "Left":
		s.MoveSelection(-1, 0)
		return true
	case "Right":
		s.MoveSelection(1, 0)
		return true
	case "Up":
		s.MoveSelection(0, -1)
		return true
	case "Down":
		s.MoveSelection(0, 1)
		return true
	case "Prior", "Next":
		if sym == "Prior" {
			s.Page(-1)
		} else {
			s.Page(1)
		}
		return true
	case "Home":
		s.selectTile(0)
		return true
	case "End":
		s.selectTile(maxInt(len(s.entries)-1, 0))
		return true
	}
	if len(sym) == 1 && (sym[0] >= 'a' && sym[0] <= 'z') {
		s.jumpToLetter(sym)
		return true
	}
	return false
}

// jumpToLetter selects the first entry whose name starts with a letter, which
// is how the original start screen behaved.
func (s *startScreen) jumpToLetter(letter string) {
	want := strings.ToLower(letter)
	for i, e := range s.entries {
		if strings.HasPrefix(strings.ToLower(e.Name), want) {
			s.selectTile(i)
			return
		}
	}
}

// HandleButton handles a press on a tile: the first press selects, a second
// one launches.
func (s *startScreen) HandleButton(e x11.ButtonPress, rootX, rootY int) {
	entry, index, ok := s.EntryAt(rootX, rootY)
	if !ok {
		s.Close()
		return
	}
	now := time.Now()
	doubleClick := index == s.lastIndex && now.Sub(s.lastClick) < 400*time.Millisecond
	s.lastIndex, s.lastClick = index, now

	if doubleClick {
		s.selection = index
		s.OpenSelected()
		return
	}
	if entry.Widget != "" {
		// A single click is enough for a live tile: it is already running.
		s.selection = index
		s.OpenSelected()
		return
	}
	s.selectTile(index)
	if len(s.overlays) > 0 {
		s.currentX, s.currentY = rootX-s.overlays[0].area.X, rootY-s.overlays[0].area.Y
	}
	s.wm.log.Debug("tile selected", "entry", entry.Name)
}

// HandleExpose repaints one overlay.
func (s *startScreen) HandleExpose(win x11.Window) {
	for _, o := range s.overlays {
		if o.win == win {
			s.paintOverlay(o)
			s.wm.d.Sync()
			return
		}
	}
}

// HandleMotion highlights the tile under the pointer.
func (s *startScreen) HandleMotion(rootX, rootY int) {
	_, index, ok := s.EntryAt(rootX, rootY)
	if !ok || index == s.selection {
		return
	}
	s.selection = index
	if len(s.overlays) > 0 {
		s.currentX, s.currentY = rootX-s.overlays[0].area.X, rootY-s.overlays[0].area.Y
	}
	s.repaint()
}

// Focusable reports whether the start screen currently wants the keyboard,
// which is the case whenever it is open.
func (s *startScreen) Focusable() bool { return s.open }

// Status renders a short summary for the IPC layer.
func (s *startScreen) Status() map[string]string {
	return map[string]string{
		"open":      fmt.Sprintf("%t", s.open),
		"dir":       s.dir,
		"entries":   fmt.Sprintf("%d", len(s.entries)),
		"page":      fmt.Sprintf("%d", s.page),
		"selection": fmt.Sprintf("%d", s.selection),
	}
}

func (s *startScreen) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "start screen: %s\n", map[bool]string{true: "open", false: "closed"}[s.open])
	fmt.Fprintf(&b, "desktop: %s\n", s.dir)
	if s.scanErr != nil {
		fmt.Fprintf(&b, "scan error: %v\n", s.scanErr)
	}
	fmt.Fprintf(&b, "entries: %d in %d groups\n", len(s.entries), len(s.groups))
	for _, g := range s.groups {
		fmt.Fprintf(&b, "  %-14s %d\n", g.Title, g.Count)
	}
	if s.open && len(s.overlays) > 0 {
		g := s.overlays[0].grid
		fmt.Fprintf(&b, "page %d/%d, %d columns, selection %d\n",
			s.page+1, g.PageCount(), g.Columns, s.selection)
	}
	return b.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if lo > hi {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

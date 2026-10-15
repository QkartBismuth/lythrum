package wm

import (
	"fmt"
	"strings"
	"time"

	"lythrum/internal/desk"
	"lythrum/internal/layout"
	"lythrum/internal/search"
	"lythrum/internal/x11"
)

// searchScreen is the Super+S bar: a single line of text over a dimmed
// desktop that offers applications and files as the user types.
type searchScreen struct {
	wm *WM

	win  x11.Window
	gc   x11.GC
	text *x11.FontDraw
	area layout.Rect

	query   string
	results []search.Result
	cursor  int
	apps    []search.Item
	files   []search.Item
	loaded  bool
	opened  time.Time
}

// searchMaxResults is how many rows the bar shows at once.
const searchMaxResults = 7

func newSearchScreen(w *WM) *searchScreen {
	return &searchScreen{wm: w, apps: searchApplications(), files: searchFiles()}
}

// searchApplications builds the application part of the index.
func searchApplications() []search.Item {
	items := make([]search.Item, 0, 128)
	for _, entry := range desk.Applications() {
		items = append(items, search.Item{
			Name: entry.Name, Path: entry.Path, Kind: "application", KindRank: 30,
		})
	}
	return items
}

// searchFiles builds the file part of the index.
func searchFiles() []search.Item {
	entries := desk.SearchFiles(800)
	items := make([]search.Item, 0, len(entries))
	for _, entry := range entries {
		items = append(items, search.Item{
			Name: entry.Name, Path: entry.Path, Kind: "file", KindRank: 10,
		})
	}
	return items
}

// Toggle shows the bar, or hides it when it is already open.
func (s *searchScreen) Toggle() error {
	if s.win != 0 {
		s.Close()
		return nil
	}
	return s.Open()
}

// Open shows the bar and grabs the keyboard.
func (s *searchScreen) Open() error {
	if s.win != 0 {
		s.repaint()
		return nil
	}
	s.query = ""
	s.cursor = 0
	s.opened = time.Now()
	area := s.wm.workArea(0)
	if len(s.wm.monitors) == 0 {
		return fmt.Errorf("wm: no monitor to show the search on")
	}
	s.area = area
	win, err := s.wm.d.CreateWindow(x11.CreateWindowParams{
		Parent:           s.wm.d.Root(),
		X:                area.X,
		Y:                area.Y,
		Width:            uint(area.W),
		Height:           uint(area.H),
		Class:            x11.ClassInputOutput,
		BackPixel:        0x0d1117ff,
		OverrideRedirect: true,
		EventMask:        x11.ExposureMask | x11.KeyPressMask | x11.StructureNotifyMask,
	})
	if err != nil {
		return err
	}
	s.win = win
	gc, err := s.wm.d.CreateGC(win)
	if err != nil {
		s.wm.d.DestroyWindow(win)
		s.win = 0
		return err
	}
	s.gc = gc
	if draw, err := s.wm.d.NewFontDraw(win, s.wm.font()); err == nil {
		s.text = draw
	}
	s.wm.d.MapWindow(win)
	s.wm.d.RaiseWindow(win)
	s.wm.d.Focus(win, x11.RevertToPointerRoot, 0)
	if err := s.wm.d.GrabInput(s.win, x11.PointerMotionMask|x11.ButtonPressMask, x11.KeyPressMask); err != nil {
		s.wm.log.Debug("cannot grab the input for the search bar", "error", err)
	}
	s.refresh()
	s.repaint()
	s.wm.log.Info("search opened")
	return nil
}

// Close hides the bar and gives the keyboard back.
func (s *searchScreen) Close() {
	if s.win == 0 {
		return
	}
	s.wm.d.UngrabInput()
	if s.text != nil {
		s.text.Close(s.wm.d)
		s.text = nil
	}
	if s.gc.Valid() {
		s.wm.d.FreeGC(s.gc)
	}
	s.wm.d.DestroyWindow(s.win)
	s.win = 0
	s.wm.d.Focus(s.wm.d.Root(), x11.RevertToPointerRoot, 0)
	s.wm.restoreFocus()
}

// Open reports whether the bar is on screen.
func (s *searchScreen) Open2() bool { return s.win != 0 }

func (s *searchScreen) refresh() {
	all := make([]search.Item, 0, len(s.apps)+len(s.files))
	all = append(all, s.apps...)
	all = append(all, s.files...)
	s.results = search.Rank(s.query, all, searchMaxResults)
	if s.cursor >= len(s.results) {
		s.cursor = maxInt(len(s.results)-1, 0)
	}
}

func (s *searchScreen) repaint() {
	if s.win == 0 || !s.gc.Valid() {
		return
	}
	d := s.wm.d
	colors := gridPalette(s.wm.cfg)
	d.SetForegroundOf(s.gc, 0x000d1117)
	d.FillRectangle(s.win, s.gc, 0, 0, s.area.W, s.area.H)

	margin := maxInt(s.area.W/12, 40)
	barW := s.area.W - 2*margin
	barH := maxInt(s.area.H/12, 64)
	barX := margin
	barY := s.area.Y - s.area.Y + maxInt(s.area.H/8, 60)

	d.SetForegroundOf(s.gc, 0x00181d26)
	d.FillRectangle(s.win, s.gc, barX, barY, barW, barH)
	d.SetForegroundOf(s.gc, 0x002a3140)
	d.FillRectangle(s.win, s.gc, barX, barY, barW, 2)

	if s.text == nil {
		return
	}
	font := s.wm.font()
	label := s.query
	muted := label == ""
	if muted {
		label = "Поиск файлов и приложений…"
		s.text.SetColor(d, colors.TextDim)
	} else {
		s.text.SetColor(d, colors.Text)
	}
	shown := font.Truncate(label, barW-70)
	s.text.DrawText(d, barX+24, barY+barH/2+font.Height()/3, shown)
	// The caret is a small rectangle, which every font can draw.
	caretX := barX + 24 + font.Width(shown) + 2
	d.SetForegroundOf(s.gc, 0x006ee7ff)
	d.FillRectangle(s.win, s.gc, caretX, barY+barH/2-font.Height()/3, 2, font.Height())

	rowH := maxInt(font.LineHeight()+14, 30)
	for i, result := range s.results {
		y := barY + barH + 10 + i*rowH
		if y+rowH > s.area.H-20 {
			break
		}
		if i == s.cursor {
			d.SetForegroundOf(s.gc, 0x00222b38)
			d.FillRectangle(s.win, s.gc, barX, y, barW, rowH-4)
		}
		s.text.SetColor(d, colors.Text)
		s.text.DrawText(d, barX+24, y+rowH/2+font.Height()/3, font.Truncate(result.Name, barW-260))
		if result.Item.Kind == "application" {
			s.text.SetColor(d, colors.TextDim)
			s.text.DrawText(d, barX+barW-220, y+rowH/2+font.Height()/3, "приложение")
		} else {
			s.text.SetColor(d, colors.TextDim)
			s.text.DrawText(d, barX+barW-220, y+rowH/2+font.Height()/3,
				font.Truncate(dirOf(result.Path), 200))
		}
	}
	d.Flush()
}

func dirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i > 0 {
		return strings.TrimPrefix(path[:i], "/home/")
	}
	return ""
}

// HandleKey feeds one key press to the bar and reports whether it was used.
func (s *searchScreen) HandleKey(e x11.KeyPress, binding x11.Binding) bool {
	if s.win == 0 {
		return false
	}
	sym := x11.KeySymName(binding.Sym)
	switch sym {
	case "Escape":
		s.Close()
		return true
	case "Return", "KP_Enter":
		s.activate()
		return true
	case "Down":
		if s.cursor < len(s.results)-1 {
			s.cursor++
		}
		s.repaint()
		return true
	case "Up":
		if s.cursor > 0 {
			s.cursor--
		}
		s.repaint()
		return true
	case "BackSpace":
		if s.query != "" {
			runes := []rune(s.query)
			s.query = string(runes[:len(runes)-1])
			s.cursor = 0
			s.refresh()
			s.repaint()
		}
		return true
	}
	if sym == "u" && (binding.Mods&x11.ControlMask) != 0 {
		s.query = ""
		s.cursor = 0
		s.refresh()
		s.repaint()
		return true
	}
	if text := x11.KeySymText(binding.Sym); text != "" {
		s.query += text
		s.cursor = 0
		s.refresh()
		s.repaint()
		return true
	}
	return false
}

// activate opens what the cursor is on.
func (s *searchScreen) activate() {
	if s.cursor < 0 || s.cursor >= len(s.results) {
		s.Close()
		return
	}
	result := s.results[s.cursor]
	path := result.Path
	s.Close()
	go func() {
		entry, err := desk.EntryAt(path)
		if err != nil {
			s.wm.log.Warn("cannot open the search result", "path", path, "error", err)
			return
		}
		if err := entry.Launch(); err != nil {
			s.wm.log.Warn("cannot launch", "path", path, "error", err)
		}
	}()
}

// Visible reports whether the bar is on screen.
func (s *searchScreen) Visible() bool { return s.win != 0 }

// HandleExpose repaints after the window was uncovered.
func (s *searchScreen) HandleExpose(win x11.Window) {
	if win == s.win {
		s.repaint()
		s.wm.d.Sync()
	}
}

// font returns the font the bar draws with.
func (w *WM) font() *x11.Font {
	if w.start != nil && w.start.font != nil {
		return w.start.font
	}
	return nil
}

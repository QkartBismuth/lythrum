package wm

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"lythrum/internal/desk"
	"lythrum/internal/layout"
	"lythrum/internal/webview"
	"lythrum/internal/widget"
	"lythrum/internal/x11"
)

// liveViews keeps the off screen web views of live tiles. Only tiles the user
// can see are allowed to run, and the number of live views is capped so a
// start screen full of widgets does not eat the machine.
type liveViews struct {
	wm      *WM
	mu      sync.Mutex
	views   map[string]*liveView
	order   []string
	limit   int
	fps     int
	stopped bool
}

// liveView is one widget page bound to a tile size.
type liveView struct {
	view   *webview.View
	widget string
	width  int
	height int
	used   time.Time
	fps    int
}

func newLiveViews(w *WM, limit, fps int) *liveViews {
	if limit <= 0 {
		limit = 6
	}
	if fps <= 0 {
		fps = 12
	}
	return &liveViews{wm: w, views: map[string]*liveView{}, limit: limit, fps: fps}
}

// key identifies a view by widget id and tile size, so a tile that changes
// size gets a view that matches it.
func liveKey(widgetID string, rect layout.Rect) string {
	return fmt.Sprintf("%s@%dx%d", widgetID, rect.W, rect.H)
}

// Show makes sure a widget is rendering at the given size and returns its most
// recent frame. A tile that scrolls out of view keeps its view but stops it.
func (l *liveViews) Show(widgetID string, rect layout.Rect) (webview.Frame, bool) {
	if l == nil || l.stopped || widgetID == "" {
		return webview.Frame{}, false
	}
	key := liveKey(widgetID, rect)
	l.mu.Lock()
	defer l.mu.Unlock()
	live, ok := l.views[key]
	if !ok {
		if _, known := l.wm.widgets.Lookup(widgetID); !known {
			return webview.Frame{}, false
		}
		if len(l.views) >= l.limit {
			l.evict()
		}
		created, err := l.create(widgetID, rect)
		if err != nil {
			l.wm.log.Warn("cannot start a live tile", "widget", widgetID, "error", err)
			return webview.Frame{}, false
		}
		live = created
		l.views[key] = live
		l.order = append(l.order, key)
	}
	live.used = time.Now()
	// Every widget asks for as many frames as it needs, and the setting is the
	// upper limit, so a clock never costs what a slideshow needs.
	fps := l.fps
	if info, known := l.wm.widgets.Lookup(widgetID); known && info.FPS > 0 && info.FPS < fps {
		fps = info.FPS
	}
	if live.fps != fps {
		live.view.SetFPS(fps)
		live.fps = fps
	}
	frame := live.view.Frame()
	return frame, frame.Pixels != nil
}

// Hide stops every view, which the shell does when the start screen closes.
func (l *liveViews) Hide() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, live := range l.views {
		live.view.SetFPS(0)
		live.fps = 0
	}
}

// describe reports the state of every live view, which makes the shell easy to
// inspect while developing.
func (l *liveViews) describe() map[string]any {
	out := map[string]any{}
	if l == nil {
		return out
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, live := range l.views {
		frame := live.view.Frame()
		out[key] = map[string]any{
			"size":   fmt.Sprintf("%dx%d", frame.Width, frame.Height),
			"shots":  live.view.Shots(),
			"loaded": live.view.Loaded(),
			"fps":    live.fps,
		}
	}
	return out
}

// Stop closes every view.
func (l *liveViews) Stop() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, live := range l.views {
		live.view.Close()
		delete(l.views, key)
	}
	l.order = nil
	l.stopped = true
}

// evict closes the least recently shown view.
func (l *liveViews) evict() {
	oldest, at := "", time.Time{}
	for _, key := range l.order {
		live, ok := l.views[key]
		if !ok {
			continue
		}
		if oldest == "" || live.used.Before(at) {
			oldest, at = key, live.used
		}
	}
	if oldest == "" {
		return
	}
	if live, ok := l.views[oldest]; ok {
		live.view.Close()
		delete(l.views, oldest)
	}
	kept := l.order[:0]
	for _, key := range l.order {
		if key != oldest {
			kept = append(kept, key)
		}
	}
	l.order = kept
}

func (l *liveViews) create(widgetID string, rect layout.Rect) (*liveView, error) {
	w := l.wm.webEngine()
	if w == nil {
		if l.wm.webErr != nil {
			return nil, l.wm.webErr
		}
		return nil, webview.ErrNoDisplay
	}
	fps := l.fps
	if info, known := l.wm.widgets.Lookup(widgetID); known && info.FPS > 0 && info.FPS < fps {
		fps = info.FPS
	}
	// The first frames arrive faster at a low rate and settle afterwards, so
	// a tile fills in quickly without keeping the browser busy.
	view, err := w.NewViewURI(rect.W, rect.H, 2, widget.URL(widgetID))
	if err != nil {
		return nil, err
	}
	cfg := l.wm.config()
	theme := map[string]string{
		"background": cfg.GridBackground,
		"accent":     cfg.GridAccent,
		"text":       cfg.GridText,
	}
	view.Eval(fmt.Sprintf("window.lythrumBoot && window.lythrumBoot(%s)",
		jsonString(theme)))
	return &liveView{view: view, widget: widgetID, width: rect.W, height: rect.H, fps: 2}, nil
}

func jsonString(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// widgetEntries returns the built-in live tiles, ready to be mixed into the
// start screen groups.
func widgetEntries(reg *widget.Registry) []liveEntry {
	var out []liveEntry
	for _, w := range reg.All() {
		out = append(out, liveEntry{
			ID:    w.ID,
			Title: w.Title,
			Group: w.Group,
		})
	}
	return out
}

type liveEntry struct {
	ID    string
	Title string
	Group string
}

// newWidgetRegistry builds the widget registry with a session wired to the
// shell.
func (w *WM) newWidgetRegistry() *widget.Registry {
	return widget.NewRegistry(widget.Session{
		Now: time.Now,
		Config: func() ([]byte, error) {
			return json.Marshal(w.config())
		},
		SetConfig: func(raw json.RawMessage) ([]byte, error) {
			return w.queueConfig(raw)
		},
		OpenURL: func(target string) {
			if target == "" {
				return
			}
			go func() {
				if err := runExternal(target); err != nil {
					w.log.Warn("cannot open", "target", target, "error", err)
				}
			}()
		},
		Info: func() map[string]any {
			cfg := w.config()
			list := w.monitorsSnapshot()
			monitors := make([]string, 0, len(list))
			for _, m := range list {
				monitors = append(monitors, fmt.Sprintf("%s %dx%d", m.Name, m.W, m.H))
			}
			return map[string]any{
				"layout":           string(w.layoutKind()),
				"clients":          w.clientCount(),
				"desktops":         cfg.Desktops,
				"desktop":          int(w.root.CurrentDesktop()) + 1,
				"monitors":         monitors,
				"uptime":           time.Since(w.started).Round(time.Second).String(),
				"wallpaper":        cfg.WallpaperImage,
				"wallpaperEnabled": cfg.WallpaperEnabled,
				"session":          os.Getenv("XDG_SESSION_ID"),
				"sessionType":      w.session.String(),
				"sessionNote":      w.session.Summary(),
				"configPath":       w.configPath,
				"keybinds":         len(cfg.Keybinds),
			}
		},
		Actions: func() []widget.Action {
			return shellActions()
		},
		Theme: func() map[string]string {
			cfg := w.config()
			return map[string]string{
				"background": cfg.GridBackground,
				"tile":       cfg.GridTile,
				"accent":     cfg.GridAccent,
				"text":       cfg.GridText,
			}
		},
	})
}

// addWidgetEntries puts the built-in live tiles in front of the desktop
// entries, unless the desktop already provides launchers for them.
func (s *startScreen) addWidgetEntries(entries []desk.Entry) []desk.Entry {
	if s.wm == nil || s.wm.widgets == nil {
		return entries
	}
	present := map[string]bool{}
	for _, e := range entries {
		if e.Widget != "" {
			present[e.Widget] = true
		}
	}
	var out []desk.Entry
	for _, w := range s.wm.widgets.All() {
		if present[w.ID] || !s.wm.cfg.GridWidgets {
			continue
		}
		out = append(out, desk.Entry{
			Name:   w.Title,
			Path:   widget.URL(w.ID),
			Kind:   desk.KindLive,
			Widget: w.ID,
			Icon:   w.Accent,
		})
	}
	if len(out) == 0 {
		return entries
	}
	return append(out, entries...)
}

// openWidgetFullscreen shows a widget across one monitor until it is closed.
// The page is painted by the shell and the input is forwarded into it, so a
// widget is usable even though it renders off screen.
func (w *WM) openWidgetFullscreen(widgetID string) error {
	engine := w.webEngine()
	if engine == nil {
		if w.webErr != nil {
			return w.webErr
		}
		return webview.ErrNoDisplay
	}
	monitor := w.monitors[0]
	rect := layout.Rect{X: monitor.X, Y: monitor.Y, W: monitor.W, H: monitor.H}
	fps := w.cfg.GridLiveFPS
	if fps <= 0 {
		fps = 12
	}
	if info, ok := w.widgets.Lookup(widgetID); ok && info.FPS > 0 && info.FPS < fps {
		fps = info.FPS
	}
	view, err := engine.NewViewURI(rect.W, rect.H, fps, widget.URL(widgetID))
	if err != nil {
		return err
	}
	win, gc, err := w.makeWidgetWindow(rect)
	if err != nil {
		view.Close()
		return err
	}
	session := &widgetSession{view: view, win: win, gc: gc, area: rect, wm: w, id: widgetID}
	session.start()
	return nil
}

// widgetSession paints a fullscreen widget until it is closed. The painting
// happens from the event loop, because the X connection belongs to it.
type widgetSession struct {
	view *webview.View
	win  x11.Window
	gc   x11.GC
	area layout.Rect
	wm   *WM
	id   string
	once sync.Once
}

func (s *widgetSession) start() {
	s.wm.addWidgetSession(s)
}

// paint draws the newest frame into the window.
func (s *widgetSession) paint() {
	frame := s.view.Frame()
	if frame.Pixels == nil {
		return
	}
	if err := s.wm.d.UploadImageOver(s.win, s.gc, frame.Image(), 0, 0,
		color.RGBA{R: 0x10, G: 0x10, B: 0x14, A: 0xff}); err != nil {
		s.wm.log.Debug("cannot paint a fullscreen widget", "widget", s.id, "error", err)
	}
}

// activeSession returns the fullscreen widget that currently owns the
// keyboard. The page draws in its own window and that window may hold the X
// focus even though the painted overlay is what the user sees, so the keyboard
// cannot be matched by the event window.
func (w *WM) activeSession() *widgetSession {
	w.sessionsMu.Lock()
	defer w.sessionsMu.Unlock()
	if len(w.sessions) == 0 {
		return nil
	}
	return w.sessions[len(w.sessions)-1]
}

// Click forwards a click in root coordinates into the page.
func (s *widgetSession) Click(rootX, rootY int) {
	s.view.Click(rootX-s.area.X, rootY-s.area.Y)
}

// Hover forwards pointer movement into the page.
func (s *widgetSession) Hover(rootX, rootY int) {
	s.view.Hover(rootX-s.area.X, rootY-s.area.Y)
}

// Key forwards a key press into the page.
func (s *widgetSession) Key(name, text string) { s.view.Key(name, text) }

// Close stops a fullscreen widget.
func (s *widgetSession) Close() {
	s.once.Do(func() {
		s.wm.removeWidgetSession(s)
		s.view.Close()
		if s.gc.Valid() {
			s.wm.d.FreeGC(s.gc)
		}
		if s.win != 0 {
			s.wm.d.DestroyWindow(s.win)
		}
		if s.wm.start != nil && !s.wm.start.open {
			_ = s.wm.start.Open()
		}
	})
}

// addWidgetSession registers a fullscreen widget with the shell.
func (w *WM) addWidgetSession(s *widgetSession) {
	w.sessionsMu.Lock()
	w.sessions = append(w.sessions, s)
	w.sessionsMu.Unlock()
}

func (w *WM) removeWidgetSession(target *widgetSession) {
	w.sessionsMu.Lock()
	kept := w.sessions[:0]
	for _, s := range w.sessions {
		if s != target {
			kept = append(kept, s)
		}
	}
	w.sessions = kept
	w.sessionsMu.Unlock()
}

// paintWidgetSessions refreshes every fullscreen widget.
func (w *WM) paintWidgetSessions() {
	w.sessionsMu.Lock()
	list := append([]*widgetSession(nil), w.sessions...)
	w.sessionsMu.Unlock()
	for _, s := range list {
		s.paint()
	}
}

// closeTopWidget closes the fullscreen widget that is on screen, if any.
func (w *WM) closeTopWidget() {
	w.sessionsMu.Lock()
	if len(w.sessions) == 0 {
		w.sessionsMu.Unlock()
		return
	}
	session := w.sessions[len(w.sessions)-1]
	w.sessionsMu.Unlock()
	session.Close()
}

// widgetSessionFor returns the fullscreen widget a window belongs to.
func (w *WM) widgetSessionFor(win x11.Window) *widgetSession {
	w.sessionsMu.Lock()
	defer w.sessionsMu.Unlock()
	for _, s := range w.sessions {
		if s.win == win {
			return s
		}
	}
	return nil
}

// runExternal hands a target to the desktop handler.
func runExternal(target string) error {
	launcher := "xdg-open"
	if _, err := os.Stat("/usr/bin/xdg-open"); err != nil {
		launcher = "gio"
	}
	args := []string{launcher, target}
	if launcher == "gio" {
		args = []string{"gio", "open", target}
	}
	return exec.Command(args[0], args[1:]...).Start()
}

// previewContext returns the context background work runs under.
func (w *WM) previewContext() context.Context {
	if w.previewCtx == nil {
		return context.Background()
	}
	return w.previewCtx
}

// previewCacheDir is where previews are kept between runs.
func previewCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "lythrum", "previews")
	}
	return ""
}

// makeWidgetWindow creates the overlay a fullscreen widget paints into.
func (w *WM) makeWidgetWindow(rect layout.Rect) (x11.Window, x11.GC, error) {
	win, err := w.d.CreateWindow(x11.CreateWindowParams{
		Parent:           w.d.Root(),
		X:                rect.X,
		Y:                rect.Y,
		Width:            uint(maxInt(rect.W, 1)),
		Height:           uint(maxInt(rect.H, 1)),
		Class:            x11.ClassInputOutput,
		BackPixel:        0x00101014,
		OverrideRedirect: true,
		EventMask: x11.ExposureMask | x11.KeyPressMask | x11.StructureNotifyMask |
			x11.ButtonPressMask | x11.ButtonReleaseMask | x11.PointerMotionMask,
	})
	if err != nil {
		return 0, x11.GC{}, err
	}
	gc, err := w.d.CreateGC(win)
	if err != nil {
		w.d.DestroyWindow(win)
		return 0, x11.GC{}, err
	}
	w.d.MapWindow(win)
	w.d.RaiseWindow(win)
	w.d.Focus(win, x11.RevertToPointerRoot, 0)
	return win, gc, nil
}

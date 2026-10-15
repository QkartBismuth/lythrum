// Package wm implements the window manager proper: it turns X clients into
// managed windows, keeps them framed, focused and arranged.
package wm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"lythrum/internal/config"
	"lythrum/internal/layout"
	"lythrum/internal/preview"
	"lythrum/internal/webview"
	"lythrum/internal/widget"
	"lythrum/internal/x11"
)

// ErrAlreadyRunning is returned when another window manager already owns the
// session.
var ErrAlreadyRunning = errors.New("wm: another window manager is already running on this display")

// WM is the window manager.
type WM struct {
	d    *x11.Display
	cfg  config.Config
	log  *slog.Logger
	root *x11.EWMHRoot
	// session records whether a Wayland compositor hosts the desktop.
	session Session

	mu      sync.Mutex
	clients []*Client
	byID    map[x11.Window]*Client
	focused *Client
	// order is the bottom-to-top stacking order of managed windows.
	order []x11.Window

	keybinds map[x11.Binding]string
	grabs    []x11.Binding

	// cfgMu guards cfg against the widget threads, which read the settings
	// while answering a page. Only the event loop writes cfg. layoutMu does the
	// same for the current layout, which the settings page also reports.
	cfgMu    sync.RWMutex
	layoutMu sync.RWMutex
	// pendingConfig carries validated settings from a widget to the event
	// loop, which is the only place allowed to re-grab keys, reload the
	// wallpaper and lay the windows out again.
	pendingConfig chan config.Config

	// search is the Super+S bar and gridColors keeps the start screen palette
	// so a settings change can recolour the shell without a restart.
	search *searchScreen
	// power is the Super+L menu with the session and machine actions.
	power      *powerScreen
	gridColors gridColors
	// mediaJobs carries the file work away from the browser thread, queue
	// builds the tile previews and sessions holds the fullscreen widgets.
	mediaJobs  chan func()
	queue      *previewQueue
	sessions   []*widgetSession
	sessionsMu sync.Mutex

	// paper paints the desktop background and previews builds file previews.
	paper         *wallpapers
	previews      *preview.Service
	previewCtx    context.Context
	previewCancel context.CancelFunc

	// widgets hosts the live tiles and live owns their off screen views.
	widgets *widget.Registry
	live    *liveViews
	web     *webview.Engine
	webOnce sync.Once
	webErr  error

	monitors   []layout.Monitor
	layout     layout.StackKind
	canvas     *canvas
	start      *startScreen
	desktop    *desktopLayer
	desk       *deskCache
	started    time.Time
	quit       chan struct{}
	quitOnce   sync.Once
	socket     string
	listener   net.Listener
	configPath string
	restarter  func()
}

// webEngine starts WebKit the first time a live tile needs it, so a session
// that never shows a widget pays nothing.
func (w *WM) webEngine() *webview.Engine {
	w.webOnce.Do(func() {
		engine, err := webview.Start()
		if err != nil {
			w.webErr = err
			w.log.Warn("live tiles are unavailable", "error", err)
			return
		}
		engine.SetLogger(func(line string) { w.log.Debug("webview", "message", line) })
		engine.SetSchemeHandler(func(req webview.SchemeRequest, reply func(webview.SchemeResponse, bool)) {
			// Reading a file from disk must not happen on the GTK thread, so
			// the work runs on the shell's own queue.
			w.mediaJobs <- func() { reply(w.serveScheme(req)) }
		})
		engine.SetMessageHandler(func(view *webview.View, msg webview.Message) {
			view.Dispatch(func() { w.answerWidget(view, msg) })
		})
		w.web = engine
	})
	return w.web
}

// widgetFilePrefix marks the addresses that stream local files.
const widgetFilePrefix = "lythrum://app/file/"

// serveScheme answers one lythrum:// request.
func (w *WM) serveScheme(req webview.SchemeRequest) (webview.SchemeResponse, bool) {
	if w.widgets == nil {
		return webview.SchemeResponse{}, false
	}
	// Media pages load their files straight from disk.
	if strings.HasPrefix(req.URL, widgetFilePrefix) {
		mime, body, status, contentRange, ok := w.widgets.ServeFile(req.URL, req.Range)
		if !ok {
			return webview.SchemeResponse{}, false
		}
		return webview.SchemeResponse{
			Mime: mime, Body: body, Status: status, ContentRange: contentRange,
		}, true
	}
	mime, body, ok := w.widgets.Serve(req.URL)
	if !ok {
		return webview.SchemeResponse{}, false
	}
	return webview.SchemeResponse{Mime: mime, Body: body}, true
}

// startMediaWorker runs the file work the widget pages ask for.
func (w *WM) startMediaWorker() {
	for job := range w.mediaJobs {
		job()
	}
}

// answerWidget runs a backend call and sends the answer back to the page.
func (w *WM) answerWidget(view *webview.View, msg webview.Message) {
	if w.widgets == nil {
		view.Eval(fmt.Sprintf("window.__lythrumResolve&&window.__lythrumResolve(%q,%s)",
			msg.ID, jsonString(map[string]string{"error": "виджеты недоступны"})))
		return
	}
	data, err := w.widgets.Call(msg.Method, msg.Params)
	if err != nil {
		w.log.Warn("widget call failed", "method", msg.Method, "error", err)
		view.Eval(fmt.Sprintf("window.__lythrumResolve&&window.__lythrumResolve(%q,%s)",
			msg.ID, jsonString(map[string]string{"error": err.Error()})))
		return
	}
	view.Eval(fmt.Sprintf("window.__lythrumResolve&&window.__lythrumResolve(%q,%s)", msg.ID, data))
}

// applyConfig pushes a new configuration into the running shell: colours,
// wallpaper and key bindings all change without a restart.
// config returns a copy of the settings, safe to read from any thread.
func (w *WM) config() config.Config {
	w.cfgMu.RLock()
	defer w.cfgMu.RUnlock()
	return w.cfg
}

// queueConfig takes a settings patch from a widget, checks it and hands the
// result to the event loop. The answer has to be immediate because the page
// waits for it, but applying a patch touches Xlib and the client maps, so only
// the event loop may do it.
func (w *WM) queueConfig(raw json.RawMessage) ([]byte, error) {
	next := w.config()
	if err := next.Merge(raw); err != nil {
		return nil, err
	}
	data, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	select {
	case w.pendingConfig <- next:
	default:
		return nil, fmt.Errorf("wm: the settings queue is full, try again")
	}
	return data, nil
}

// applyPendingConfig takes the settings the widgets handed over.
func (w *WM) applyPendingConfig() {
	for {
		select {
		case next := <-w.pendingConfig:
			w.cfgMu.Lock()
			w.cfg = next
			w.cfgMu.Unlock()
			w.applyConfig()
		default:
			return
		}
	}
}

func (w *WM) applyConfig() {
	w.gridColors = gridPalette(w.cfg)
	if w.start != nil {
		w.start.colors = w.gridColors
		w.start.rebuildPalette()
	}
	// Key bindings changed, so the old grabs have to go before new ones.
	for _, b := range w.grabs {
		w.d.UngrabKey(b, w.d.Root())
	}
	w.grabs = nil
	w.keybinds = make(map[x11.Binding]string)
	if err := w.grabKeybinds(); err != nil {
		w.log.Warn("cannot apply the new key bindings", "error", err)
	}
	if w.paper != nil {
		w.paper.Reload()
	}
	if w.desktop != nil {
		// The icons restack onto the wallpaper, which may have just been
		// replaced.
		w.desktop.Reload()
	}
	// Frame sizes, the tiling layout and the number of desktops change what is
	// on screen, so the shell has to lay the windows out again.
	w.setLayoutKind(w.cfg.LayoutKind())
	w.relayout()
	w.syncDesktops()
	w.log.Info("settings applied")
}

// syncDesktops pushes a changed number of desktops or a new set of names into
// the root window, so the settings page does not need a restart.
func (w *WM) syncDesktops() {
	if w.root == nil {
		return
	}
	names := w.cfg.DesktopNamesOrDefault()
	root := w.d.Root()
	if count := w.root.NumberOfDesktops(); count != uint32(w.cfg.Desktops) {
		if err := w.d.SetCardinal(root, x11.A.NETNumberOfDesktops,
			x11.A.Cardinal, uint32(w.cfg.Desktops)); err != nil {
			w.log.Debug("cannot set the number of desktops", "error", err)
		} else {
			w.root.SetDesktops(uint32(w.cfg.Desktops), names)
		}
	}
	w.d.SetProperty(root, x11.A.NETDesktopNames, x11.A.UTF8String, 8, desktopNames(names))
	if current := w.root.CurrentDesktop(); current >= uint32(w.cfg.Desktops) {
		w.SetDesktop(uint32(w.cfg.Desktops) - 1)
	}
}

// desktopNames joins the desktop names the way the EWMH property wants them.
func desktopNames(names []string) []byte {
	out := make([]byte, 0, len(names)*8)
	for _, name := range names {
		out = append(out, []byte(name)...)
		out = append(out, 0)
	}
	return out
}

// SetRestarter installs a callback used by the "restart" action.
func (w *WM) SetRestarter(fn func()) { w.restarter = fn }

// SetConfigPath records where the configuration was loaded from so it can be
// reloaded on demand.
func (w *WM) SetConfigPath(path string) { w.configPath = path }

// SetSession tells the shell what kind of session it runs in. The command sets
// it from the environment; the tests leave the zero value, which reads as X11.
func (w *WM) SetSession(s Session) { w.session = s }

// Session returns the session the shell is running in.
func (w *WM) Session() Session { return w.session }

// New creates a window manager on the given display.
func New(d *x11.Display, cfg config.Config, log *slog.Logger) (*WM, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	w := &WM{
		d:        d,
		cfg:      cfg,
		log:      log,
		byID:     make(map[x11.Window]*Client),
		keybinds: make(map[x11.Binding]string),
		layout:   cfg.LayoutKind(),
		quit:     make(chan struct{}),
		// A nil channel would make every settings change look like a full
		// queue, so it is created together with the shell.
		pendingConfig: make(chan config.Config, 4),
		started:       time.Now(),
	}
	w.root = x11.NewEWMHRoot(d)
	// The canvas has to exist before the windows already on the display are
	// adopted: they are placed in its coordinates.
	w.canvas = newCanvas(w)
	if err := w.setup(); err != nil {
		return nil, err
	}
	w.start = w.newStartScreen()
	w.search = newSearchScreen(w)
	w.power = newPowerScreen(w)
	w.desk = newDeskCache(w)
	w.desk.scan(true)
	// The desktop surface paints the wallpaper and the icons into one window,
	// so it has to exist before the wallpaper player hands it its first frame.
	w.desktop = newDesktopLayer(w)
	w.paper = newWallpapers(w)
	w.mediaJobs = make(chan func(), 128)
	go w.startMediaWorker()
	w.previewCtx, w.previewCancel = context.WithCancel(context.Background())
	w.previews = preview.NewService(previewCacheDir())
	w.queue = newPreviewQueue(w, w.previews)
	return w, nil
}

// Display returns the X connection.
func (w *WM) Display() *x11.Display { return w.d }

// Quitting is closed when the window manager should exit.
func (w *WM) Quitting() <-chan struct{} { return w.quit }

// Quit asks the event loop to stop.
func (w *WM) Quit() {
	w.quitOnce.Do(func() { close(w.quit) })
}

// setup prepares the root window and takes over the session.
func (w *WM) setup() error {
	d := w.d
	root := d.Root()

	// Becoming the window manager means selecting SubstructureRedirect on the
	// root; every other window manager would fight us for it.
	mask := x11.SubstructureRedirectMask | x11.SubstructureNotifyMask |
		x11.PropertyChangeMask | x11.KeyPressMask | x11.ButtonPressMask |
		x11.PointerMotionMask | x11.EnterWindowMask | x11.LeaveWindowMask
	d.SelectInput(root, mask)

	// Only one client can hold SubstructureRedirect on the root window. If
	// the selection did not take, another window manager owns the session and
	// starting would leave two managers fighting over every client.
	d.Sync()
	attrs, err := d.Attributes(root)
	if err != nil {
		return fmt.Errorf("wm: cannot read root window attributes: %w", err)
	}
	if attrs.YourEventMask&x11.SubstructureRedirectMask == 0 {
		return ErrAlreadyRunning
	}

	supported := []x11.Atom{
		x11.A.NETSupported, x11.A.NETClientList, x11.A.NETClientListStacking,
		x11.A.NETNumberOfDesktops, x11.A.NETCurrentDesktop, x11.A.NETDesktopNames,
		x11.A.NETWorkArea, x11.A.NETActiveWindow, x11.A.NETCloseWindow,
		x11.A.NETRestart, x11.A.NETShutdown, x11.A.NETMoveResizeWindow,
		x11.A.NETWMName, x11.A.NETWMIconName, x11.A.NETWMPID,
		x11.A.NETWindowType, x11.A.TypeNormal, x11.A.TypeDialog,
		x11.A.TypeMenu, x11.A.TypeUtility, x11.A.TypeToolbar,
		x11.A.TypeSplash, x11.A.TypeDropdownMenu,
		x11.A.TypePopupMenu, x11.A.TypeTooltip,
		x11.A.TypeNotification, x11.A.TypeCombo,
		x11.A.TypeDND, x11.A.TypeOverrideOther,
		x11.A.NETWMState, x11.A.StateModal, x11.A.StateSticky,
		x11.A.StateMaximizedVert, x11.A.StateMaximizedHorz,
		x11.A.StateShaded, x11.A.StateSkipTaskbar, x11.A.StateSkipPager,
		x11.A.StateHidden, x11.A.StateFullscreen, x11.A.StateAbove,
		x11.A.StateBelow, x11.A.StateDemandsAttention,
		x11.A.NETWMDesktop, x11.A.NETWMDesktopHint, x11.A.NETWMStrut,
		x11.A.NETWMStrutPartial, x11.A.NETWMUserTime, x11.A.NETFrameExtents,
		x11.A.NETRequestFrameExtents, x11.A.WMProtocols, x11.A.WMDeleteWindow,
		x11.A.WMTakeFocus, x11.A.WMChangeState,
	}

	if err := w.root.Start(w.cfg.Name, supported, uint32(w.cfg.Desktops), w.cfg.DesktopNamesOrDefault()); err != nil {
		return fmt.Errorf("wm: cannot claim the session: %w", err)
	}

	if err := d.RefreshKeymap(); err != nil {
		w.log.Warn("cannot read the keyboard mapping", "error", err)
	}
	if err := w.grabKeybinds(); err != nil {
		return err
	}

	w.refreshMonitors()
	d.SelectMonitorEvents(root)

	// Adopt the windows that already existed when we started.
	if err := w.adoptExisting(); err != nil {
		return err
	}
	return nil
}

// grabKeybinds installs every configured binding on the root window.
func (w *WM) grabKeybinds() error {
	binds, err := w.cfg.ResolvedKeybinds()
	if err != nil {
		return err
	}
	for _, b := range binds {
		if err := w.d.GrabKey(b.Binding, w.d.Root()); err != nil {
			w.log.Warn("cannot grab key", "binding", b.Binding.String(), "error", err)
			continue
		}
		w.keybinds[b.Binding] = b.Action
		w.grabs = append(w.grabs, b.Binding)
	}
	return nil
}

// adoptExisting walks the window tree and manages what is already mapped.
func (w *WM) adoptExisting() error {
	root := w.d.Root()
	if err := w.walkTree(root, 0); err != nil {
		return err
	}
	w.syncEWMH()
	w.relayout()
	return nil
}

// isRenderHelper reports whether a window belongs to the web renderer. Those
// windows are the hidden surfaces WebKit paints the widget tiles into, so the
// window manager has to leave them alone: framing them would put empty boxes
// on the desktop.
func (w *WM) isRenderHelper(id x11.Window) bool {
	name, _, ok := w.d.GetText(id, x11.A.WMName)
	if !ok || name == "" {
		name, ok = w.d.GetUTF8(id, x11.A.NETWMName)
		if !ok {
			return false
		}
	}
	return name == renderHelperName
}

// renderHelperName is the window title the web renderer uses.
const renderHelperName = "lythrum-render"

// walkTree descends the window hierarchy looking for windows to manage.
func (w *WM) walkTree(parent x11.Window, depth int) error {
	if depth > 16 {
		return nil
	}
	children, err := w.d.QueryTree(parent)
	if err != nil {
		return nil
	}
	for _, child := range children {
		attrs, err := w.d.Attributes(child)
		if err != nil {
			continue
		}
		if attrs.OverrideRedirect {
			continue
		}
		if attrs.MapState == x11.StateViewable && parent == w.d.Root() {
			if !w.isRenderHelper(child) {
				w.manage(child)
			}
			continue
		}
		if err := w.walkTree(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// refreshMonitors re-reads the display layout.
func (w *WM) refreshMonitors() {
	mons, err := w.d.Monitors()
	if err != nil {
		w.log.Warn("cannot query monitors", "error", err)
		sw, sh := w.d.Size()
		mons = []x11.Monitor{{X: 0, Y: 0, Width: sw, Height: sh, Primary: true}}
	}
	w.monitors = make([]layout.Monitor, 0, len(mons))
	for i, m := range mons {
		name := fmt.Sprintf("head-%d", i)
		w.monitors = append(w.monitors, layout.Monitor{Name: name, X: m.X, Y: m.Y, W: m.Width, H: m.Height, Primary: m.Primary})
	}
}

// Monitors returns the current monitor list.
func (w *WM) Monitors() []layout.Monitor { return w.monitors }

// StartScreen returns the tile grid overlay.
func (w *WM) StartScreen() *startScreen { return w.start }

// focusOverlay moves the keyboard focus to the first start-screen window so
// that keys reach the grid rather than a client.
func (w *WM) focusOverlay() {
	if w.start == nil || len(w.start.overlays) == 0 {
		return
	}
	w.d.Focus(w.start.overlays[0].win, x11.RevertToPointerRoot, 0)
}

// restoreFocus gives the keyboard back to the focused client.
func (w *WM) restoreFocus() {
	if w.focused == nil {
		w.d.FocusPointerRoot(x11.RevertToPointerRoot)
		return
	}
	w.d.Focus(w.focused.id, x11.RevertToPointerRoot, 0)
}

// workArea returns the area windows are placed in for a monitor.
func (w *WM) workArea(monitor int) layout.Rect {
	if monitor < 0 || monitor >= len(w.monitors) {
		return layout.Rect{W: screenW(w.d), H: screenH(w.d)}
	}
	m := w.monitors[monitor]
	return layout.Rect{X: m.X, Y: m.Y, W: m.W, H: m.H}
}

func screenW(d *x11.Display) int {
	w, _ := d.Size()
	return w
}

func screenH(d *x11.Display) int {
	_, h := d.Size()
	return h
}

// SyncEWMH republishes the EWMH root properties.
func (w *WM) SyncEWMH() { w.syncEWMH() }

func (w *WM) syncEWMH() {
	list := w.clientWindows()
	ids := make([]string, 0, len(list))
	for _, id := range list {
		ids = append(ids, fmt.Sprintf("0x%x", uint32(id)))
	}
	w.log.Debug("publishing client list", "clients", strings.Join(ids, ","))
	w.root.SetClientListStacking(list)
	w.root.SetClientList(list)
}

// spawn starts a detached child process with DISPLAY set.
func (w *WM) spawn(name string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("wm: nothing to spawn")
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return fmt.Errorf("wm: %s: %w", args[0], err)
	}
	cmd := exec.Command(path, args[1:]...)
	cmd.Env = append(os.Environ(), "DISPLAY="+os.Getenv("DISPLAY"))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("wm: cannot start %s: %w", args[0], err)
	}
	w.log.Info("spawned", "command", name, "pid", cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	return nil
}

// setLayoutKind stores the layout the windows are arranged in.
func (w *WM) setLayoutKind(kind layout.StackKind) {
	w.layoutMu.Lock()
	w.layout = kind
	w.layoutMu.Unlock()
}

// monitorsSnapshot copies the monitor list for readers outside the event loop.
func (w *WM) monitorsSnapshot() []layout.Monitor {
	w.layoutMu.RLock()
	defer w.layoutMu.RUnlock()
	return append([]layout.Monitor(nil), w.monitors...)
}

// layoutKind reports the current layout for code that runs off the event loop.
func (w *WM) layoutKind() layout.StackKind {
	w.layoutMu.RLock()
	defer w.layoutMu.RUnlock()
	return w.layout
}

// clientCount reports how many windows are managed, safe off the event loop.
func (w *WM) clientCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.clients)
}

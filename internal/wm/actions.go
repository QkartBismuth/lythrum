package wm

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"lythrum/internal/desk"
	"lythrum/internal/layout"
)

// runAction executes a keybinding or IPC action. The argument syntax is
// "action arg1 arg2"; actions that need no argument ignore it.
func (w *WM) runAction(action, arg string) {
	name, rest, _ := strings.Cut(strings.TrimSpace(action), " ")
	args := strings.Fields(arg)
	if strings.TrimSpace(rest) != "" {
		args = append(args, strings.Fields(rest)...)
	}

	switch name {
	case "spawn":
		w.doSpawn(w.cfg.Terminal)
	case "spawn_menu":
		w.doSpawn(w.cfg.Menu)
	case "spawn_exec":
		if len(args) == 0 {
			w.log.Error("spawn_exec needs a command")
			return
		}
		w.doSpawn(args)
	case "focus_next":
		w.focusNext(1)
	case "focus_prev":
		w.focusNext(-1)
	case "focus_left":
		w.focusDirection(-1)
	case "focus_right":
		w.focusDirection(1)
	case "focus_up":
		w.focusDirection(-2)
	case "focus_down":
		w.focusDirection(2)
	case "close_focused":
		if w.focused != nil {
			w.closeClient(w.focused)
		}
	case "close":
		if c, ok := w.resolveClient(args); ok {
			w.closeClient(c)
		}
	case "focus":
		if c, ok := w.resolveClient(args); ok {
			w.focus(c)
			w.raise(c)
		}
	case "toggle_float":
		if w.focused != nil {
			w.focused.ToggleFloating()
		}
	case "layout":
		w.setLayout(arg)
	case "tile":
		w.setLayout(string(layout.LayoutTiled))
	case "monocle":
		w.setLayout(string(layout.LayoutMonocle))
	case "stack":
		w.setLayout(string(layout.LayoutStack))
	case "search":
		if err := w.search.Toggle(); err != nil {
			w.log.Error("cannot open the search bar", "error", err)
		}
	case "power":
		if err := w.power.Toggle(); err != nil {
			w.log.Error("cannot open the power menu", "error", err)
		}
	case "logout":
		go func() {
			if err := w.logout(); err != nil {
				w.log.Error("cannot end the session", "error", err)
			}
		}()
	case "start_screen", "grid":
		if err := w.start.Toggle(); err != nil {
			w.log.Error("cannot open the start screen", "error", err)
		}
	case "start_screen_open":
		if w.start.selection < len(w.start.entries) {
			w.start.OpenSelected()
		}
	case "desktop_scan":
		w.rescanTiles()
	case "desktop_icons":
		w.desktop.Reload()
	case "quit":
		w.log.Info("quit requested")
		if w.start.open {
			w.start.Close()
		}
		w.Quit()
	case "restart":
		if w.restarter != nil {
			w.restarter()
		}
		w.Quit()
	case "reload":
		w.reloadConfig()
	default:
		if strings.HasPrefix(name, "desktop_") {
			n, err := strconv.Atoi(strings.TrimPrefix(name, "desktop_"))
			if err != nil || n < 1 {
				w.log.Error("bad desktop action", "action", action)
				return
			}
			w.SetDesktop(uint32(n - 1))
			return
		}
		if strings.HasPrefix(name, "workspace_") {
			n, err := strconv.Atoi(strings.TrimPrefix(name, "workspace_"))
			if err != nil {
				return
			}
			w.SetDesktop(uint32(n))
			return
		}
		w.log.Warn("unknown action", "action", action)
	}
}

// rescanTiles re-reads the desktop, the launchers and the home folders, then
// lays the new tiles out. The walk runs in the background, so the tiles appear
// as soon as it lands.
func (w *WM) rescanTiles() {
	if w.desk != nil {
		w.desk.scan(true)
		w.log.Info("desktop rescanned, waiting for the scan", "dir", desk.Dir())
		return
	}
	w.refreshTiles()
}

// doSpawn launches a command detached from the window manager.
func (w *WM) doSpawn(args []string) {
	if len(args) == 0 {
		return
	}
	if err := w.spawn(args[0], args); err != nil {
		w.log.Error("spawn failed", "command", args[0], "error", err)
	}
}

// setLayout switches the tiling arrangement.
func (w *WM) setLayout(name string) {
	kind, err := layout.Parse(name)
	if err != nil {
		w.log.Error("bad layout", "layout", name, "error", err)
		return
	}
	w.setLayoutKind(kind)
	w.cfg.Layout = string(kind)
	w.relayout()
	w.log.Info("layout changed", "layout", kind)
}

// focusDirection moves the focus in the given direction. The horizontal and
// vertical pairs are distinct actions so they can be bound independently.
func (w *WM) focusDirection(dir int) {
	list := w.focusList()
	if len(list) == 0 {
		return
	}
	idx := 0
	for i, c := range list {
		if c == w.focused {
			idx = i
			break
		}
	}
	next := idx + dir
	if next < 0 || next >= len(list) {
		return
	}
	w.focus(list[next])
	w.raise(list[next])
}

// resolveClient turns an argument into a client: an index in the focus list
// or a hexadecimal window id.
func (w *WM) resolveClient(args []string) (*Client, bool) {
	if len(args) == 0 {
		return w.focused, w.focused != nil
	}
	return parseClientID(args[0], w.focusList())
}

// reloadConfig re-reads the configuration file, if there is one.
func (w *WM) reloadConfig() {
	if w.configPath == "" {
		w.log.Info("no configuration file to reload")
		return
	}
	cfg, err := loadConfig(w.configPath)
	if err != nil {
		w.log.Error("cannot reload the configuration", "error", err)
		return
	}
	w.cfg = cfg
	w.setLayoutKind(cfg.LayoutKind())
	w.relayout()
	w.log.Info("configuration reloaded", "path", w.configPath)
}

// Describe returns a short human readable state dump, used by the IPC layer
// and by the --info flag.
func (w *WM) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s on root 0x%x\n", w.cfg.Name, uint32(w.d.Root()))
	fmt.Fprintf(&b, "uptime: %s\n", time.Since(w.started).Round(time.Second))
	fmt.Fprintf(&b, "layout: %s  desktops: %d (current %d)\n",
		w.layoutKind(), w.config().Desktops, w.root.CurrentDesktop())
	monitors := w.monitorsSnapshot()
	fmt.Fprintf(&b, "session: %s\n", w.session)
	off := w.canvas.Offset()
	fmt.Fprintf(&b, "desktop canvas: origin at %d,%d on screen\n", off.X, off.Y)
	fmt.Fprintf(&b, "monitors: %d\n", len(monitors))
	for _, m := range monitors {
		fmt.Fprintf(&b, "  %s %dx%d+%d+%d%s\n", m.Name, m.W, m.H, m.X, m.Y, primaryMark(m.Primary))
	}
	fmt.Fprintf(&b, "%s", w.start)
	fmt.Fprintf(&b, "%s", w.desktop)
	fmt.Fprintf(&b, "clients: %d\n", len(w.clients))
	for i, c := range w.focusList() {
		mark := " "
		if c == w.focused {
			mark = "*"
		}
		fmt.Fprintf(&b, "  %s [%d] 0x%x %s\n", mark, i, uint32(c.id), c.String())
	}
	return b.String()
}

func primaryMark(primary bool) string {
	if primary {
		return " (primary)"
	}
	return ""
}

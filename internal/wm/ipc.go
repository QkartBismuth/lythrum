package wm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"lythrum/internal/config"
	"lythrum/internal/layout"
)

// Command is one line of the control protocol. Requests and replies are JSON
// objects separated by newlines, which keeps the protocol easy to script:
//
//	printf '{"command":"focus","args":["1"]}\n' | nc -U /run/user/1000/lythrum.sock
type Command struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Reply is the answer to a Command.
type Reply struct {
	OK     bool              `json:"ok"`
	Error  string            `json:"error,omitempty"`
	Result json.RawMessage   `json:"result,omitempty"`
	Data   map[string]string `json:"data,omitempty"`
}

// ClientInfo describes a managed window to a control client.
type ClientInfo struct {
	ID       uint32 `json:"id"`
	Title    string `json:"title"`
	Class    string `json:"class"`
	Focused  bool   `json:"focused"`
	Floating bool   `json:"floating"`
	Desktop  uint32 `json:"desktop"`
	// Geometry is reported as x, y, width and height.
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"width"`
	H int `json:"height"`
}

// commands carries requests from the socket goroutine into the event loop,
// where all X access happens.
var (
	cmdMu   sync.Mutex
	cmdPipe = make(chan command, 64)
)

type command struct {
	cmd Command
	rep chan Reply
}

// IPCSocketPath returns the socket a running window manager listens on.
func IPCSocketPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "lythrum.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("lythrum-%d.sock", os.Getuid()))
}

// startIPC creates the control socket.
func (w *WM) startIPC() error {
	path := IPCSocketPath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("wm: cannot listen on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return err
	}
	w.socket = path
	w.listener = ln
	go w.acceptLoop(ln)
	w.log.Info("listening", "socket", path)
	return nil
}

// acceptLoop accepts control connections until the window manager stops.
func (w *WM) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-w.quit:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			w.log.Warn("accept failed", "error", err)
			return
		}
		go w.serveConn(conn)
	}
}

// serveConn reads newline-delimited commands from one client.
func (w *WM) serveConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var cmd Command
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			writeReply(conn, Reply{OK: false, Error: "malformed request: " + err.Error()})
			continue
		}
		writeReply(conn, w.submit(cmd))
	}
}

func writeReply(conn net.Conn, rep Reply) {
	data, err := json.Marshal(rep)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(data, '\n'))
}

// submit hands a command to the event loop and waits for the answer. The
// window manager owns the X connection, so every command must run there.
func (w *WM) submit(cmd Command) Reply {
	reply := make(chan Reply, 1)
	select {
	case cmdPipe <- command{cmd: cmd, rep: reply}:
	case <-w.quit:
		return Reply{OK: false, Error: "shutting down"}
	}
	select {
	case rep := <-reply:
		return rep
	case <-w.quit:
		return Reply{OK: false, Error: "shutting down"}
	}
}

// drainCommands runs every queued control command. It is called from the event
// loop, which is the only goroutine allowed to touch X.
func (w *WM) drainCommands() {
	for {
		select {
		case c := <-cmdPipe:
			c.rep <- w.execute(c.cmd)
		default:
			return
		}
	}
}

// ipcLoop starts the control socket and stops it when the context is done.
func (w *WM) ipcLoop(ctx context.Context) {
	if err := w.startIPC(); err != nil {
		w.log.Error("control socket unavailable", "error", err)
		return
	}
	<-ctx.Done()
	w.stopIPC()
}

func (w *WM) stopIPC() {
	if w.listener != nil {
		_ = w.listener.Close()
		w.listener = nil
	}
	if w.socket != "" {
		_ = os.Remove(w.socket)
		w.socket = ""
	}
}

// runPower performs one power action without opening the menu, which is what
// the IPC command and the scripts use.
func (w *WM) runPower(id string) Reply {
	switch id {
	case "logout", "suspend", "hibernate", "reboot", "poweroff":
	default:
		return Reply{OK: false, Error: "unknown power action " + id}
	}
	if !powerAvailableID(id) {
		return Reply{OK: false, Error: id + " is not available on this machine"}
	}
	go func() {
		if err := w.power.run(id); err != nil {
			w.log.Error("power action failed", "action", id, "error", err)
		}
	}()
	return Reply{OK: true, Data: map[string]string{"action": id}}
}

// execute performs a control command and produces its reply.
func (w *WM) execute(cmd Command) Reply {
	switch cmd.Command {
	case "ping":
		return Reply{OK: true, Result: json.RawMessage(`{"pong":true}`)}
	case "version":
		return Reply{OK: true, Result: json.RawMessage(`{"protocol":1}`)}
	case "list":
		list := make([]ClientInfo, 0, len(w.clients))
		for _, c := range w.focusList() {
			list = append(list, w.winInfo(c))
		}
		return Reply{OK: true, Result: mustJSON(list)}
	case "focus":
		if c, ok := w.resolveClient(cmd.Args); ok {
			w.focus(c)
			w.raise(c)
			return Reply{OK: true, Result: mustJSON(w.winInfo(c))}
		}
		return Reply{OK: false, Error: "no such client"}
	case "close":
		if c, ok := w.resolveClient(cmd.Args); ok {
			w.closeClient(c)
			return Reply{OK: true}
		}
		return Reply{OK: false, Error: "no such client"}
	case "spawn":
		args := cmd.Args
		if len(args) == 0 {
			args = w.cfg.Terminal
		}
		if err := w.spawn("ipc", args); err != nil {
			return Reply{OK: false, Error: err.Error()}
		}
		return Reply{OK: true}
	case "layout":
		if len(cmd.Args) == 0 {
			return Reply{OK: true, Result: mustJSON(map[string]string{"layout": string(w.layout)})}
		}
		w.setLayout(cmd.Args[0])
		return Reply{OK: true, Result: mustJSON(map[string]string{"layout": string(w.layout)})}
	case "desktop":
		if len(cmd.Args) == 0 {
			return Reply{OK: true, Result: mustJSON(map[string]uint32{"desktop": w.root.CurrentDesktop()})}
		}
		n, err := parseUint(cmd.Args[0])
		if err != nil {
			return Reply{OK: false, Error: err.Error()}
		}
		w.SetDesktop(n)
		return Reply{OK: true, Result: mustJSON(map[string]uint32{"desktop": w.root.CurrentDesktop()})}
	case "monitors":
		type mon struct {
			Name    string `json:"name"`
			X       int    `json:"x"`
			Y       int    `json:"y"`
			W       int    `json:"w"`
			H       int    `json:"h"`
			Primary bool   `json:"primary"`
		}
		list := make([]mon, 0, len(w.monitors))
		for _, m := range w.monitors {
			list = append(list, mon{Name: m.Name, X: m.X, Y: m.Y, W: m.W, H: m.H, Primary: m.Primary})
		}
		return Reply{OK: true, Result: mustJSON(list)}
	case "search":
		if err := w.search.Toggle(); err != nil {
			return Reply{OK: false, Error: err.Error()}
		}
		return Reply{OK: true}
	case "power":
		if len(cmd.Args) == 0 {
			if err := w.power.Toggle(); err != nil {
				return Reply{OK: false, Error: err.Error()}
			}
			return Reply{OK: true}
		}
		return w.runPower(cmd.Args[0])
	case "start_screen":
		switch len(cmd.Args) {
		case 0:
			if err := w.start.Toggle(); err != nil {
				return Reply{OK: false, Error: err.Error()}
			}
		case 1:
			switch cmd.Args[0] {
			case "open":
				w.start.OpenSelected()
			case "scan":
				w.rescanTiles()
			case "page":
				w.start.Page(1)
			default:
				return Reply{OK: false, Error: "use open, scan or page"}
			}
		default:
			return Reply{OK: false, Error: "start_screen takes at most one argument"}
		}
		return Reply{OK: true, Result: mustJSON(w.start.Status())}
	case "desktop_entries":
		return Reply{OK: true, Result: mustJSON(desktopSnapshot(w.start))}
	case "canvas":
		return w.canvasCommand(cmd.Args)
	case "toggle_float":
		if w.focused != nil {
			w.focused.ToggleFloating()
		}
		return Reply{OK: true}
	case "desktop_scan":
		w.rescanTiles()
		return Reply{OK: true}
	case "desktop_icons":
		w.desktop.Reload()
		return Reply{OK: true, Result: mustJSON(w.desktop.Status())}
	case "start_screen_paint":
		w.start.repaint()
		return Reply{OK: true}
	case "start_screen_info":
		return Reply{OK: true, Result: mustJSON(w.start.snapshot())}
	case "keybind":
		return Reply{OK: true, Result: mustJSON(w.keybindList())}
	case "info":
		return Reply{OK: true, Data: map[string]string{"info": w.Describe()}}
	case "quit":
		w.Quit()
		return Reply{OK: true}
	case "reload":
		w.reloadConfig()
		return Reply{OK: true}
	}
	return Reply{OK: false, Error: "unknown command " + cmd.Command}
}

// desktopEntry is one tile as reported over the control socket.
type desktopEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	Icon string `json:"icon,omitempty"`
}

func desktopSnapshot(s *startScreen) desktopReport {
	items := make([]desktopEntry, 0, len(s.entries))
	for _, e := range s.entries {
		items = append(items, desktopEntry{Name: e.Name, Path: e.Path, Kind: string(e.Kind), Icon: e.Icon})
	}
	return desktopReport{Dir: s.dir, Open: s.open, Page: s.page, Selection: s.selection, Entries: items}
}

// desktopReport is the tile inventory shown over the control socket.
type desktopReport struct {
	Dir       string         `json:"dir"`
	Open      bool           `json:"open"`
	Page      int            `json:"page"`
	Selection int            `json:"selection"`
	Entries   []desktopEntry `json:"entries"`
}

// KeybindInfo describes one key binding to a control client.
type KeybindInfo struct {
	Binding string `json:"binding"`
	Action  string `json:"action"`
}

func (w *WM) keybindList() []KeybindInfo {
	out := make([]KeybindInfo, 0, len(w.keybinds))
	for b, action := range w.keybinds {
		out = append(out, KeybindInfo{Binding: b.String(), Action: action})
	}
	return out
}

// canvasCommand moves the desktop. Without arguments it puts the view back at
// the origin, with two numbers it shows a point of the desktop, and with one
// number it slides that far.
func (w *WM) canvasCommand(args []string) Reply {
	switch len(args) {
	case 0:
		off := w.canvas.Offset()
		w.panCanvas(-off.X, -off.Y)
	case 1:
		dx, err := strconv.Atoi(args[0])
		if err != nil {
			return Reply{OK: false, Error: "the shift must be a number"}
		}
		w.panCanvas(dx, 0)
	case 2:
		x, errX := strconv.Atoi(args[0])
		y, errY := strconv.Atoi(args[1])
		if errX != nil || errY != nil {
			return Reply{OK: false, Error: "the point must be two numbers"}
		}
		off := w.canvas.Offset()
		w.canvas.PanTo(x-off.X, y-off.Y)
		w.desktopPainted()
	default:
		return Reply{OK: false, Error: "canvas takes a shift or a point"}
	}
	off := w.canvas.Offset()
	return Reply{OK: true, Result: mustJSON(map[string]int{"x": off.X, "y": off.Y})}
}

func (w *WM) winInfo(c *Client) ClientInfo {
	// The position is reported on the screen: it is what a tool asking about a
	// window expects, and the canvas place of the window follows from it.
	root := w.canvas.ToRootRect(c.geom)
	return ClientInfo{
		ID:       uint32(c.id),
		Title:    c.String(),
		Class:    c.info.Class[1],
		Focused:  c == w.focused,
		Floating: c.floating,
		Desktop:  c.desktop,
		X:        root.X, Y: root.Y, W: root.W, H: root.H,
	}
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return data
}

func parseUint(s string) (uint32, error) {
	var n uint32
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return 0, fmt.Errorf("not a number: %q", s)
	}
	return n, nil
}

// loadConfig is a seam for tests.
var loadConfig = config.Load

var _ = layout.LayoutStack

// RunStartup executes one startup command, which may contain a full command
// line rather than a pre-split argument list.
func (w *WM) RunStartup(commandLine string) {
	args, err := splitCommandLine(commandLine)
	if err != nil {
		w.log.Error("bad startup command", "command", commandLine, "error", err)
		return
	}
	if err := w.spawn("startup", args); err != nil {
		w.log.Error("startup command failed", "command", commandLine, "error", err)
	}
}

// splitCommandLine splits a command line honouring quotes.
func splitCommandLine(s string) ([]string, error) {
	var (
		out     []string
		cur     strings.Builder
		quote   rune
		escaped bool
	)
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	if escaped {
		return nil, errors.New("trailing backslash")
	}
	if quote != 0 {
		return nil, errors.New("unbalanced quote")
	}
	flush()
	if len(out) == 0 {
		return nil, errors.New("empty command")
	}
	return out, nil
}

// SendCommand talks to a running window manager over its control socket. It
// is what the command line client uses.
func SendCommand(cmd Command, timeout time.Duration) (Reply, error) {
	path := IPCSocketPath()
	conn, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		return Reply{}, fmt.Errorf("cannot reach the window manager at %s: %w", path, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return Reply{}, err
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return Reply{}, err
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return Reply{}, err
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Reply{}, err
	}
	var reply Reply
	if err := json.Unmarshal(line, &reply); err != nil {
		return Reply{}, fmt.Errorf("malformed reply: %w", err)
	}
	return reply, nil
}

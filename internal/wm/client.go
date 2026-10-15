package wm

import (
	"fmt"
	"strconv"

	"lythrum/internal/layout"
	"lythrum/internal/x11"
)

// Client is a managed top-level window.
type Client struct {
	wm *WM
	id x11.Window

	frame   x11.Window
	info    x11.ClientInfo
	geom    layout.Rect
	monitor int
	desktop uint32

	mapped   bool
	urgent   bool
	sticky   bool
	floating bool
}

func (c *Client) String() string {
	title := c.info.Name
	if title == "" {
		title = c.info.Class[1]
	}
	if title == "" {
		title = fmt.Sprintf("0x%x", uint32(c.id))
	}
	return title
}

// ID returns the client window id.
func (c *Client) ID() x11.Window { return c.id }

// Frame returns the decoration window, or 0 while the client is unmanaged.
func (c *Client) Frame() x11.Window { return c.frame }

// Info returns the cached ICCCM/EWMH properties.
func (c *Client) Info() x11.ClientInfo { return c.info }

// Geometry returns the last applied window geometry.
func (c *Client) Geometry() layout.Rect { return c.geom }

// Focused reports whether the client has the input focus.
func (c *Client) Focused() bool { return c.wm.focused == c }

// Floating reports whether the client is exempt from tiling.
func (c *Client) Floating() bool { return c.floating }

// manage takes ownership of a window: it reads the client properties, frames
// the window, maps it and focuses it if the configuration says so.
func (w *WM) manage(id x11.Window) *Client {
	if _, ok := w.byID[id]; ok {
		return w.byID[id]
	}
	info := w.d.ClientInfo(id)

	c := &Client{wm: w, id: id, info: info, desktop: w.d.WMDesktop(id)}
	c.floating = info.Transient != 0 || info.WindowType == x11.A.TypeDialog ||
		info.WindowType == x11.A.TypeSplash || info.WindowType == x11.A.TypeUtility ||
		info.WindowType == x11.A.TypeMenu || info.WindowType == x11.A.TypeDropdownMenu ||
		info.WindowType == x11.A.TypePopupMenu || info.WindowType == x11.A.TypeTooltip
	c.sticky = w.d.HasWMState(id, x11.A.StateSticky)

	d := w.d
	d.SelectInput(id, x11.PropertyChangeMask|x11.StructureNotifyMask|
		x11.FocusChangeMask|x11.EnterWindowMask|x11.VisibilityChangeMask)

	if err := c.createFrame(); err != nil {
		w.log.Error("cannot frame window", "window", fmt.Sprintf("0x%x", uint32(id)), "error", err)
		return nil
	}
	d.ChangeSaveSet(id)

	w.byID[id] = c
	w.clients = append(w.clients, c)
	w.order = append(w.order, id)
	w.log.Info("managing", "window", c.String(), "type", w.windowTypeName(id))

	d.SetWMState(id, x11.StateNormal)
	d.MapRaised(c.frame)

	if w.cfg.FocusNewWindows {
		w.focus(c)
	}
	w.syncEWMH()
	w.relayout()
	return c
}

func (w *WM) windowTypeName(id x11.Window) string {
	switch w.d.GetWindowType(id) {
	case x11.A.TypeDialog:
		return "dialog"
	case x11.A.TypeUtility:
		return "utility"
	case x11.A.TypeToolbar:
		return "toolbar"
	case x11.A.TypeMenu, x11.A.TypeDropdownMenu, x11.A.TypePopupMenu:
		return "menu"
	case x11.A.TypeSplash:
		return "splash"
	default:
		return "normal"
	}
}

// createFrame wraps the client in a decoration window.
func (c *Client) createFrame() error {
	w := c.wm
	d := w.d

	geom, err := d.Geometry(c.id)
	if err != nil {
		return err
	}
	width := int(geom.Width)
	height := int(geom.Height)
	if width <= 0 {
		width = 640
	}
	if height <= 0 {
		height = 480
	}
	x, y := int(geom.X), int(geom.Y)

	frame, err := d.CreateWindow(x11.CreateWindowParams{
		Parent:    d.Root(),
		X:         x,
		Y:         y,
		Width:     uint(width + 2*w.cfg.Border),
		Height:    uint(height + w.cfg.Border + w.cfg.TitleBar),
		Class:     x11.ClassInputOutput,
		BackPixel: w.color(w.cfg.TitleBackground),
		EventMask: x11.SubstructureRedirectMask | x11.SubstructureNotifyMask |
			x11.ButtonPressMask | x11.EnterWindowMask | x11.PointerMotionMask |
			x11.ExposureMask | x11.StructureNotifyMask | x11.FocusChangeMask,
	})
	if err != nil {
		return err
	}
	c.frame = frame

	d.SelectInput(c.id, x11.PropertyChangeMask|x11.StructureNotifyMask|
		x11.FocusChangeMask|x11.EnterWindowMask|x11.VisibilityChangeMask)
	d.ReparentWindow(c.id, frame, w.cfg.Border, w.cfg.Border+w.cfg.TitleBar)
	d.MapRaised(frame)
	return nil
}

// unmanage releases a client: the window is reparented back to the root so
// the client can keep drawing (some toolkits do not survive being reparented).
func (w *WM) unmanage(c *Client, destroy bool) {
	d := w.d
	delete(w.byID, c.id)
	for i, cl := range w.clients {
		if cl == c {
			w.clients = append(w.clients[:i], w.clients[i+1:]...)
			break
		}
	}
	for i, id := range w.order {
		if id == c.id {
			w.order = append(w.order[:i], w.order[i+1:]...)
			break
		}
	}
	if w.focused == c {
		w.focused = nil
		w.focusNext(1)
	}

	if c.frame != 0 {
		d.ReparentWindow(c.id, d.Root(), c.geom.X, c.geom.Y)
		d.UnmapWindow(c.frame)
		d.DestroyWindow(c.frame)
		c.frame = 0
	}
	if destroy {
		d.DestroyWindow(c.id)
	} else {
		d.SelectInput(c.id, x11.NoEventMask)
	}
	w.log.Info("unmanaged", "window", c.String())
	w.syncEWMH()
	w.relayout()
}

// closeClient asks a client to close: WM_DELETE_WINDOW when the client
// supports it, XKillClient otherwise.
func (w *WM) closeClient(c *Client) {
	d := w.d
	if d.SupportsProtocol(c.id, x11.A.WMDeleteWindow) {
		d.SendClientMessage(c.id, x11.A.WMProtocols, 32, [5]uint32{uint32(x11.A.WMDeleteWindow), 0, 0, 0, 0})
		return
	}
	w.log.Warn("client does not support WM_DELETE_WINDOW, killing it", "window", c.String())
	w.killClient(c)
}

// killClient destroys a client's connection, which closes every window it owns.
func (w *WM) killClient(c *Client) {
	if pid := c.info.PID; pid > 0 {
		if err := killProcess(pid, 9); err != nil {
			w.log.Warn("cannot kill client", "pid", pid, "error", err)
		}
	}
	w.unmanage(c, true)
}

// setWMStateIconic iconifies a window.
func (w *WM) setWMStateIconic(c *Client) {
	w.d.ChangeWMState(c.id, x11.A.StateHidden, false)
	w.d.IconifyWindow(c.id)
}

// clientWindows lists managed windows in bottom-to-top stacking order.
func (w *WM) clientWindows() []x11.Window {
	out := make([]x11.Window, 0, len(w.order))
	for _, id := range w.order {
		if _, ok := w.byID[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// focusList returns the focusable clients in stacking order.
func (w *WM) focusList() []*Client {
	out := make([]*Client, 0, len(w.order))
	for _, id := range w.order {
		c, ok := w.byID[id]
		if !ok {
			continue
		}
		if c.desktop != w.root.CurrentDesktop() && c.desktop != x11.AllDesktops {
			continue
		}
		if w.d.HasWMState(c.id, x11.A.StateHidden) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// focus gives the input focus to a client and publishes it to the session.
func (w *WM) focus(c *Client) {
	if c == nil {
		w.focused = nil
		w.d.SetActiveWindow(0)
		w.d.FocusPointerRoot(x11.RevertToPointerRoot)
		w.updateFrameColors()
		return
	}
	w.focused = c
	w.d.Restack(c.frame, 0, x11.Above)
	w.d.Focus(c.id, x11.RevertToPointerRoot, 0)
	_ = w.d.SetActiveWindow(c.id)
	if w.cfg.FollowMouse {
		// Centre the pointer on the newly focused window.
		g := c.geom
		w.d.WarpPointer((g.X + g.W/2), (g.Y + g.H/2))
	}
	w.updateFrameColors()
}

// focusNext moves the focus by delta positions in the stacking order.
func (w *WM) focusNext(delta int) {
	list := w.focusList()
	if len(list) == 0 {
		w.focus(nil)
		return
	}
	idx := 0
	for i, c := range list {
		if c == w.focused {
			idx = i
			break
		}
	}
	next := (idx + delta) % len(list)
	if next < 0 {
		next += len(list)
	}
	w.focus(list[next])
}

// focusClient focuses a client by position in the stacking order.
func (w *WM) focusClient(i int) {
	list := w.focusList()
	if i < 0 || i >= len(list) {
		return
	}
	w.focus(list[i])
}

// raise moves a client to the top of the stacking order.
func (w *WM) raise(c *Client) {
	for i, id := range w.order {
		if id == c.id {
			w.order = append(w.order[:i], w.order[i+1:]...)
			break
		}
	}
	w.order = append(w.order, c.id)
	w.d.Restack(c.frame, 0, x11.Above)
	w.syncEWMH()
}

// clientsRect applies a layout to the current clients and returns the
// geometry each client should occupy.
func (w *WM) arrange() map[x11.Window]layout.Rect {
	list := w.focusList()
	out := make(map[x11.Window]layout.Rect, len(list))
	if len(list) == 0 {
		return out
	}
	opts := w.cfg.LayoutOptions()
	tileable := make([]*Client, 0, len(list))
	for _, c := range list {
		if c.floating {
			continue
		}
		tileable = append(tileable, c)
	}

	work := w.workArea(0)
	switch w.layout {
	case layout.LayoutTiled:
		for i, c := range tileable {
			out[c.id] = layout.Tiled(work, len(tileable), opts)[i]
		}
	case layout.LayoutMonocle, layout.LayoutStack, layout.LayoutFloat:
		for _, c := range tileable {
			out[c.id] = layout.Stack(work)[0]
		}
	}
	for _, c := range list {
		if c.floating {
			out[c.id] = c.geom
		}
	}
	return out
}

// relayout recomputes and applies the geometry of every managed window.
func (w *WM) relayout() {
	rects := w.arrange()
	for _, c := range w.clients {
		r, ok := rects[c.id]
		if !ok {
			continue
		}
		w.applyGeometry(c, r)
	}
	w.syncEWMH()
}

// applyGeometry moves the frame of a client and resizes the client inside it.
func (w *WM) applyGeometry(c *Client, r layout.Rect) {
	d := w.d
	opts := w.cfg.LayoutOptions()
	if r == c.geom {
		return
	}
	c.geom = r

	if c.frame != 0 {
		d.MoveWindow(c.frame, r.X, r.Y)
		d.ResizeWindow(c.frame, uint32(r.W+2*opts.Border), uint32(r.H+opts.Border+opts.TitleBar))
		d.ConfigureWindow(c.frame, 0, 0, 0, 0, 0, 0, 0, 0)
	}
	d.MoveWindow(c.id, opts.Border, opts.Border+opts.TitleBar)
	d.ResizeWindow(c.id, uint32(max(r.W, 1)), uint32(max(r.H, 1)))
	d.Flush()
}

// updateFrameColors repaints the frames so the focused one stands out.
func (w *WM) updateFrameColors() {
	for _, c := range w.clients {
		if c.frame == 0 {
			continue
		}
		color := w.color(w.cfg.InactiveBorder)
		if c == w.focused {
			color = w.color(w.cfg.ActiveBorder)
		}
		w.d.SetWindowBackground(c.frame, color)
	}
	w.d.Flush()
}

// DesktopNames returns the configured desktop names.
func (w *WM) DesktopNames() []string { return w.cfg.DesktopNamesOrDefault() }

// SetDesktop switches the visible desktop, hiding and showing clients.
func (w *WM) SetDesktop(n uint32) {
	if n >= uint32(len(w.cfg.DesktopNamesOrDefault())) {
		return
	}
	w.root.SetCurrentDesktop(n)
	for _, c := range w.clients {
		visible := c.desktop == n || c.desktop == x11.AllDesktops
		if visible && c.frame != 0 {
			w.d.MapRaised(c.frame)
		} else if c.frame != 0 {
			w.d.UnmapWindow(c.frame)
		}
	}
	w.focused = nil
	w.relayout()
	w.focusNext(1)
}

// Clients returns a snapshot of the managed clients.
func (w *WM) Clients() []*Client {
	out := make([]*Client, len(w.clients))
	copy(out, w.clients)
	return out
}

// parseClientID parses a client index or window id from a command argument.
func parseClientID(s string, list []*Client) (*Client, bool) {
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 || n >= len(list) {
			return nil, false
		}
		return list[n], true
	}
	var id uint64
	if _, err := fmt.Sscanf(s, "0x%x", &id); err != nil {
		return nil, false
	}
	for _, c := range list {
		if uint64(c.id) == id {
			return c, true
		}
	}
	return nil, false
}

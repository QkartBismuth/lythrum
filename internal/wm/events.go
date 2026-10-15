package wm

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode"

	"lythrum/internal/x11"
)

// pollInterval is how often the loop wakes up to check for queued commands
// even when no X event arrives.
const pollInterval = 100 * time.Millisecond

// Run is the main event loop. It returns when the context is cancelled, the
// quit action fires or the X connection is lost.
func (w *WM) Run(ctx context.Context) error {
	defer w.shutdown()

	go w.ipcLoop(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.quit:
			return nil
		default:
		}

		timeout := pollInterval
		if w.paper != nil {
			timeout = w.paper.Interval()
		}
		ev, err := w.d.PollEvent(timeout)
		switch {
		case errors.Is(err, x11.ErrTimeout):
			w.drainCommands()
			w.applyPendingConfig()
			if w.desk != nil && w.desk.poll() {
				w.refreshTiles()
			}
			if w.queue != nil && w.queue.Poll() {
				// A preview landed, so the tiles that asked for it have to be
				// drawn again.
				if w.start != nil && w.start.open {
					w.start.repaint()
					continue
				}
				if w.desktop != nil {
					w.desktop.repaint()
				}
			}
			w.paintWidgetSessions()
			w.start.tickLive()
			w.paper.Tick(false)
			continue
		case errors.Is(err, x11.ErrConnectionLost):
			return err
		case err != nil:
			w.log.Warn("event error", "error", err)
			continue
		}

		w.log.Debug("event", "type", eventName(ev), "window", fmt.Sprintf("0x%x", eventWindow(ev)))
		w.handle(ev)
		w.drainCommands()
		w.applyPendingConfig()
		if w.desk != nil && w.desk.poll() {
			w.refreshTiles()
		}
	}
}

// shutdown gives the session back to whoever wants it next.
func (w *WM) shutdown() {
	if w.desktop != nil {
		w.desktop.Shutdown()
	}
	w.paper.Shutdown()
	for _, b := range w.grabs {
		w.d.UngrabKey(b, w.d.Root())
	}
	for _, c := range w.clients {
		if c.frame != 0 {
			w.d.ReparentWindow(c.id, w.d.Root(), c.geom.X, c.geom.Y)
		}
	}
	w.root.Stop()
	w.d.Sync()
	w.log.Info("stopped")
}

// handle dispatches a single X event.
func (w *WM) handle(ev x11.Event) {
	switch e := ev.(type) {
	case x11.MapRequest:
		w.onMapRequest(e)
	case x11.UnmapNotify:
		w.onUnmap(e)
	case x11.DestroyNotify:
		w.onDestroy(e)
	case x11.ConfigureRequest:
		w.onConfigureRequest(e)
	case x11.ReparentNotify:
		// The window manager reparents clients; ignore our own doing.
	case x11.PropertyNotify:
		w.onPropertyChange(e)
	case x11.ClientMessage:
		w.onClientMessage(e)
	case x11.KeyPress:
		w.onKeyPress(e)
	case x11.ButtonPress:
		w.onButtonPress(e)
	case x11.ButtonRelease:
		w.onButtonRelease(e)
	case x11.MotionNotify:
		w.onMotion(e)
	case x11.EnterNotify:
		w.onEnter(e)
	case x11.MappingNotify:
		if err := w.d.RefreshKeymap(); err != nil {
			w.log.Warn("cannot refresh the keyboard mapping", "error", err)
		}
	case x11.Generic:
		if x11.MonitorChanged(e) {
			w.log.Info("display layout changed")
			w.refreshMonitors()
			w.relayout()
			w.root.SetWorkArea(w.workAreas())
			if w.start != nil && w.start.open {
				// Rebuild the overlays against the new monitor layout.
				w.start.Close()
				_ = w.start.Open()
			}
			if w.desktop != nil {
				// The icons are placed per monitor, so the layer has to be
				// rebuilt against the new layout as well.
				w.desktop.build()
				w.desktop.scan()
			}
		}
	case x11.Expose:
		w.onExpose(e)
	}
}

// overlayFor reports the start-screen overlay a window belongs to.
func (w *WM) overlayFor(win x11.Window) *overlay {
	if w.start == nil {
		return nil
	}
	for _, o := range w.start.overlays {
		if o.win == win {
			return o
		}
	}
	return nil
}

func (w *WM) onMapRequest(e x11.MapRequest) {
	attrs, err := w.d.Attributes(e.Window)
	if err != nil {
		return
	}
	if w.start != nil && w.start.open {
		// The start screen is modal; new windows appear behind it.
		if err := w.d.GrabInput(e.Window, x11.ButtonPressMask, 0); err == nil {
			w.d.RaiseWindow(e.Window)
		}
	}
	if attrs.OverrideRedirect {
		// Override-redirect windows (menus, tooltips, docks) bypass the
		// window manager entirely.
		w.d.MapWindow(e.Window)
		return
	}
	if w.isRenderHelper(e.Window) {
		// The web renderer paints into this window off screen.
		w.d.MapWindow(e.Window)
		return
	}
	if _, managed := w.byID[e.Window]; managed {
		// A managed client asks to be mapped again, for instance after it was
		// iconified or after the manager reparented it into its frame.
		w.d.MapWindow(e.Window)
		return
	}
	w.manage(e.Window)
}

func (w *WM) onUnmap(e x11.UnmapNotify) {
	c, ok := w.byID[e.Window]
	if !ok {
		return
	}
	// A client unmaps itself to iconify; the WM unmap arrives with
	// FromConfigure set.
	if e.FromConfigure {
		return
	}
	w.unmanage(c, false)
}

func (w *WM) onDestroy(e x11.DestroyNotify) {
	if c, ok := w.byID[e.Window]; ok {
		w.unmanage(c, true)
	}
}

func (w *WM) onConfigureRequest(e x11.ConfigureRequest) {
	c, ok := w.byID[e.Window]
	if !ok {
		// Requests for unmanaged windows must be honoured as sent.
		w.applyConfigureRequest(e)
		return
	}
	// The window manager owns the geometry of managed clients; only the
	// border width stays under the client's control, so its drawing
	// assumptions keep working.
	if e.ValueMask&16 != 0 {
		w.d.ConfigureWindow(e.Window, x11.ConfigBorderWidth, 0, 0, 0, 0, uint32(w.cfg.Border), 0, 0)
	}
	_ = c
}

func (w *WM) applyConfigureRequest(e x11.ConfigureRequest) {
	var mask uint
	x, y, width, height, border, sibling := 0, 0, 0, 0, uint32(0), uint32(0)
	mode := 0
	vm := uint(e.ValueMask)
	if vm&1 != 0 {
		mask |= x11.ConfigX
		x = e.X
	}
	if vm&2 != 0 {
		mask |= x11.ConfigY
		y = e.Y
	}
	if vm&4 != 0 {
		mask |= x11.ConfigWidth
		width = e.Width
	}
	if vm&8 != 0 {
		mask |= x11.ConfigHeight
		height = e.Height
	}
	if vm&16 != 0 {
		mask |= x11.ConfigBorderWidth
		border = uint32(e.BorderWidth)
	}
	if vm&32 != 0 {
		mask |= x11.ConfigSibling
		sibling = uint32(e.AboveSibling)
	}
	if vm&64 != 0 {
		mask |= x11.ConfigStackMode
		mode = e.Detail
	}
	w.d.ConfigureWindow(e.Window, mask, x, y, uint32(width), uint32(height), border, sibling, mode)
}

func (w *WM) onPropertyChange(e x11.PropertyNotify) {
	c, ok := w.byID[e.Window]
	if !ok {
		return
	}
	switch e.Atom {
	case x11.A.NETWMName, x11.A.WMName, x11.A.WMClass, x11.A.NETWMIconName:
		c.info.Name = w.d.GetWMName(e.Window)
		w.log.Debug("title changed", "window", c.String())
	case x11.A.NETWMState:
		if hidden := w.d.HasWMState(e.Window, x11.A.StateHidden); hidden {
			c.mapped = false
		} else if !e.NewValue {
			c.mapped = true
		}
	case x11.A.NETWMDesktop, x11.A.NETWMDesktopHint:
		c.desktop = w.d.WMDesktop(e.Window)
		w.relayout()
	}
}

func (w *WM) onClientMessage(e x11.ClientMessage) {
	switch e.Type {
	case x11.A.NETActiveWindow:
		if c, ok := w.byID[e.Window]; ok {
			w.focus(c)
			w.raise(c)
		}
	case x11.A.NETCloseWindow:
		if c, ok := w.byID[e.Window]; ok {
			w.closeClient(c)
		}
	case x11.A.NETCurrentDesktop:
		w.SetDesktop(e.Data[0])
	case x11.A.NETMoveResizeWindow:
		w.moveResize(e)
	case x11.A.WMChangeState:
		if c, ok := w.byID[e.Window]; ok {
			switch e.Data[0] {
			case 1: // IconicState
				w.setWMStateIconic(c)
			case 3: // NormalState
				w.d.MapRaised(c.frame)
			}
		}
	case x11.A.NETWMDesktop:
		if c, ok := w.byID[e.Window]; ok && e.Data[2] == 1 {
			c.desktop = e.Data[0]
			w.relayout()
		}
	}
}

// moveResize implements the EWMH _NET_MOVERESIZE_WINDOW request.
func (w *WM) moveResize(e x11.ClientMessage) {
	c, ok := w.byID[e.Window]
	if !ok {
		return
	}
	// The message encodes the requested geometry plus flags.
	flags := e.Data[3]
	x, y := int(int16(uint16(e.Data[0]))), int(int16(uint16(e.Data[1])))
	width, height := int(uint16(e.Data[2])), int(uint16(uint16(e.Data[0]>>16)))

	// The message carries screen coordinates; the shell keeps the window in
	// canvas coordinates.
	x, y = w.canvas.ToCanvas(x, y)
	g := c.geom
	switch {
	case flags&(1<<8) != 0 && flags&(1<<9) != 0: // x and y
		g.X, g.Y = x, y
	case flags&(1<<8) != 0: // x only
		g.X = x
	case flags&(1<<9) != 0: // y only
		g.Y = y
	}
	if flags&(1<<10) != 0 || flags&(1<<11) != 0 { // width and height
		if width > 0 {
			g.W = width
		}
		if height > 0 {
			g.H = height
		}
	}
	c.floating = true
	w.applyGeometry(c, g, false)
}

func (w *WM) onKeyPress(e x11.KeyPress) {
	binding, ok := w.d.LookupKey(e.KeyEvent)
	if !ok {
		return
	}
	// A fullscreen widget takes the keyboard until it is closed.
	if session := w.activeSession(); session != nil {
		switch x11.KeySymName(binding.Sym) {
		case "Escape":
			session.Close()
		default:
			session.Key(x11.KeySymName(binding.Sym), keyText(binding.Sym))
		}
		return
	}
	if w.power != nil && w.power.Visible() {
		w.power.HandleKey(e, binding)
		return
	}
	if w.search != nil && w.search.Visible() {
		w.log.Debug("search key", "keysym", x11.KeySymName(binding.Sym))
		w.search.HandleKey(e, binding)
		return
	}
	if w.start != nil && w.start.open {
		w.log.Debug("start screen key", "keysym", x11.KeySymName(binding.Sym))
		w.start.HandleKey(e, binding)
		return
	}
	action, ok := w.keybinds[binding]
	if !ok {
		// Not ours: let the focused client handle it by re-sending the key
		// press, which the server delivers only if we did not grab it.
		if w.focused != nil {
			w.forwardKey(e, w.focused)
		}
		return
	}
	w.log.Debug("keybinding", "binding", binding.String(), "action", action)
	w.runAction(action, "")
}

// keyText returns the character a key symbol types, so a widget receives the
// letters of the current keyboard layout instead of Latin key names.
func keyText(ks x11.KeySym) string {
	if r, ok := x11.KeySymRune(ks); ok && unicode.IsPrint(r) {
		return string(r)
	}
	return x11.KeySymText(ks)
}

// forwardKey re-delivers a key press we did not act on.
func (w *WM) forwardKey(e x11.KeyPress, c *Client) {
	w.d.ForwardKey(e, c.id)
}

func (w *WM) onButtonPress(e x11.ButtonPress) {
	if session := w.activeSession(); session != nil {
		if e.Button == 1 {
			session.Click(int(e.RootX), int(e.RootY))
		}
		return
	}
	switch e.Button {
	case 4, 5, 6, 7:
		// The wheel turns the pages of the tile grid when it is open, and
		// slides the desktop when the grid is not.
		w.onScroll(e)
		return
	case 2:
		// The middle button pans from anywhere, which is the quickest way to
		// walk to a window standing at the far end of the desktop.
		w.beginPan(int(e.RootX), int(e.RootY))
		return
	case 1:
	default:
		return
	}
	rootX, rootY := int(e.RootX), int(e.RootY)
	if w.power != nil && w.power.Visible() {
		// The menu grabs the pointer, so the event carries coordinates inside
		// the menu window and they have to be turned back into root ones.
		if lx, ly, _, err := w.d.TranslateCoordinates(w.power.win, w.power.win,
			int(e.EventX), int(e.EventY)); err == nil {
			rootX, rootY = rootPoint(w.power.area, lx, ly)
		}
		w.power.HandleButton(rootX, rootY)
		return
	}
	if w.search != nil && w.search.Visible() {
		if lx, ly, _, err := w.d.TranslateCoordinates(w.search.win, w.search.win,
			int(e.EventX), int(e.EventY)); err == nil {
			rootX, rootY = rootPoint(w.search.area, lx, ly)
		}
		w.search.HandleButton(rootX, rootY)
		return
	}
	if o := w.overlayFor(e.Window); o != nil {
		// The pointer is grabbed to the overlay, so the root coordinates
		// need converting into the overlay's own space.
		if lx, ly, _, err := w.d.TranslateCoordinates(o.win, o.win, int(e.EventX), int(e.EventY)); err == nil {
			rootX, rootY = rootPoint(o.area, lx, ly)
		}
	}
	if w.start != nil && w.start.open {
		w.start.BeginDrag(rootX, rootY)
		w.start.HandleButton(e, rootX, rootY)
		return
	}
	if w.desktop != nil && w.desktop.surfaceFor(e.Window) != nil {
		// A press on the desktop may turn into a pan, so the icon layer is
		// asked whether it took the click and the pan starts either way.
		if w.desktop.HandleButton(rootX, rootY) {
			return
		}
		w.beginPan(rootX, rootY)
	}
}

// beginPan starts dragging the desktop with the pointer.
func (w *WM) beginPan(rootX, rootY int) {
	w.canvas.BeginDrag(rootX, rootY)
}

// onScroll pans the desktop or pages the start screen. Buttons 4 and 5 are the
// vertical wheel, 6 and 7 the horizontal one.
func (w *WM) onScroll(e x11.ButtonPress) {
	horizontal := e.Button == 6 || e.Button == 7
	if w.start != nil && w.start.open {
		switch e.Button {
		case 4:
			w.start.Page(-1)
		case 5:
			w.start.Page(1)
		case 6:
			w.start.Page(-1)
		case 7:
			w.start.Page(1)
		}
		return
	}
	notches := -1
	switch e.Button {
	case 5, 7:
		notches = 1
	case 4, 6:
		notches = -1
	default:
		return
	}
	w.canvas.PanByNotches(notches, horizontal)
	w.desktopPainted()
}

// panCanvas moves the desktop and repaints everything that lives on it: the
// wallpaper, the icons and the frames of the windows.
func (w *WM) panCanvas(dx, dy int) {
	if dx == 0 && dy == 0 {
		return
	}
	w.canvas.PanBy(dx, dy)
	w.desktopPainted()
}

// desktopPainted repaints the layers of the desktop after it moved.
func (w *WM) desktopPainted() {
	if w.paper != nil {
		w.paper.Repaint()
	}
	if w.desktop != nil {
		w.desktop.repaint()
	}
	w.relayout()
}

// onButtonRelease ends a drag on the start screen or on the desktop.
func (w *WM) onButtonRelease(x11.ButtonRelease) {
	if w.start != nil {
		w.start.EndDrag()
	}
	if w.canvas.EndDrag() {
		// The pointer was panning the desktop, so the press did not mean a
		// click on the empty desktop.
		return
	}
	if w.desktop != nil {
		w.desktop.Deselect()
	}
}

func (w *WM) onMotion(e x11.MotionNotify) {
	if w.start != nil && w.start.open && w.overlayFor(e.Window) != nil {
		if w.start.dragging {
			w.start.DragTo(int(e.RootX), int(e.RootY))
			return
		}
		w.start.HandleMotion(int(e.RootX), int(e.RootY))
		return
	}
	if w.canvas.dragging && w.canvas.DragTo(int(e.RootX), int(e.RootY)) {
		w.desktopPainted()
	}
}

func (w *WM) onEnter(e x11.EnterNotify) {
	if w.start != nil && w.start.open {
		// The start screen has the input; do not steal focus.
		return
	}
	if !w.cfg.FollowMouse {
		return
	}
	if c, ok := w.byID[e.Window]; ok && c != w.focused {
		w.focus(c)
		w.raise(c)
		return
	}
	// The pointer entered a frame rather than the client itself.
	if c, ok := w.byID[e.Child]; ok && c != w.focused {
		w.focus(c)
		w.raise(c)
	}
}

func (w *WM) onExpose(e x11.Expose) {
	if w.power != nil && w.power.Visible() {
		w.power.HandleExpose(e.Window)
		return
	}
	if w.search != nil && w.search.Visible() {
		w.search.HandleExpose(e.Window)
		return
	}
	if w.start != nil && w.overlayFor(e.Window) != nil {
		w.start.HandleExpose(e.Window)
		return
	}
	if w.desktop != nil && w.desktop.surfaceFor(e.Window) != nil {
		w.desktop.HandleExpose(e.Window)
		return
	}
	// Frames are filled with a solid colour, so a full repaint is enough.
	w.d.ClearArea(e.Window, 0, 0, 0, 0)
}

// workAreas returns the work area of every desktop.
func (w *WM) workAreas() [][4]uint32 {
	out := make([][4]uint32, 0, len(w.monitors))
	for _, m := range w.monitors {
		out = append(out, [4]uint32{uint32(m.X), uint32(m.Y), uint32(m.X + m.W), uint32(m.Y + m.H)})
	}
	return out
}

// eventName returns a readable name for an event, used in debug logs.
func eventName(ev x11.Event) string {
	switch ev.(type) {
	case x11.KeyPress:
		return "KeyPress"
	case x11.KeyRelease:
		return "KeyRelease"
	case x11.ButtonPress:
		return "ButtonPress"
	case x11.MotionNotify:
		return "MotionNotify"
	case x11.EnterNotify:
		return "EnterNotify"
	case x11.LeaveNotify:
		return "LeaveNotify"
	case x11.FocusIn:
		return "FocusIn"
	case x11.FocusOut:
		return "FocusOut"
	case x11.Expose:
		return "Expose"
	case x11.VisibilityNotify:
		return "VisibilityNotify"
	case x11.CreateNotify:
		return "CreateNotify"
	case x11.DestroyNotify:
		return "DestroyNotify"
	case x11.UnmapNotify:
		return "UnmapNotify"
	case x11.MapNotify:
		return "MapNotify"
	case x11.MapRequest:
		return "MapRequest"
	case x11.ReparentNotify:
		return "ReparentNotify"
	case x11.ConfigureNotify:
		return "ConfigureNotify"
	case x11.ConfigureRequest:
		return "ConfigureRequest"
	case x11.PropertyNotify:
		return "PropertyNotify"
	case x11.ClientMessage:
		return "ClientMessage"
	case x11.MappingNotify:
		return "MappingNotify"
	case x11.Generic:
		return "Generic"
	}
	return fmt.Sprintf("type-%d", ev.TypeCode())
}

// eventWindow returns the window an event refers to, or zero.
func eventWindow(ev x11.Event) uint32 {
	switch e := ev.(type) {
	case x11.MapRequest:
		return uint32(e.Window)
	case x11.UnmapNotify:
		return uint32(e.Window)
	case x11.DestroyNotify:
		return uint32(e.Window)
	case x11.ConfigureRequest:
		return uint32(e.Window)
	case x11.PropertyNotify:
		return uint32(e.Window)
	case x11.ClientMessage:
		return uint32(e.Window)
	case x11.KeyPress:
		return uint32(e.Window)
	case x11.EnterNotify:
		return uint32(e.Window)
	case x11.Expose:
		return uint32(e.Window)
	case x11.ConfigureNotify:
		return uint32(e.Window)
	}
	return 0
}

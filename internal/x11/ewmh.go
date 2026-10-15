package x11

// EWMHRoot is the root-window state a window manager owns: the support window,
// the supported atom list, desktop bookkeeping and the work area.
type EWMHRoot struct {
	d          *Display
	checkWin   Window
	desktops   uint32
	current    uint32
	names      []string
	workarea   [][4]uint32
	clientList []Window
	stacking   []Window

	// previousWindow/previousName remember the window manager that was
	// running before us so it can be restored on shutdown.
	previousWindow Window
	previousName   string
}

// NewEWMHRoot prepares the root-window side of the EWMH conversation.
func NewEWMHRoot(d *Display) *EWMHRoot {
	return &EWMHRoot{d: d, desktops: 4, current: 0, names: []string{"One", "Two", "Three", "Four"}}
}

// CheckWindow is the window referenced by _NET_SUPPORTING_WM_CHECK. Clients
// watch it for changes to detect that the window manager restarted.
func (r *EWMHRoot) CheckWindow() Window { return r.checkWin }

// Start advertises the window manager: it creates the support window, clears
// any stale advertisement and publishes the atom list.
func (r *EWMHRoot) Start(name string, supported []Atom, desktops uint32, names []string) error {
	d := r.d
	if desktops == 0 {
		desktops = 1
	}
	r.desktops = desktops
	if len(names) == int(desktops) {
		r.names = names
	}

	root := d.Root()
	// Remove any advertisement left by a previous window manager, but
	// remember it so we can restore it on shutdown.
	if prev, ok := d.GetAtomValue(root, A.NETSupportingWMCheck); ok && prev != 0 {
		if name, _, _ := d.GetText(Window(prev), A.WMName); name != "" {
			r.previousName = name
			r.previousWindow = Window(prev)
		}
		d.DeleteProperty(root, A.NETSupportingWMCheck)
	}

	win, err := d.CreateWindow(CreateWindowParams{
		Parent:           root,
		X:                -100,
		Y:                -100,
		Width:            1,
		Height:           1,
		Class:            ClassInputOutput,
		BackPixel:        0,
		BorderPixel:      0,
		EventMask:        PropertyChangeMask,
		OverrideRedirect: true,
	})
	if err != nil {
		return err
	}
	r.checkWin = win

	d.SetUTF8(win, A.NETWMName, name)
	d.SetUTF8(win, A.WMIconName, name)
	instance, class := name, name
	d.SetProperty(win, A.WMClass, A.String, 8, []byte(instance+"\x00"+class+"\x00"))

	// _NET_SUPPORTING_WM_CHECK is set last: its change is the signal clients
	// wait for before reading the rest.
	if err := d.SetCardinal(root, A.NETSupportingWMCheck, A.Cardinal, uint32(win)); err != nil {
		return err
	}
	d.SetUTF8(root, A.NETWMName, name)
	if err := d.SetSupportedAtoms(supported); err != nil {
		return err
	}
	if err := d.SetCardinal(root, A.NETNumberOfDesktops, A.Cardinal, desktops); err != nil {
		return err
	}
	d.SetProperty(root, A.NETDesktopNames, A.UTF8String, 8, joinNUL(r.names))
	r.SetCurrentDesktop(r.current)
	r.SetWorkArea(r.workareaForMonitors())
	d.Sync()
	return nil
}

// Stop removes the advertisement so clients can fall back to another manager.
func (r *EWMHRoot) Stop() {
	d := r.d
	if r.checkWin == 0 {
		return
	}
	d.DeleteProperty(d.Root(), A.NETSupportingWMCheck)
	d.DestroyWindow(r.checkWin)
	if r.previousWindow != 0 {
		_ = d.SetCardinal(d.Root(), A.NETSupportingWMCheck, A.Cardinal, uint32(r.previousWindow))
	}
	d.Sync()
	r.checkWin = 0
}

// SetWorkArea publishes the desktop work area (left, top, right, bottom).
func (r *EWMHRoot) SetWorkArea(areas [][4]uint32) {
	r.workarea = areas
	if len(areas) == 0 {
		return
	}
	flat := make([]uint32, 0, 4*len(areas))
	for _, a := range areas {
		flat = append(flat, a[:]...)
	}
	_ = r.d.SetCardinals(r.d.Root(), A.NETWorkArea, A.Cardinal, flat)
}

// SetCurrentDesktop switches desktops and tells the session about it.
func (r *EWMHRoot) SetCurrentDesktop(n uint32) {
	r.current = n
	_ = r.d.SetCardinal(r.d.Root(), A.NETCurrentDesktop, A.Cardinal, n)
	r.d.SendClientMessage(r.d.Root(), A.NETCurrentDesktop, 32, [5]uint32{n, 0, 0, sourceIndication, 0})
}

// CurrentDesktop returns the active desktop number.
func (r *EWMHRoot) CurrentDesktop() uint32 { return r.current }

// NumberOfDesktops returns how many desktops are configured.
func (r *EWMHRoot) NumberOfDesktops() uint32 { return r.desktops }

// SetDesktops records a new number of desktops after the window manager has
// published the matching root window property.
func (r *EWMHRoot) SetDesktops(n uint32, names []string) {
	if n == 0 {
		n = 1
	}
	r.desktops = n
	r.names = names
}

// SetClientList publishes _NET_CLIENT_LIST (bottom-to-top stacking order).
func (r *EWMHRoot) SetClientList(list []Window) {
	r.clientList = list
	_ = r.d.SetCardinals(r.d.Root(), A.NETClientList, A.Cardinal, windowsToUint32(list))
}

// SetClientListStacking publishes _NET_CLIENT_LIST_STACKING.
func (r *EWMHRoot) SetClientListStacking(list []Window) {
	r.stacking = list
	_ = r.d.SetCardinals(r.d.Root(), A.NETClientListStacking, A.Cardinal, windowsToUint32(list))
}

func windowsToUint32(list []Window) []uint32 {
	out := make([]uint32, len(list))
	for i, w := range list {
		out[i] = uint32(w)
	}
	return out
}

func joinNUL(items []string) []byte {
	var out []byte
	for _, s := range items {
		out = append(out, s...)
		out = append(out, 0)
	}
	return out
}

// workareaForMonitors computes the desktop work area covering every monitor.
func (r *EWMHRoot) workareaForMonitors() [][4]uint32 {
	mons, err := r.d.Monitors()
	if err != nil || len(mons) == 0 {
		w, h := r.d.Size()
		return [][4]uint32{{0, 0, uint32(w), uint32(h)}}
	}
	areas := make([][4]uint32, len(mons))
	for i, m := range mons {
		areas[i] = [4]uint32{uint32(m.X), uint32(m.Y), uint32(m.X + m.Width), uint32(m.Y + m.Height)}
	}
	return areas
}

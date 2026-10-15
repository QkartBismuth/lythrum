package x11

import (
	"strings"
	"unicode/utf8"
)

// WMState is the ICCCM WM_STATE value of a window.
type WMState int

// ICCCM window states.
const (
	StateWithdrawn WMState = 0
	StateNormal    WMState = 1
	StateIconic    WMState = 3
)

// SizeHintsFlags are the ICCCM WM_NORMAL_HINTS flag bits.
type SizeHintsFlags uint32

// WM_NORMAL_HINTS flags.
const (
	USPosition  SizeHintsFlags = 1 << 0
	USSize      SizeHintsFlags = 1 << 1
	PPosition   SizeHintsFlags = 1 << 2
	PSize       SizeHintsFlags = 1 << 3
	PMinSize    SizeHintsFlags = 1 << 4
	PMaxSize    SizeHintsFlags = 1 << 5
	PResizeInc  SizeHintsFlags = 1 << 6
	PAspect     SizeHintsFlags = 1 << 7
	PBaseSize   SizeHintsFlags = 1 << 8
	PWinGravity SizeHintsFlags = 1 << 9
)

// NormalHints is the ICCCM WM_NORMAL_HINTS structure.
type NormalHints struct {
	Flags                  SizeHintsFlags
	X, Y                   int
	Width, Height          int
	MinWidth, MinHeight    int
	MaxWidth, MaxHeight    int
	WidthInc, HeightInc    int
	MinAspectX, MinAspectY int
	MaxAspectX, MaxAspectY int
	BaseWidth, BaseHeight  int
	WinGravity             int
}

// ClientInfo caches the ICCCM/EWMH properties of a managed window.
type ClientInfo struct {
	Class       [2]string // instance name, class name
	Name        string
	IconName    string
	Role        string
	WindowType  Atom
	Transient   Window
	Protocols   []Atom
	PID         int
	Urgency     bool
	State       WMState
	NormalHints NormalHints
}

// GetWMClass reads the ICCCM WM_CLASS property.
func (d *Display) GetWMClass(w Window) (instance, class string, ok bool) {
	p, err := d.GetProperty(w, A.WMClass, 0, 0)
	if err != nil || p == nil || len(p.Data) == 0 {
		return "", "", false
	}
	parts := strings.Split(strings.TrimRight(string(p.Data), "\x00"), "\x00")
	if len(parts) > 0 {
		instance = parts[0]
	}
	if len(parts) > 1 {
		class = parts[1]
	}
	return instance, class, true
}

// GetWMName reads the window title, preferring the EWMH UTF-8 property and
// falling back to the ICCCM one.
func (d *Display) GetWMName(w Window) string {
	if s, ok := d.GetUTF8(w, A.NETWMName); ok && s != "" {
		return s
	}
	if s, _, ok := d.GetText(w, A.WMName); ok && s != "" {
		return s
	}
	if s, ok := d.GetUTF8(w, A.WMIconName); ok && s != "" {
		return s
	}
	return ""
}

// GetWMIconName reads the icon name of a window.
func (d *Display) GetWMIconName(w Window) string {
	if s, ok := d.GetUTF8(w, A.NETWMIconName); ok && s != "" {
		return s
	}
	if s, _, ok := d.GetText(w, A.WMIconName); ok {
		return s
	}
	return ""
}

// GetWMState reads the ICCCM WM_STATE property.
func (d *Display) GetWMState(w Window) WMState {
	vals, err := d.GetCardinals(w, A.WMState)
	if err != nil || len(vals) == 0 {
		return StateWithdrawn
	}
	return WMState(vals[0])
}

// SetWMState writes the ICCCM WM_STATE property.
func (d *Display) SetWMState(w Window, s WMState) error {
	return d.SetCardinal(w, A.WMState, A.Cardinal, uint32(s))
}

// GetWMProtocols lists the WM_PROTOCOLS entries of a window.
func (d *Display) GetWMProtocols(w Window) ([]Atom, error) {
	vals, err := d.GetCardinals(w, A.WMProtocols)
	if err != nil {
		return nil, err
	}
	out := make([]Atom, 0, len(vals))
	for _, v := range vals {
		out = append(out, Atom(v))
	}
	return out, nil
}

// SupportsProtocol reports whether the window listed the given WM_PROTOCOLS
// entry, which is how a client asks to be asked before it is closed.
func (d *Display) SupportsProtocol(w Window, p Atom) bool {
	list, err := d.GetWMProtocols(w)
	if err != nil {
		return false
	}
	for _, a := range list {
		if a == p {
			return true
		}
	}
	return false
}

// GetTransientFor reads the WM_TRANSIENT_FOR parent of a dialog.
func (d *Display) GetTransientFor(w Window) (Window, bool) {
	vals, err := d.GetCardinals(w, A.WMTransientFor)
	if err != nil || len(vals) == 0 || vals[0] == 0 {
		return 0, false
	}
	return Window(vals[0]), true
}

// GetPID reads the _NET_WM_PID property.
func (d *Display) GetPID(w Window) int {
	vals, err := d.GetCardinals(w, A.NETWMPID)
	if err != nil || len(vals) == 0 {
		return 0
	}
	return int(vals[0])
}

// GetWindowType reads the EWMH _NET_WM_WINDOW_TYPE atom.
func (d *Display) GetWindowType(w Window) Atom {
	vals, err := d.GetCardinals(w, A.NETWindowType)
	if err != nil || len(vals) == 0 {
		return A.TypeNormal
	}
	return Atom(vals[0])
}

// GetWMStates reads the EWMH _NET_WM_STATE list of a window.
func (d *Display) GetWMStates(w Window) []Atom {
	vals, err := d.GetCardinals(w, A.NETWMState)
	if err != nil {
		return nil
	}
	out := make([]Atom, 0, len(vals))
	for _, v := range vals {
		out = append(out, Atom(v))
	}
	return out
}

// HasWMState reports whether the window currently has the given EWMH state.
func (d *Display) HasWMState(w Window, state Atom) bool {
	for _, s := range d.GetWMStates(w) {
		if s == state {
			return true
		}
	}
	return false
}

// ChangeWMState adds or removes an EWMH window state and notifies the client
// and the root window, as required by the EWMH specification.
func (d *Display) ChangeWMState(w Window, state Atom, remove bool) error {
	states := d.GetWMStates(w)
	out := make([]Atom, 0, len(states)+1)
	found := false
	for _, s := range states {
		if s == state {
			found = true
			if remove {
				continue
			}
		}
		out = append(out, s)
	}
	if !found && !remove {
		out = append(out, state)
	}
	if err := d.SetCardinals(w, A.NETWMState, A.Atom, atomsFromUint32(out)); err != nil {
		return err
	}

	action := uint32(stateAdd)
	if remove {
		action = stateRemove
	}
	// The client is told first, then the root window, so pagers and panels
	// can track the change.
	d.SendClientMessage(w, A.NETWMState, 32, [5]uint32{action, uint32(state), 0, sourceIndication, 0})
	d.SendClientMessage(d.Root(), A.NETWMState, 32, [5]uint32{action, uint32(state), uint32(w), sourceIndication, 0})
	return nil
}

// SetWMDesktop moves a window to a desktop.
func (d *Display) SetWMDesktop(w Window, desktop uint32) error {
	if err := d.SetCardinal(w, A.NETWMDesktop, A.Cardinal, desktop); err != nil {
		return err
	}
	d.SendClientMessage(d.Root(), A.NETWMDesktop, 32, [5]uint32{uint32(w), desktop, 1, 0, 0})
	return nil
}

// WMDesktop reads the desktop of a window. A window without either EWMH
// property belongs to desktop 0; only an explicit 0xFFFFFFFF hint means the
// client asked to be visible on every desktop.
func (d *Display) WMDesktop(w Window) uint32 {
	if v, ok := d.GetAtomValue(w, A.NETWMDesktop); ok && v != 0xFFFFFFFF {
		return uint32(v)
	}
	if hint, err := d.GetCardinals(w, A.NETWMDesktopHint); err == nil && len(hint) > 0 {
		return hint[0]
	}
	return 0
}

// AllDesktops is the EWMH sticky-desktop value.
const AllDesktops = 0xFFFFFFFF

func atomsFromUint32(list []Atom) []uint32 {
	out := make([]uint32, len(list))
	for i, a := range list {
		out[i] = uint32(a)
	}
	return out
}

// ClientInfo collects every property the window manager needs about a window.
func (d *Display) ClientInfo(w Window) ClientInfo {
	info := ClientInfo{
		Name:     d.GetWMName(w),
		IconName: d.GetWMIconName(w),
		PID:      d.GetPID(w),
		State:    d.GetWMState(w),
	}
	if inst, class, ok := d.GetWMClass(w); ok {
		info.Class = [2]string{inst, class}
	}
	info.WindowType = d.GetWindowType(w)
	if t, ok := d.GetTransientFor(w); ok {
		info.Transient = t
	}
	if protocols, err := d.GetWMProtocols(w); err == nil {
		info.Protocols = protocols
	}
	info.NormalHints = d.GetNormalHints(w)
	return info
}

// GetNormalHints reads WM_NORMAL_HINTS.
func (d *Display) GetNormalHints(w Window) NormalHints {
	p, err := d.GetProperty(w, A.WMNormalHints, A.WMSizeHints, 0)
	if err != nil || p == nil || len(p.Data) < 18*4 {
		return NormalHints{}
	}
	g := func(i int) uint32 { return u32at(p.Data, i*4) }
	h := NormalHints{
		Flags:      SizeHintsFlags(g(0)),
		X:          int(int32(g(1))),
		Y:          int(int32(g(2))),
		Width:      int(int32(g(3))),
		Height:     int(int32(g(4))),
		MinWidth:   int(int32(g(5))),
		MinHeight:  int(int32(g(6))),
		MaxWidth:   int(int32(g(7))),
		MaxHeight:  int(int32(g(8))),
		WidthInc:   int(int32(g(9))),
		HeightInc:  int(int32(g(10))),
		MinAspectX: int(int32(g(11))),
		MinAspectY: int(int32(g(12))),
		MaxAspectX: int(int32(g(13))),
		MaxAspectY: int(int32(g(14))),
		BaseWidth:  int(int32(g(15))),
		BaseHeight: int(int32(g(16))),
		WinGravity: int(g(17)),
	}
	return h
}

// TruncateUTF8 cuts a string so that it fits in maxBytes without splitting a
// rune. Window titles can be arbitrarily long; the window manager only needs
// a bounded string for display.
func TruncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := s[:maxBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

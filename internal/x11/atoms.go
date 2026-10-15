package x11

// atomNames enumerates every ICCCM/EWMH atom the window manager touches.
// Interning them up front keeps the hot paths free of round-trips.
var atomNames = []string{
	// ICCCM.
	"WM_PROTOCOLS", "WM_DELETE_WINDOW", "WM_TAKE_FOCUS", "WM_STATE",
	"WM_CHANGE_STATE", "WM_CLASS", "WM_TRANSIENT_FOR", "WM_NORMAL_HINTS",
	"WM_SIZE_HINTS", "WM_HINTS", "WM_ICON_NAME", "WM_CLIENT_MACHINE",
	"WM_BYPASS_COMPOSITOR", "UTF8_STRING", "COMPOUND_TEXT",

	// EWMH atoms and properties.
	"_NET_SUPPORTED", "_NET_SUPPORTING_WM_CHECK", "_NET_CLIENT_LIST",
	"_NET_CLIENT_LIST_STACKING", "_NET_NUMBER_OF_DESKTOPS", "_NET_CURRENT_DESKTOP",
	"_NET_DESKTOP_NAMES", "_NET_DESKTOP_GEOMETRY", "_NET_DESKTOP_VIEWPORT",
	"_NET_WORKAREA", "_NET_ACTIVE_WINDOW", "_NET_CLOSE_WINDOW",
	"_NET_RESTART", "_NET_SHUTDOWN", "_NET_MOVERESIZE_WINDOW",
	"_NET_WM_NAME", "_NET_WM_VISIBLE_NAME", "_NET_WM_ICON_NAME",
	"_NET_WM_VISIBLE_ICON_NAME", "_NET_WM_PID", "_NET_WM_PID_INFO",
	"_NET_WM_WINDOW_TYPE", "_NET_WM_WINDOW_TYPE_NORMAL",
	"_NET_WM_WINDOW_TYPE_DIALOG", "_NET_WM_WINDOW_TYPE_MENU",
	"_NET_WM_WINDOW_TYPE_UTILITY", "_NET_WM_WINDOW_TYPE_TOOLBAR",
	"_NET_WM_WINDOW_TYPE_SPLASH", "_NET_WM_WINDOW_TYPE_DROPDOWN_MENU",
	"_NET_WM_WINDOW_TYPE_POPUP_MENU", "_NET_WM_WINDOW_TYPE_TOOLTIP",
	"_NET_WM_WINDOW_TYPE_NOTIFICATION", "_NET_WM_WINDOW_TYPE_COMBO",
	"_NET_WM_WINDOW_TYPE_DND", "_NET_WM_WINDOW_TYPE_OVERRIDE_OTHER",
	"_NET_WM_STATE", "_NET_WM_STATE_MODAL", "_NET_WM_STATE_STICKY",
	"_NET_WM_STATE_MAXIMIZED_VERT", "_NET_WM_STATE_MAXIMIZED_HORZ",
	"_NET_WM_STATE_SHADED", "_NET_WM_STATE_SKIP_TASKBAR",
	"_NET_WM_STATE_SKIP_PAGER", "_NET_WM_STATE_HIDDEN",
	"_NET_WM_STATE_FULLSCREEN", "_NET_WM_STATE_ABOVE",
	"_NET_WM_STATE_BELOW", "_NET_WM_STATE_DEMANDS_ATTENTION",
	"_NET_WM_DESKTOP", "_NET_WM_DESKTOP_HINT", "_NET_WM_STRUT",
	"_NET_WM_STRUT_PARTIAL", "_NET_WM_ICON", "_NET_WM_MENU",
	"_NET_WM_USER_TIME", "_NET_WM_WINDOW_OPACITY",
	"_NET_FRAME_EXTENTS", "_NET_REQUEST_FRAME_EXTENTS",
	"_NET_WM_MACRO", "_NET_WM_ALLOWED_ACTIONS",
	"_NET_WM_WINDOW_TYPE_NORMAL_NAME",
}

// atoms holds every atom the window manager uses.
type atoms struct {
	// ICCCM.
	WMProtocols     Atom
	WMDeleteWindow  Atom
	WMTakeFocus     Atom
	WMState         Atom
	WMChangeState   Atom
	WMClass         Atom
	WMName          Atom
	WMTransientFor  Atom
	Atom            Atom
	WMNormalHints   Atom
	WMSizeHints     Atom
	WMHints         Atom
	WMIconName      Atom
	WMClientMachine Atom
	WMBypassWM      Atom
	Cardinal        Atom
	String          Atom
	UTF8String      Atom
	CompoundText    Atom

	// EWMH properties.
	NETSupported           Atom
	NETSupportingWMCheck   Atom
	NETClientList          Atom
	NETClientListStacking  Atom
	NETNumberOfDesktops    Atom
	NETCurrentDesktop      Atom
	NETDesktopNames        Atom
	NETDesktopGeometry     Atom
	NETWorkArea            Atom
	NETActiveWindow        Atom
	NETCloseWindow         Atom
	NETRestart             Atom
	NETShutdown            Atom
	NETMoveResizeWindow    Atom
	NETWMName              Atom
	NETWMVisibleName       Atom
	NETWMIconName          Atom
	NETWMPID               Atom
	NETWMUserTime          Atom
	NETWindowOpacity       Atom
	NETFrameExtents        Atom
	NETRequestFrameExtents Atom
	NETWMDesktop           Atom
	NETWMDesktopHint       Atom
	NETWMStrut             Atom
	NETWMStrutPartial      Atom
	NETWMAllowedActions    Atom

	// Window types, named after the EWMH atoms with the prefix stripped.
	NETWindowType     Atom
	TypeNormal        Atom
	TypeDialog        Atom
	TypeMenu          Atom
	TypeUtility       Atom
	TypeToolbar       Atom
	TypeSplash        Atom
	TypeDropdownMenu  Atom
	TypePopupMenu     Atom
	TypeTooltip       Atom
	TypeNotification  Atom
	TypeCombo         Atom
	TypeDND           Atom
	TypeOverrideOther Atom

	// States.
	NETWMState            Atom
	StateModal            Atom
	StateSticky           Atom
	StateMaximizedVert    Atom
	StateMaximizedHorz    Atom
	StateShaded           Atom
	StateSkipTaskbar      Atom
	StateSkipPager        Atom
	StateHidden           Atom
	StateFullscreen       Atom
	StateAbove            Atom
	StateBelow            Atom
	StateDemandsAttention Atom
}

// A is the process-wide atom table, populated by Display.Open.
var A atoms

// loadStandardAtoms interns every atom in atomNames and fills A.
func loadStandardAtoms(d *Display) {
	A = atoms{}
	for _, p := range []struct {
		name string
		dst  *Atom
	}{
		{"WM_PROTOCOLS", &A.WMProtocols},
		{"WM_DELETE_WINDOW", &A.WMDeleteWindow},
		{"WM_TAKE_FOCUS", &A.WMTakeFocus},
		{"WM_STATE", &A.WMState},
		{"WM_CHANGE_STATE", &A.WMChangeState},
		{"WM_CLASS", &A.WMClass},
		{"WM_TRANSIENT_FOR", &A.WMTransientFor},
		{"WM_NAME", &A.WMName},
		{"ATOM", &A.Atom},
		{"WM_NORMAL_HINTS", &A.WMNormalHints},
		{"WM_SIZE_HINTS", &A.WMSizeHints},
		{"WM_HINTS", &A.WMHints},
		{"WM_ICON_NAME", &A.WMIconName},
		{"WM_CLIENT_MACHINE", &A.WMClientMachine},
		{"WM_BYPASS_COMPOSITOR", &A.WMBypassWM},
		{"UTF8_STRING", &A.UTF8String},
		{"COMPOUND_TEXT", &A.CompoundText},
		{"CARDINAL", &A.Cardinal},
		{"STRING", &A.String},

		{"_NET_SUPPORTED", &A.NETSupported},
		{"_NET_SUPPORTING_WM_CHECK", &A.NETSupportingWMCheck},
		{"_NET_CLIENT_LIST", &A.NETClientList},
		{"_NET_CLIENT_LIST_STACKING", &A.NETClientListStacking},
		{"_NET_NUMBER_OF_DESKTOPS", &A.NETNumberOfDesktops},
		{"_NET_CURRENT_DESKTOP", &A.NETCurrentDesktop},
		{"_NET_DESKTOP_NAMES", &A.NETDesktopNames},
		{"_NET_DESKTOP_GEOMETRY", &A.NETDesktopGeometry},
		{"_NET_WORKAREA", &A.NETWorkArea},
		{"_NET_ACTIVE_WINDOW", &A.NETActiveWindow},
		{"_NET_CLOSE_WINDOW", &A.NETCloseWindow},
		{"_NET_RESTART", &A.NETRestart},
		{"_NET_SHUTDOWN", &A.NETShutdown},
		{"_NET_MOVERESIZE_WINDOW", &A.NETMoveResizeWindow},
		{"_NET_WM_NAME", &A.NETWMName},
		{"_NET_WM_VISIBLE_NAME", &A.NETWMVisibleName},
		{"_NET_WM_ICON_NAME", &A.NETWMIconName},
		{"_NET_WM_PID", &A.NETWMPID},
		{"_NET_WM_USER_TIME", &A.NETWMUserTime},
		{"_NET_WM_WINDOW_OPACITY", &A.NETWindowOpacity},
		{"_NET_FRAME_EXTENTS", &A.NETFrameExtents},
		{"_NET_REQUEST_FRAME_EXTENTS", &A.NETRequestFrameExtents},
		{"_NET_WM_DESKTOP", &A.NETWMDesktop},
		{"_NET_WM_DESKTOP_HINT", &A.NETWMDesktopHint},
		{"_NET_WM_STRUT", &A.NETWMStrut},
		{"_NET_WM_STRUT_PARTIAL", &A.NETWMStrutPartial},
		{"_NET_WM_ALLOWED_ACTIONS", &A.NETWMAllowedActions},
		{"_NET_WM_WINDOW_TYPE", &A.NETWindowType},

		{"_NET_WM_WINDOW_TYPE_NORMAL", &A.TypeNormal},
		{"_NET_WM_WINDOW_TYPE_DIALOG", &A.TypeDialog},
		{"_NET_WM_WINDOW_TYPE_MENU", &A.TypeMenu},
		{"_NET_WM_WINDOW_TYPE_UTILITY", &A.TypeUtility},
		{"_NET_WM_WINDOW_TYPE_TOOLBAR", &A.TypeToolbar},
		{"_NET_WM_WINDOW_TYPE_SPLASH", &A.TypeSplash},
		{"_NET_WM_WINDOW_TYPE_DROPDOWN_MENU", &A.TypeDropdownMenu},
		{"_NET_WM_WINDOW_TYPE_POPUP_MENU", &A.TypePopupMenu},
		{"_NET_WM_WINDOW_TYPE_TOOLTIP", &A.TypeTooltip},
		{"_NET_WM_WINDOW_TYPE_NOTIFICATION", &A.TypeNotification},
		{"_NET_WM_WINDOW_TYPE_COMBO", &A.TypeCombo},
		{"_NET_WM_WINDOW_TYPE_DND", &A.TypeDND},
		{"_NET_WM_WINDOW_TYPE_OVERRIDE_OTHER", &A.TypeOverrideOther},

		{"_NET_WM_STATE", &A.NETWMState},
		{"_NET_WM_STATE_MODAL", &A.StateModal},
		{"_NET_WM_STATE_STICKY", &A.StateSticky},
		{"_NET_WM_STATE_MAXIMIZED_VERT", &A.StateMaximizedVert},
		{"_NET_WM_STATE_MAXIMIZED_HORZ", &A.StateMaximizedHorz},
		{"_NET_WM_STATE_SHADED", &A.StateShaded},
		{"_NET_WM_STATE_SKIP_TASKBAR", &A.StateSkipTaskbar},
		{"_NET_WM_STATE_SKIP_PAGER", &A.StateSkipPager},
		{"_NET_WM_STATE_HIDDEN", &A.StateHidden},
		{"_NET_WM_STATE_FULLSCREEN", &A.StateFullscreen},
		{"_NET_WM_STATE_ABOVE", &A.StateAbove},
		{"_NET_WM_STATE_BELOW", &A.StateBelow},
		{"_NET_WM_STATE_DEMANDS_ATTENTION", &A.StateDemandsAttention},
	} {
		*p.dst = d.Atom(p.name)
	}
}

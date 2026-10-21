package main

/*
#cgo pkg-config: x11 gtk+-3.0 webkit2gtk-4.1
#include <X11/Xlib.h>
#include <gtk/gtk.h>
#include <gdk/gdkx.h>
#include <webkit2/webkit2.h>
#include <stdlib.h>

#pragma GCC diagnostic ignored "-Wdeprecated-declarations"

static GHashTable *canvas_widget_views = NULL;

// canvas_widget_new creates a widget: a window without a frame that holds a
// web page. The style sheet of the active style is added to the page.
static unsigned long canvas_widget_new(const char *uri, const char *css,
                                       const char *title, int w, int h) {
	if (!canvas_widget_views) {
		canvas_widget_views = g_hash_table_new(g_direct_hash, g_direct_equal);
	}
	GtkWidget *win = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_decorated(GTK_WINDOW(win), FALSE);
	gtk_window_set_resizable(GTK_WINDOW(win), TRUE);
	gtk_window_set_default_size(GTK_WINDOW(win), w, h);
	gtk_window_set_title(GTK_WINDOW(win), title);
	WebKitWebView *view = WEBKIT_WEB_VIEW(webkit_web_view_new());
	gtk_container_add(GTK_CONTAINER(win), GTK_WIDGET(view));
	WebKitUserContentManager *mgr =
		webkit_web_view_get_user_content_manager(view);
	if (css && *css) {
		WebKitUserStyleSheet *sheet = webkit_user_style_sheet_new(
			css, WEBKIT_USER_CONTENT_INJECT_ALL_FRAMES,
			WEBKIT_USER_STYLE_LEVEL_USER, NULL, NULL);
		if (sheet) {
			webkit_user_content_manager_add_style_sheet(mgr, sheet);
			webkit_user_style_sheet_unref(sheet);
		}
	}
	webkit_web_view_load_uri(view, uri);
	gtk_widget_show_all(win);
	// The window has to be realised before its X window can be named: let
	// the toolkit do its work.
	int guard = 0;
	while (!gtk_widget_get_window(GTK_WIDGET(win)) &&
	       gtk_events_pending() && guard < 1000) {
		gtk_main_iteration();
		guard++;
	}
	GdkWindow *gw = gtk_widget_get_window(GTK_WIDGET(win));
	if (!gw) {
		g_printerr("canvas: the widget window has no GDK window yet\n");
		return 0;
	}
	unsigned long xid = (unsigned long)GDK_WINDOW_XID(gw);
	g_hash_table_insert(canvas_widget_views, GUINT_TO_POINTER(xid), view);
	return xid;
}

// canvas_widget_uri returns the address of the page of a widget. The page
// sends its commands to the shell in the fragment of the address.
static char *canvas_widget_uri(unsigned long xid) {
	if (!canvas_widget_views) {
		return NULL;
	}
	WebKitWebView *view = g_hash_table_lookup(canvas_widget_views,
		GUINT_TO_POINTER(xid));
	if (!view) {
		return NULL;
	}
	const char *uri = webkit_web_view_get_uri(view);
	return uri ? g_strdup(uri) : NULL;
}

// canvas_widget_js runs a script inside a widget.
static void canvas_widget_js(unsigned long xid, const char *js) {
	if (!canvas_widget_views) {
		return;
	}
	WebKitWebView *view = g_hash_table_lookup(canvas_widget_views,
		GUINT_TO_POINTER(xid));
	if (!view) {
		return;
	}
	webkit_web_view_run_javascript(view, js, NULL, NULL, NULL);
}
*/
import "C"

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"
)

// initToolkit starts the toolkit of the widgets. The widgets are HTML pages
// shown in a window of the toolkit, so it has to be awake even when the shell
// does not run its own loop.
func (v *view) initToolkit() {
	// The widgets live in the board, which is an X11 window: the toolkit has
	// to speak X11 as well, even in a Wayland session.
	if os.Getenv("GDK_BACKEND") == "" {
		os.Setenv("GDK_BACKEND", "x11")
	}
	// The engine of the pages wants a signal of its own for its garbage
	// collector: the one it asks for by default belongs to the Go runtime.
	if os.Getenv("JSC_SIGNAL_FOR_GC") == "" {
		os.Setenv("JSC_SIGNAL_FOR_GC", "2")
	}
	// The shared memory buffers of the engine do not work under a
	// compositor, the pages are drawn by the processor itself.
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
		os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
	if C.gtk_init_check(nil, nil) == 0 {
		fmt.Fprintln(os.Stderr, "canvas: the toolkit of the widgets is not available")
		return
	}
	// The engine took the interrupt signal for its collector: the board
	// takes it back, so that the usual keys still work.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
}

// widgetActive tells the event loop that the widgets need the toolkit.
var widgetActive atomic.Bool

// widgetDef describes a widget of the shell.
type widgetDef struct {
	name  string
	title string
	file  string
	w, h  int
}

// widgetDefs are the widgets the shell offers.
var widgetDefs = []widgetDef{
	{"clock", "Часы", "clock.html", 240, 150},
	{"calendar", "Календарь", "calendar.html", 300, 320},
	{"control", "Панель управления", "control.html", 300, 230},
	{"messages", "Сообщения", "messages.html", 340, 260},
	{"player", "Плеер", "player.html", 320, 130},
}

// openWidgetPicker opens the list of the widgets.
func (v *view) openWidgetPicker() {
	var items []menuItem
	for _, def := range widgetDefs {
		d := def
		items = append(items, menuItem{label: d.title, do: func() { v.createWidget(d) }})
	}
	items = append(items, menuItem{sep: true})
	items = append(items, menuItem{label: "Закрыть все виджеты", do: v.closeWidgets})
	v.menu = items
	v.menuOpen = true
	v.menuHover = -1
	v.menuX = v.w/2 - 80
	v.menuY = v.h / 3
	v.menuW = 0
	for _, it := range items {
		if !it.sep {
			if w := v.textWidth(it.label) + 30; w > v.menuW {
				v.menuW = w
			}
		}
	}
	v.redraw = true
}

// widgetFolder is the folder of the desktop where the pages of the widgets
// live. The shell copies them there, so the user can change them: the
// widgets show the pages of this folder when it holds them.
func widgetFolder() string {
	return filepath.Join(desktopDir(), "Lythrum", "widget")
}

// ensureWidgetFolder creates the folder of the widgets and copies the pages
// of the shell into it, when they are not there yet.
func ensureWidgetFolder() {
	dir := widgetFolder()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	for _, def := range widgetDefs {
		dst := filepath.Join(dir, def.file)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		src, err := widgetFile(def.file)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		os.WriteFile(dst, data, 0644)
	}
}

// widgetFile returns the path of the page of a widget: the copy of the
// desktop comes first, so the user can change the pages.
func widgetFile(name string) (string, error) {
	candidates := []string{
		filepath.Join(widgetFolder(), name),
		filepath.Join("data", "widgets", name),
		filepath.Join("canvas", "data", "widgets", name),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "data", "widgets", name),
			filepath.Join(dir, "..", "data", "widgets", name),
			filepath.Join(dir, "..", "share", "canvas", "data", "widgets", name),
			filepath.Join(dir, "..", "..", "share", "canvas", "data", "widgets", name))
	}
	candidates = append(candidates,
		filepath.Join("/usr/local/share/canvas/data/widgets", name),
		filepath.Join("/usr/share/canvas/data/widgets", name))
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".local", "share", "canvas", "data", "widgets", name))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, err := filepath.Abs(p)
			if err != nil {
				return p, nil
			}
			return abs, nil
		}
	}
	return "", os.ErrNotExist
}

// createWidget opens a widget on the board.
func (v *view) createWidget(def widgetDef) {
	path, err := widgetFile(def.file)
	if err != nil {
		v.setStatus("Виджет не найден: " + def.file)
		return
	}
	cx := v.ox + float64(v.w)/(2*v.zoom)
	cy := v.oy + float64(v.h)/(2*v.zoom)
	// The widget is a new window of the session: the board adopts it and
	// pins it to this place.
	v.pending = &pendingLaunch{
		cx:     cx,
		cy:     cy,
		before: v.rootChildren(),
		until:  time.Now().Add(3 * time.Second),
	}
	css := styleWidgetCSS(v.cfg.Style)
	uri := "file://" + path
	xid := C.canvas_widget_new(cstr(uri), cstr(css), cstr(def.title),
		C.int(def.w), C.int(def.h))
	if xid == 0 {
		v.setStatus("Не удалось открыть виджет")
		return
	}
	v.widgetXID = xid
	v.widgetKinds[xid] = def.name
	// The toolkit opens helper windows of its own: they must not be adopted
	// as applications.
	v.widgetUntil = time.Now().Add(3 * time.Second)
	widgetActive.Store(true)
	v.setStatus("Виджет: " + def.title)
	v.redraw = true
}

// closeWidgets closes every widget of the board.
func (v *view) closeWidgets() {
	for i := range v.wins {
		if v.wins[i].widget {
			v.closeWin(i)
		}
	}
	widgetActive.Store(false)
}

// widgetJS runs a script inside a widget.
func (v *view) widgetJS(xid C.ulong, script string) {
	if xid == 0 {
		return
	}
	C.canvas_widget_js(xid, cstr(script))
}

// widgetCommand is a command a widget sends to the shell.
type widgetCommand struct {
	cmd   string
	value float64
	text  string
}

// handleWidgetCommand runs a command of a widget. The command travels in the
// fragment of the address of the page: "cmd:value:text".
func (v *view) handleWidgetCommand(xid C.ulong, body string) {
	parts := strings.SplitN(body, ":", 3)
	if len(parts) == 0 {
		return
	}
	cmd := widgetCommand{cmd: parts[0]}
	if len(parts) > 1 {
		cmd.value, _ = strconv.ParseFloat(parts[1], 64)
	}
	if len(parts) > 2 {
		cmd.text = parts[2]
	}
	switch cmd.cmd {
	case "volume":
		vol := int(cmd.value)
		if vol < 0 {
			vol = 0
		}
		if vol > 100 {
			vol = 100
		}
		v.cfg.Sound.Volume = vol
		v.cfg.Sound.Muted = vol == 0
		v.cfg.save()
		applyVolume(vol, vol == 0)
	case "mute":
		v.cfg.Sound.Muted = cmd.value > 0
		v.cfg.save()
		applyVolume(v.cfg.Sound.Volume, v.cfg.Sound.Muted)
	case "brightness":
		level := strconv.Itoa(int(cmd.value)) + "%"
		runCommand("brightnessctl", "set", level)
		v.cfg.Screen.Brightness = int(cmd.value)
		v.cfg.save()
	case "wifi":
		on := "off"
		if cmd.value > 0 {
			on = "on"
		}
		runCommand("nmcli", "radio", "wifi", on)
	case "player":
		switch cmd.text {
		case "play-pause":
			runCommand("playerctl", "play-pause")
		case "next":
			runCommand("playerctl", "next")
		case "prev":
			runCommand("playerctl", "previous")
		}
	case "closewin":
		idx := int(cmd.value)
		if idx >= 0 && idx < len(v.wins) {
			v.closeWin(idx)
		}
	case "close":
		for i := range v.wins {
			if v.wins[i].widget {
				v.closeWin(i)
			}
		}
	}
}

// startWidgetTicker keeps the toolkit of the widgets alive: the event loop
// wakes up regularly and lets the widgets paint and run their scripts.
func startWidgetTicker() {
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for range tick.C {
			if widgetActive.Load() {
				wake()
			}
		}
	}()
}

// pumpToolkit lets the toolkit of the widgets do its work.
func (v *view) pumpToolkit() {
	if !widgetActive.Load() {
		return
	}
	C.g_main_context_iteration(C.g_main_context_default(), C.False)
}

// widgetTray lists the windows of the board for the panel of the widgets.
func (v *view) widgetTray() string {
	var items []string
	for i := range v.wins {
		title := v.wins[i].title
		if title == "" {
			title = "Окно"
		}
		items = append(items, fmt.Sprintf(`{"title":%q,"index":%d}`,
			title, i))
	}
	return "[" + strings.Join(items, ",") + "]"
}

// widgetPlayer asks the player for the state of the music.
func (v *view) widgetPlayer() string {
	title, _ := runCommand("playerctl", "metadata", "title")
	artist, _ := runCommand("playerctl", "metadata", "artist")
	status, _ := runCommand("playerctl", "status")
	pos, _ := runCommand("playerctl", "position")
	length, _ := runCommand("playerctl", "metadata", "mpris:length")
	playing := status == "Playing"
	return fmt.Sprintf(`{"title":%q,"artist":%q,"playing":%v,"position":%q,"length":%q}`,
		title, artist, playing, pos, length)
}

// tickWidgets feeds the widgets with the state of the shell and reads the
// commands they send.
func (v *view) tickWidgets() {
	if !widgetActive.Load() {
		return
	}
	if time.Since(v.widgetTick) < 300*time.Millisecond {
		return
	}
	v.widgetTick = time.Now()
	v.pollWidgets()
	v.updateWidgets()
}

// pollWidgets reads the commands the widgets have sent.
func (v *view) pollWidgets() {
	if !widgetActive.Load() {
		return
	}
	for i := range v.wins {
		w := &v.wins[i]
		if !w.widget {
			continue
		}
		uri := C.canvas_widget_uri(w.widgetXID)
		if uri == nil {
			continue
		}
		text := C.GoString(uri)
		C.g_free(C.gpointer(unsafe.Pointer(uri)))
		if hash := strings.IndexByte(text, '#'); hash >= 0 {
			frag := text[hash+1:]
			if frag != "" && !strings.HasPrefix(frag, "cmd=") {
				v.handleWidgetCommand(w.widgetXID, frag)
				v.widgetJS(w.widgetXID, "location.hash='';")
			}
		}
	}
}

// updateWidgets pushes the state of the shell into the widgets.
func (v *view) updateWidgets() {
	if !widgetActive.Load() {
		return
	}
	for i := range v.wins {
		w := &v.wins[i]
		if !w.widget {
			continue
		}
		switch w.widgetKind {
		case "control":
			v.widgetJS(w.widgetXID, "window.setWindows && window.setWindows("+
				v.widgetTray()+");")
		case "player":
			v.widgetJS(w.widgetXID, "window.setPlayer && window.setPlayer("+
				v.widgetPlayer()+");")
		case "messages":
			v.widgetJS(w.widgetXID, "window.setMessage && window.setMessage("+
				strconv.Quote(v.status)+");")
		}
	}
}

// widgetInfo describes a widget for the shell.
type widgetInfo struct {
	kind string
	xid  C.ulong
}

// unusedC keeps the compiler happy when the toolkit is not linked.
var _ = exec.Command
var _ = unsafe.Pointer(nil)

// bridge.c renders WebKitGTK pages off screen. Every view lives in a tiny GTK
// window that is pushed outside the visible area, and finished snapshots are
// handed to Go as plain RGBA buffers.
//
// WebKit reports its pixel buffers as ARGB32/R8G8B8A8, yet in practice the
// bytes arrive with red and blue swapped. The engine therefore calibrates the
// channel order once at start up against a page with known colours and applies
// the correction to every frame afterwards.

#include <locale.h>

#include <gtk/gtk.h>
#include <gdk/x11/gdkx.h>
#include <webkit/webkit.h>
#include <jsc/jsc.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

extern void lvGoFrame(uintptr_t handle, void *data, int w, int h, int swapped);
extern void lvGoTitle(uintptr_t handle, char *title);
extern void lvGoScheme(char *url, char *method, char *range, uintptr_t request);
extern void lvGoMessage(uintptr_t handle, char *payload);
extern void lvGoLoad(uintptr_t handle, int event);
extern void lvGoTerminated(uintptr_t handle, int reason);

typedef struct {
	uintptr_t handle;
	int width;
	int height;
	const char *html;
	const char *base;
	const char *uri;
	int fps;
	int transparent;
	int visible;
} LvArgs;

typedef struct {
	WebKitWebView *view;
	GtkWidget *window;
	uintptr_t handle;
	int width;
	int height;
	guint timer;
	gboolean dead;
	gboolean ready;
} LvView;

static WebKitWebContext *g_context;
static WebKitSettings *g_settings;
static GHashTable *g_views;
static GMainLoop *g_loop;
static gboolean g_ready;
static int g_swap_channels;

typedef void (*LvTaskFn)(void *);

typedef struct {
	LvTaskFn fn;
	void *data;
	GDestroyNotify destroy;
} LvTask;

static gboolean lv_task_trampoline(gpointer data) {
	LvTask *task = (LvTask *)data;
	task->fn(task->data);
	return G_SOURCE_REMOVE;
}

static void lv_task_free(gpointer data) {
	LvTask *task = (LvTask *)data;
	if (task->destroy) {
		task->destroy(task->data);
	}
	g_free(task);
}

// lv_post runs fn on the GTK thread once and then frees the task together with
// whatever it owns. The task is heap allocated because a compound literal
// would not survive until the idle actually runs.
static void lv_post(LvTaskFn fn, void *data, GDestroyNotify destroy) {
	LvTask *task = g_new0(LvTask, 1);
	task->fn = fn;
	task->data = data;
	task->destroy = destroy;
	g_idle_add_full(G_PRIORITY_DEFAULT, lv_task_trampoline, task, lv_task_free);
}

// lv_on_message receives a string posted by the page through the script
// message handler, which keeps widget to backend calls out of the network
// stack entirely.
static void lv_on_message(WebKitUserContentManager *manager, JSCValue *value, gpointer data) {
	(void)manager;
	LvView *v = (LvView *)data;
	if (!v || !value) {
		return;
	}
	GBytes *bytes = jsc_value_to_string_as_bytes(value);
	if (bytes) {
		gsize length = 0;
		const char *utf8 = g_bytes_get_data(bytes, &length);
		char *copy = g_malloc(length + 1);
		memcpy(copy, utf8, length);
		copy[length] = 0;
		lvGoMessage(v->handle, copy);
		g_free(copy);
		g_bytes_unref(bytes);
	}
}

static void lv_on_scheme(WebKitURISchemeRequest *request, gpointer user_data) {
	(void)user_data;
	const char *uri = webkit_uri_scheme_request_get_uri(request);
	if (!uri) {
		return;
	}
	// Media elements ask for byte ranges before they can seek, so the method
	// and the range header travel to the Go side as well.
	const char *method = webkit_uri_scheme_request_get_http_method(request);
	SoupMessageHeaders *headers = webkit_uri_scheme_request_get_http_headers(request);
	const char *range = headers ? soup_message_headers_get_one(headers, "Range") : NULL;
	lvGoScheme((char *)uri, (char *)method, (char *)range, (uintptr_t)request);
	// webkit_uri_scheme_request_get_http_headers hands back a borrowed
	// reference, so the headers must stay untouched here.
	(void)headers;
}

// lv_copy_frame converts a snapshot texture into RGBA for Go.
static void lv_copy_frame(LvView *v, GdkTexture *texture) {
	int w = gdk_texture_get_width(texture);
	int h = gdk_texture_get_height(texture);
	if (w <= 0 || h <= 0) {
		return;
	}
	gsize stride = (gsize)w * 4;
	unsigned char *raw = g_malloc((gsize)w * (gsize)h * 4);
	if (!raw) {
		return;
	}
	gdk_texture_download(texture, raw, stride);

	unsigned char *out = g_malloc((gsize)w * (gsize)h * 4);
	if (!out) {
		g_free(raw);
		return;
	}
	for (gsize i = 0, n = (gsize)w * (gsize)h * 4; i < n; i += 4) {
		unsigned char r = raw[i + 0], g = raw[i + 1], b = raw[i + 2];
		if (g_swap_channels) {
			r = raw[i + 2];
			b = raw[i + 0];
		}
		unsigned char a = raw[i + 3];
		if (a == 0) {
			r = g = b = 0;
		} else if (a < 255) {
			// The buffer is premultiplied, RGBA in Go is not.
			r = (unsigned char)MIN(255, (r * 255 + a / 2) / a);
			g = (unsigned char)MIN(255, (g * 255 + a / 2) / a);
			b = (unsigned char)MIN(255, (b * 255 + a / 2) / a);
		}
		out[i + 0] = r;
		out[i + 1] = g;
		out[i + 2] = b;
		out[i + 3] = a;
	}
	lvGoFrame(v->handle, out, w, h, g_swap_channels);
	g_free(out);
	g_free(raw);
}

static void lv_on_snapshot(GObject *source, GAsyncResult *result, gpointer data) {
	LvView *v = (LvView *)data;
	GError *error = NULL;
	GdkTexture *texture = webkit_web_view_get_snapshot_finish(WEBKIT_WEB_VIEW(source), result, &error);
	if (texture) {
		lv_copy_frame(v, texture);
		g_object_unref(texture);
	} else if (error) {
		g_warning("lythrum: snapshot failed: %s", error->message);
		g_error_free(error);
	}
}

static gboolean lv_frame_tick(gpointer data) {
	LvView *v = (LvView *)data;
	if (v->dead || !v->view) {
		return G_SOURCE_REMOVE;
	}
	// Snapshotting before the first paint returns an empty texture.
	if (!v->ready) {
		return G_SOURCE_CONTINUE;
	}
	webkit_web_view_get_snapshot(v->view, WEBKIT_SNAPSHOT_REGION_VISIBLE,
		WEBKIT_SNAPSHOT_OPTIONS_NONE, NULL, lv_on_snapshot, v);
	return G_SOURCE_CONTINUE;
}

static void lv_on_title(GObject *object, GParamSpec *pspec, gpointer data) {
	(void)pspec;
	LvView *v = (LvView *)data;
	const char *title = webkit_web_view_get_title(WEBKIT_WEB_VIEW(object));
	if (title) {
		lvGoTitle(v->handle, (char *)title);
	}
}

static void lv_on_load_failed(WebKitWebView *view, WebKitLoadEvent event, char *uri, GError *error, gpointer data) {
	(void)view;
	(void)event;
	(void)uri;
	(void)data;
	g_warning("lythrum: load failed: %s", error ? error->message : "unknown");
}

static void lv_on_load_changed(WebKitWebView *view, WebKitLoadEvent event, gpointer data) {
	LvView *v = (LvView *)data;
	if (event == WEBKIT_LOAD_FINISHED) {
		v->ready = TRUE;
	}
	lvGoLoad(v->handle, (int)event);
}

static void lv_on_process_terminated(WebKitWebView *view, WebKitWebProcessTerminationReason reason, gpointer data) {
	(void)view;
	LvView *v = (LvView *)data;
	v->ready = FALSE;
	lvGoTerminated(v->handle, (int)reason);
}

// lv_calibrate figures out whether the snapshot buffers need red and blue
// swapped, by looking at a page whose colours we know.
static void lv_on_calibration(GObject *source, GAsyncResult *result, gpointer data) {
	GError *error = NULL;
	GdkTexture *texture = webkit_web_view_get_snapshot_finish(WEBKIT_WEB_VIEW(source), result, &error);
	if (texture) {
		int w = gdk_texture_get_width(texture), h = gdk_texture_get_height(texture);
		unsigned char *raw = g_malloc((gsize)w * (gsize)h * 4);
		gdk_texture_download(texture, raw, (gsize)w * 4);
		// The page is a red page, so the first pixel must come back red.
		g_swap_channels = !(raw[0] > 200 && raw[1] < 60 && raw[2] < 60);
		g_free(raw);
		g_object_unref(texture);
	}
	if (error) {
		g_error_free(error);
	}
	// The calibration window stays alive: destroying it from inside its own
	// snapshot callback would pull the view out from under WebKit.
}

static gboolean lv_calibrate_tick(gpointer data) {
	WebKitWebView *view = WEBKIT_WEB_VIEW(data);
	webkit_web_view_get_snapshot(view, WEBKIT_SNAPSHOT_REGION_VISIBLE,
		WEBKIT_SNAPSHOT_OPTIONS_NONE, NULL, lv_on_calibration, data);
	return G_SOURCE_REMOVE;
}

static void lv_on_calibration_load(WebKitWebView *view, WebKitLoadEvent event, gpointer data) {
	if (event == WEBKIT_LOAD_FINISHED) {
		g_timeout_add(300, lv_calibrate_tick, view);
	}
}

// lv_make_override marks a helper window as override-redirect before it is
// mapped, so no window manager ever decorates it or puts a frame on screen.
static void lv_make_override(GtkWindow *window) {
	GdkSurface *surface = gtk_native_get_surface(GTK_NATIVE(window));
	if (surface == NULL || !GDK_IS_X11_SURFACE(surface)) {
		return;
	}
	GdkDisplay *display = gdk_surface_get_display(surface);
	if (!GDK_IS_X11_DISPLAY(display)) {
		return;
	}
	Window xid = gdk_x11_surface_get_xid(GDK_X11_SURFACE(surface));
	Display *dpy = gdk_x11_display_get_xdisplay(GDK_X11_DISPLAY(display));
	XSetWindowAttributes attrs;
	memset(&attrs, 0, sizeof(attrs));
	attrs.override_redirect = True;
	XChangeWindowAttributes(dpy, xid, CWOverrideRedirect, &attrs);
	XFlush(dpy);
}

// lv_hide pushes a helper window outside the visible area, so WebKit has a
// real surface to paint into while the user sees nothing. GTK4 has no way to
// place a window itself, so the X server is asked directly.
static void lv_hide(GtkWindow *window, int width, int height) {
	GdkSurface *surface = gtk_native_get_surface(GTK_NATIVE(window));
	if (surface == NULL || !GDK_IS_X11_SURFACE(surface)) {
		return;
	}
	GdkDisplay *display = gdk_surface_get_display(surface);
	if (!GDK_IS_X11_DISPLAY(display)) {
		return;
	}
	Window xid = gdk_x11_surface_get_xid(GDK_X11_SURFACE(surface));
	Display *dpy = gdk_x11_display_get_xdisplay(GDK_X11_DISPLAY(display));
	XMoveResizeWindow(dpy, xid, -width - 64, -height - 64, (unsigned)width, (unsigned)height);
	XFlush(dpy);
}

static gpointer lv_gtk_thread(gpointer unused) {
	(void)unused;
	// GTK compares the environment against the locales the C library knows.
	// A machine with LANG=en_US.UTF-8 and no generated locale would fall back
	// to plain C and mangle the text, so C.UTF-8 is used when the environment
	// locale is missing.
	// Inside a Wayland session the toolkit would open its surfaces over the
	// Wayland socket, where the shell cannot see or move them. Setting
	// LYTHRUM_WEBKIT_X11=1 puts the widget windows on XWayland instead, which
	// is the only backend the shell can work with.
	if (g_getenv("LYTHRUM_WEBKIT_X11") != NULL) {
		g_setenv("GDK_BACKEND", "x11", TRUE);
	}

	if (setlocale(LC_ALL, "") == NULL) {
		// The name of the built-in UTF-8 locale differs between systems
		// (C.UTF-8 here, C.utf8 there). GLib calls setlocale itself while
		// gtk_init runs, so the fallback has to reach it through the
		// environment as well.
		static const char *fallbacks[] = {"C.UTF-8", "C.utf8", "en_US.UTF-8"};
		for (size_t i = 0; i < sizeof(fallbacks) / sizeof(fallbacks[0]); i++) {
			if (setlocale(LC_ALL, fallbacks[i]) != NULL) {
				g_setenv("LC_ALL", fallbacks[i], TRUE);
				break;
			}
		}
	}
	if (!gtk_init_check()) {
		g_warning("lythrum: gtk could not open the display");
		return NULL;
	}
	g_views = g_hash_table_new(g_direct_hash, g_direct_equal);
	// WebKit 6.0 has no way to attach a custom context to a view, so the
	// scheme has to be registered on the default one.
	g_context = webkit_web_context_get_default();
	webkit_web_context_register_uri_scheme(g_context, "lythrum",
		(WebKitURISchemeRequestCallback)lv_on_scheme, NULL, NULL);

	// Widget pages and their backend calls share the lythrum scheme, so it has
	// to count as a local, secure origin or every request is cross origin.
	WebKitSecurityManager *security = webkit_web_context_get_security_manager(g_context);
	webkit_security_manager_register_uri_scheme_as_local(security, "lythrum");
	webkit_security_manager_register_uri_scheme_as_secure(security, "lythrum");
	webkit_security_manager_register_uri_scheme_as_cors_enabled(security, "lythrum");

	g_settings = webkit_settings_new();
	webkit_settings_set_enable_developer_extras(g_settings, FALSE);
	webkit_settings_set_javascript_can_open_windows_automatically(g_settings, FALSE);
	webkit_settings_set_allow_file_access_from_file_urls(g_settings, FALSE);
	webkit_settings_set_hardware_acceleration_policy(g_settings,
		WEBKIT_HARDWARE_ACCELERATION_POLICY_ALWAYS);

	// A throwaway view renders one known page so the pixel order can be
	// verified before any real tile is drawn.
	GtkWidget *cal_win = gtk_window_new();
	gtk_window_set_default_size(GTK_WINDOW(cal_win), 64, 64);
	WebKitWebView *cal_view = WEBKIT_WEB_VIEW(webkit_web_view_new());
	gtk_window_set_child(GTK_WINDOW(cal_win), GTK_WIDGET(cal_view));
	gtk_window_set_title(GTK_WINDOW(cal_win), "lythrum-render");
	gtk_widget_set_opacity(cal_win, 0.0);
	gtk_widget_realize(cal_win);
	lv_make_override(GTK_WINDOW(cal_win));
	gtk_window_present(GTK_WINDOW(cal_win));
	lv_hide(GTK_WINDOW(cal_win), 64, 64);
	g_signal_connect(cal_view, "load-changed", G_CALLBACK(lv_on_calibration_load), cal_view);
	webkit_web_view_load_html(cal_view,
		"<html><body style='margin:0;background:#ff0000'></body></html>", NULL);

	g_loop = g_main_loop_new(NULL, FALSE);
	g_ready = TRUE;
	g_main_loop_run(g_loop);
	return NULL;
}

int lv_engine_start(void) {
	if (g_loop) {
		return g_ready ? 1 : 0;
	}
	GThread *thread = g_thread_new("lythrum-gtk", lv_gtk_thread, NULL);
	for (int i = 0; i < 900 && !g_ready; i++) {
		g_usleep(10000);
	}
	return g_ready ? 1 : 0;
}

int lv_engine_ready(void) { return g_ready ? 1 : 0; }

static void lv_free_args(void *data) {
	LvArgs *args = (LvArgs *)data;
	g_free((char *)args->html);
	g_free((char *)args->base);
	g_free((char *)args->uri);
	g_free(args);
}

static void lv_on_create(void *data) {
	LvArgs *args = (LvArgs *)data;
	if (!g_ready) {
		return;
	}
	LvView *v = g_new0(LvView, 1);
	v->handle = args->handle;
	v->width = args->width > 0 ? args->width : 320;
	v->height = args->height > 0 ? args->height : 240;

	GtkWidget *window = gtk_window_new();
	gtk_window_set_default_size(GTK_WINDOW(window), v->width, v->height);
	gtk_window_set_decorated(GTK_WINDOW(window), FALSE);
	gtk_window_set_resizable(GTK_WINDOW(window), FALSE);
	v->view = WEBKIT_WEB_VIEW(webkit_web_view_new());
	webkit_web_view_set_settings(v->view, g_settings);
	gtk_window_set_child(GTK_WINDOW(window), GTK_WIDGET(v->view));
	g_signal_connect(v->view, "notify::title", G_CALLBACK(lv_on_title), v);
	WebKitUserContentManager *manager = webkit_web_view_get_user_content_manager(v->view);
	g_signal_connect(manager, "script-message-received::lythrum", G_CALLBACK(lv_on_message), v);
	webkit_user_content_manager_register_script_message_handler(manager, "lythrum", NULL);

	g_signal_connect(v->view, "load-changed", G_CALLBACK(lv_on_load_changed), v);
	g_signal_connect(v->view, "load-failed-with-tls-errors", G_CALLBACK(lv_on_load_failed), v);
	g_signal_connect(v->view, "web-process-terminated", G_CALLBACK(lv_on_process_terminated), v);

	GdkRGBA transparent = {0.0, 0.0, 0.0, 0.0};
	GdkRGBA opaque = {1.0, 1.0, 1.0, 1.0};
	webkit_web_view_set_background_color(v->view, args->transparent ? &transparent : &opaque);

	// WebKit needs a presented window to paint, but the user must never see
	// it, so the helper sits outside the visible area.
	gtk_window_set_title(GTK_WINDOW(window), "lythrum-render");
	gtk_widget_set_size_request(window, v->width, v->height);
	// The helper window must exist for WebKit to paint into, but it must not
	// be seen: it is fully transparent and, on X11, moved off screen.
	gtk_widget_set_opacity(window, 0.0);
	gtk_window_present(GTK_WINDOW(window));
	gtk_widget_queue_resize(window);
	lv_hide(GTK_WINDOW(window), v->width, v->height);
	v->window = window;

	g_hash_table_insert(g_views, GSIZE_TO_POINTER(v->handle), v);

	if (args->html && args->html[0]) {
		webkit_web_view_load_html(v->view, args->html,
			args->base && args->base[0] ? args->base : NULL);
	} else if (args->uri && args->uri[0]) {
		webkit_web_view_load_uri(v->view, args->uri);
	}
	if (args->fps > 0) {
		v->timer = g_timeout_add(1000 / args->fps, lv_frame_tick, v);
	}
}

// lv_view_new_ex creates a view, optionally in a window the user can see and
// click, which is what a fullscreen widget needs.
void lv_view_new_ex(uintptr_t handle, int width, int height, const char *html,
	const char *base, const char *uri, int fps, int transparent, int visible);

void lv_view_new(uintptr_t handle, int width, int height, const char *html,
	const char *base, const char *uri, int fps, int transparent) {
	lv_view_new_ex(handle, width, height, html, base, uri, fps, transparent, 0);
}

void lv_view_new_ex(uintptr_t handle, int width, int height, const char *html,
	const char *base, const char *uri, int fps, int transparent, int visible) {
	LvArgs *args = g_new0(LvArgs, 1);
	args->handle = handle;
	args->width = width;
	args->height = height;
	args->html = html ? g_strdup(html) : NULL;
	args->base = base ? g_strdup(base) : NULL;
	args->uri = uri ? g_strdup(uri) : NULL;
	args->fps = fps;
	args->transparent = transparent;
	args->visible = visible;
	lv_post(lv_on_create, args, (GDestroyNotify)lv_free_args);
}

typedef struct {
	uintptr_t handle;
	int width;
	int height;
	int fps;
} LvConfig;

static void lv_on_configure(void *data) {
	LvConfig *cfg = (LvConfig *)data;
	LvView *v = (LvView *)g_hash_table_lookup(g_views, GSIZE_TO_POINTER(cfg->handle));
	if (!v) {
		return;
	}
	if (cfg->width > 0 && cfg->height > 0 &&
		(cfg->width != v->width || cfg->height != v->height)) {
		v->width = cfg->width;
		v->height = cfg->height;
		gtk_window_set_default_size(GTK_WINDOW(v->window), v->width, v->height);
		lv_hide(GTK_WINDOW(v->window), v->width, v->height);
	}
	if (cfg->fps >= 0) {
		if (v->timer) {
			g_source_remove(v->timer);
			v->timer = 0;
		}
		if (cfg->fps > 0) {
			v->timer = g_timeout_add(1000 / cfg->fps, lv_frame_tick, v);
		}
	}
}

void lv_view_configure(uintptr_t handle, int width, int height, int fps) {
	LvConfig *cfg = g_new0(LvConfig, 1);
	cfg->handle = handle;
	cfg->width = width;
	cfg->height = height;
	cfg->fps = fps;
	lv_post(lv_on_configure, cfg, g_free);
}

typedef struct {
	uintptr_t handle;
	const char *text;
} LvText;

static void lv_free_text(void *data) {
	LvText *t = (LvText *)data;
	g_free((char *)t->text);
	g_free(t);
}

static void lv_on_eval(void *data) {
	LvText *t = (LvText *)data;
	LvView *v = (LvView *)g_hash_table_lookup(g_views, GSIZE_TO_POINTER(t->handle));
	if (v && v->view && t->text) {
		webkit_web_view_evaluate_javascript(v->view, t->text, -1, NULL, NULL, NULL, NULL, NULL);
	}
}

void lv_view_eval(uintptr_t handle, const char *js) {
	LvText *t = g_new0(LvText, 1);
	t->handle = handle;
	t->text = js ? g_strdup(js) : NULL;
	lv_post(lv_on_eval, t, (GDestroyNotify)lv_free_text);
}

static void lv_on_load_uri(void *data) {
	LvText *t = (LvText *)data;
	LvView *v = (LvView *)g_hash_table_lookup(g_views, GSIZE_TO_POINTER(t->handle));
	if (v && v->view && t->text) {
		webkit_web_view_load_uri(v->view, t->text);
	}
}

void lv_view_load_uri(uintptr_t handle, const char *uri) {
	LvText *t = g_new0(LvText, 1);
	t->handle = handle;
	t->text = uri ? g_strdup(uri) : NULL;
	lv_post(lv_on_load_uri, t, (GDestroyNotify)lv_free_text);
}

static void lv_on_navigate(void *data) {
	LvText *t = (LvText *)data;
	LvView *v = (LvView *)g_hash_table_lookup(g_views, GSIZE_TO_POINTER(t->handle));
	if (v && v->view && t->text) {
		webkit_web_view_load_html(v->view, t->text, NULL);
	}
}

void lv_view_load_html(uintptr_t handle, const char *html) {
	LvText *t = g_new0(LvText, 1);
	t->handle = handle;
	t->text = html ? g_strdup(html) : NULL;
	lv_post(lv_on_navigate, t, (GDestroyNotify)lv_free_text);
}

static void lv_on_destroy(void *data) {
	uintptr_t handle = (uintptr_t)data;
	LvView *v = (LvView *)g_hash_table_lookup(g_views, GSIZE_TO_POINTER(handle));
	if (!v) {
		return;
	}
	if (v->timer) {
		g_source_remove(v->timer);
	}
	v->dead = TRUE;
	g_hash_table_remove(g_views, GSIZE_TO_POINTER(handle));
	if (v->window) {
		gtk_window_destroy(GTK_WINDOW(v->window));
	}
	g_free(v);
}

void lv_view_free(uintptr_t handle) {
	lv_post(lv_on_destroy, GSIZE_TO_POINTER(handle), NULL);
}

typedef struct {
	uintptr_t request;
	char *mime;
	char *body;
	int length;
	int status;
	char *range;
	int total;
} LvReply;

static void lv_on_reply(void *data) {
	LvReply *reply = (LvReply *)data;
	WebKitURISchemeRequest *request = (WebKitURISchemeRequest *)reply->request;
	GInputStream *stream = g_memory_input_stream_new_from_data(reply->body,
		(gssize)reply->length, (GDestroyNotify)g_free);

	if (reply->status == 206 || reply->range) {
		// Byte range answers let the browser seek in audio and video.
		WebKitURISchemeResponse *response = webkit_uri_scheme_response_new(stream, reply->length);
		webkit_uri_scheme_response_set_status(response, (guint)reply->status,
			reply->status == 206 ? "Partial Content" : "OK");
		webkit_uri_scheme_response_set_content_type(response, reply->mime);
		SoupMessageHeaders *headers = soup_message_headers_new(SOUP_MESSAGE_HEADERS_RESPONSE);
		soup_message_headers_append(headers, "Accept-Ranges", "bytes");
		if (reply->range) {
			soup_message_headers_append(headers, "Content-Range", reply->range);
		}
		webkit_uri_scheme_response_set_http_headers(response, headers);
		soup_message_headers_unref(headers);
		webkit_uri_scheme_request_finish_with_response(request, response);
		g_object_unref(response);
	} else {
		webkit_uri_scheme_request_finish(request, stream, reply->length, reply->mime);
	}
	// The stream keeps our copy of the body alive: WebKit reads it after this
	// callback returns, so the reference is deliberately not dropped here.
	g_free(reply->mime);
	g_free(reply->range);
	g_free(reply); // the reply frees itself, so it carries no destroy notify
}

// lv_scheme_reply completes a request that the Go side served.
void lv_scheme_reply(uintptr_t request, int status, const char *mime, const char *body,
	int length, const char *range) {
	LvReply *reply = g_new0(LvReply, 1);
	reply->request = request;
	reply->status = status > 0 ? status : 200;
	reply->mime = g_strdup(mime ? mime : "text/plain");
	reply->range = range ? g_strdup(range) : NULL;
	reply->body = g_malloc((gsize)(length > 0 ? length : 1));
	if (body && length > 0) {
		memcpy(reply->body, body, (size_t)length);
	} else {
		reply->body[0] = 0;
		length = 0;
	}
	reply->length = length;
	lv_post(lv_on_reply, reply, NULL);
}


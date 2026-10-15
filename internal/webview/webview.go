// Package webview renders HTML, CSS and JavaScript off screen with WebKitGTK
// and hands finished frames to the window manager as plain RGBA buffers. It is
// the runtime behind live tiles and the settings application.
package webview

/*
#cgo pkg-config: webkitgtk-6.0 gtk4 x11
#include <stdint.h>
#include <stdlib.h>

int lv_engine_start(void);
int lv_engine_ready(void);
void lv_view_new(uintptr_t handle, int width, int height, const char *html,
	const char *base, const char *uri, int fps, int transparent);
void lv_view_new_ex(uintptr_t handle, int width, int height, const char *html,
	const char *base, const char *uri, int fps, int transparent, int visible);
void lv_view_configure(uintptr_t handle, int width, int height, int fps);
void lv_view_eval(uintptr_t handle, const char *js);
void lv_view_load_html(uintptr_t handle, const char *html);
void lv_view_load_uri(uintptr_t handle, const char *uri);
void lv_view_free(uintptr_t handle);
void lv_scheme_reply(uintptr_t request, int status, const char *mime, const char *body,
	int length, const char *range);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"sync"
	"unsafe"
)

// ErrNoDisplay means WebKit could not open the display, so live tiles stay
// inert and the window manager falls back to static tiles.
var ErrNoDisplay = errors.New("webview: no display")

// boolToInt turns a flag into what cgo expects.
func boolToInt(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

// Frame is one rendered picture, in straight (non premultiplied) RGBA order.
type Frame struct {
	Width  int
	Height int
	Pixels []byte
}

// Image returns the frame as an image, so callers can scale or draw it with
// the same code paths as any other picture.
func (f Frame) Image() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
	copy(img.Pix, f.Pixels)
	return img
}

// At reports whether the frame covers the given point.
func (f Frame) At(x, y int) (r, g, b, a byte, ok bool) {
	if f.Pixels == nil || x < 0 || y < 0 || x >= f.Width || y >= f.Height {
		return 0, 0, 0, 0, false
	}
	i := (y*f.Width + x) * 4
	return f.Pixels[i], f.Pixels[i+1], f.Pixels[i+2], f.Pixels[i+3], true
}

// SchemeRequest is one lythrum:// request from a page.
type SchemeRequest struct {
	// URL is the full address, for example lythrum://app/clock/index.html.
	URL string
	// Method is GET, HEAD or POST.
	Method string
	// Range is the raw Range header, which media elements use to seek.
	Range string
}

// SchemeResponse is what the shell sends back.
type SchemeResponse struct {
	// Mime is the content type; an empty value means no reply.
	Mime string
	// Body is the payload.
	Body []byte
	// Status defaults to 200; 206 answers a range request.
	Status int
	// ContentRange is the Content-Range header for a partial answer.
	ContentRange string
}

// Handler serves a lythrum:// request coming from a page. Returning ok false
// makes the request fail.
type Handler func(req SchemeRequest) (resp SchemeResponse, ok bool)

// SchemeHandler serves a request outside the GTK thread. The answer may come
// later: reading a file can take a moment and must not block the browser.
type SchemeHandler func(req SchemeRequest, reply func(resp SchemeResponse, ok bool))

// Message is a call from a page into the Go backend. The page posts
// {"id":"1","method":"clock","params":{}} and gets the answer through
// View.Reply, which resolves the pending promise in JavaScript.
type Message struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Engine owns the single GTK thread WebKit needs. Start it once per process.
type Engine struct {
	mu       sync.Mutex
	views    map[uintptr]*View
	nextID   uintptr
	handler  Handler
	scheme   SchemeHandler
	messages func(view *View, msg Message)
	logger   func(string)
	started  bool
	shutdown bool
}

var (
	engineMu sync.Mutex
	engine   *Engine
)

// Start boots the shared engine. Calling it again returns the same instance.
func Start() (*Engine, error) {
	engineMu.Lock()
	defer engineMu.Unlock()
	if engine != nil {
		return engine, nil
	}
	// The rendering environment is left exactly as it is: forcing a GDK
	// backend leaves the helper windows without a size and WebKit then cannot
	// take snapshots of them.
	if C.lv_engine_start() != 1 {
		return nil, ErrNoDisplay
	}
	engine = &Engine{views: map[uintptr]*View{}, nextID: 1}
	engine.started = true
	return engine, nil
}

// Running reports whether the shared engine is up.
func Running() bool {
	engineMu.Lock()
	defer engineMu.Unlock()
	return engine != nil && engine.started
}

// SetHandler installs the callback that serves lythrum:// requests.
// SetSchemeHandler installs an asynchronous lythrum:// handler.
func (e *Engine) SetSchemeHandler(h SchemeHandler) {
	e.mu.Lock()
	e.scheme = h
	e.mu.Unlock()
}

// SetMessageHandler installs the callback that answers page messages.
func (e *Engine) SetMessageHandler(h func(view *View, msg Message)) {
	e.mu.Lock()
	e.messages = h
	e.mu.Unlock()
}

func (e *Engine) SetHandler(h Handler) {
	e.mu.Lock()
	e.handler = h
	e.mu.Unlock()
}

// SetLogger receives JavaScript console lines and page titles.
func (e *Engine) SetLogger(fn func(string)) {
	e.mu.Lock()
	e.logger = fn
	e.mu.Unlock()
}

func (e *Engine) log(format string, args ...any) {
	e.mu.Lock()
	fn := e.logger
	e.mu.Unlock()
	if fn != nil {
		fn(fmt.Sprintf(format, args...))
	}
}

func (e *Engine) serve(req SchemeRequest, request uintptr) {
	reply := func(status int, mime string, body []byte, contentRange string) {
		payload := cBytes(body)
		head := cString(mime)
		contentRangeText := cString(contentRange)
		C.lv_scheme_reply(C.uintptr_t(request), C.int(status), head,
			payload, C.int(len(body)), contentRangeText)
		if payload != nil {
			C.free(unsafe.Pointer(payload))
		}
		if head != nil {
			C.free(unsafe.Pointer(head))
		}
		if contentRangeText != nil {
			C.free(unsafe.Pointer(contentRangeText))
		}
	}
	e.mu.Lock()
	async := e.scheme
	sync := e.handler
	e.mu.Unlock()
	if async != nil {
		async(req, func(resp SchemeResponse, ok bool) {
			if !ok {
				reply(404, "text/plain", []byte("not found"), "")
				return
			}
			status := resp.Status
			if status == 0 {
				status = 200
			}
			reply(status, resp.Mime, resp.Body, resp.ContentRange)
		})
		return
	}
	if sync == nil {
		reply(200, "text/plain", nil, "")
		return
	}
	h := sync
	resp, ok := h(req)
	if !ok {
		reply(404, "text/plain", []byte("not found"), "")
		return
	}
	status := resp.Status
	if status == 0 {
		status = 200
	}
	reply(status, resp.Mime, resp.Body, resp.ContentRange)
}

func (e *Engine) dispatch(view *View, payload string) {
	e.mu.Lock()
	h := e.messages
	e.mu.Unlock()
	if h == nil || view == nil {
		return
	}
	var msg Message
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		return
	}
	h(view, msg)
}

// drain runs queued work for one view, one job at a time so replies keep the
// order the page expects.
func (v *View) drain() {
	for job := range v.queue {
		job()
	}
}

// Dispatch runs work for the view outside the GTK thread. The job must not
// touch X11 or WebKit directly; Eval and Close are safe because they post back
// to the GTK thread themselves.
func (v *View) Dispatch(job func()) {
	if v == nil || job == nil {
		return
	}
	select {
	case v.queue <- job:
	default:
		// The page is far behind, so the newest request matters more.
		go job()
	}
}

func (e *Engine) lookup(id uintptr) *View {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.views[id]
}

// View is one page rendered off screen.
type View struct {
	engine *Engine
	handle uintptr

	// queue runs the work a page asks for away from the GTK thread, in the
	// order the page sent it. WebKit calls into Go from its own thread, and
	// walking the file system there would freeze every animation.
	queue chan func()

	closeOnce sync.Once

	width  int
	height int
	fps    int

	mu      sync.Mutex
	frame   Frame
	title   string
	loaded  bool
	crashed bool
	shots   int
}

type viewArgs struct {
	engine *Engine
	handle uintptr
	width  int
	height int
	fps    int
}

// NewView loads html at the given size and starts delivering frames at fps.
// A fps of zero means frames are only taken on demand through Snapshot.
func (e *Engine) NewView(width, height, fps int, html string) (*View, error) {
	return e.newView(width, height, fps, html, "")
}

func (e *Engine) newView(width, height, fps int, html, uri string) (*View, error) {
	return e.newViewEx(width, height, fps, uri, false, html)
}

func (e *Engine) newViewEx(width, height, fps int, uri string, visible bool, html ...string) (*View, error) {
	if width < 1 {
		width = 320
	}
	if height < 1 {
		height = 240
	}
	e.mu.Lock()
	id := e.nextID
	e.nextID++
	v := &View{
		engine: e, handle: id, width: width, height: height, fps: fps,
		queue: make(chan func(), 64),
	}
	go v.drain()
	e.views[id] = v
	e.mu.Unlock()

	page := ""
	if len(html) > 0 {
		page = html[0]
	}
	cHTML := C.CString(page)
	defer C.free(unsafe.Pointer(cHTML))
	cURI := C.CString(uri)
	defer C.free(unsafe.Pointer(cURI))
	C.lv_view_new_ex(C.uintptr_t(id), C.int(width), C.int(height), cHTML,
		nil, cURI, C.int(fps), C.int(0), boolToInt(visible))
	return v, nil
}

// NewViewURI loads a page from the lythrum scheme, so the page and its backend
// calls share one origin and the browser does not treat them as cross site.
func (e *Engine) NewViewURI(width, height, fps int, uri string) (*View, error) {
	v, err := e.newView(width, height, 0, "", uri)
	if err != nil {
		return nil, err
	}
	if fps > 0 {
		v.SetFPS(fps)
	}
	return v, nil
}

// NewVisibleView opens a page in a real on screen window. Fullscreen widgets
// use it, so the user can click buttons and type into the page.
func (e *Engine) NewVisibleView(width, height, fps int, uri string) (*View, error) {
	return e.newViewEx(width, height, fps, uri, true)
}

// NewTransparentView is like NewView but keeps the page background clear so
// the tile background shows through.
func (e *Engine) NewTransparentView(width, height, fps int, html string) (*View, error) {
	v, err := e.NewView(width, height, 0, html)
	if err != nil {
		return nil, err
	}
	v.SetFPS(fps)
	return v, nil
}

// Resize changes the viewport size.
func (v *View) Resize(width, height int) {
	if width < 1 || height < 1 {
		return
	}
	v.engine.mu.Lock()
	v.width, v.height = width, height
	v.engine.mu.Unlock()
	C.lv_view_configure(C.uintptr_t(v.handle), C.int(width), C.int(height), C.int(-1))
}

// SetFPS restarts or stops the automatic frame timer.
func (v *View) SetFPS(fps int) {
	v.engine.mu.Lock()
	v.fps = fps
	v.engine.mu.Unlock()
	C.lv_view_configure(C.uintptr_t(v.handle), C.int(0), C.int(0), C.int(fps))
}

// Eval runs JavaScript in the page.
func (v *View) Eval(js string) {
	if js == "" {
		return
	}
	cjs := C.CString(js)
	defer C.free(unsafe.Pointer(cjs))
	C.lv_view_eval(C.uintptr_t(v.handle), cjs)
}

// Click sends a click at a point of the page, which is how the shell feeds a
// tile sized window to a page that is rendered off screen.
func (v *View) Click(x, y int) {
	v.Eval(fmt.Sprintf("window.__lythrumPointer && window.__lythrumPointer(%d,%d,'click')", x, y))
}

// Hover moves the pointer over the page without clicking.
func (v *View) Hover(x, y int) {
	v.Eval(fmt.Sprintf("window.__lythrumPointer && window.__lythrumPointer(%d,%d,'move')", x, y))
}

// Key presses a key in the page by its name, for example "Enter" or "a".
func (v *View) Key(name, text string) {
	if name == "" {
		return
	}
	// text is the character the key produced: a keyboard layout in another
	// language does not share its letters with the key name.
	v.Eval(fmt.Sprintf("window.__lythrumKey && window.__lythrumKey(%s,%s)",
		jsString(name), jsString(text)))
}

// jsString quotes a string for the page.
func jsString(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(data)
}

// LoadURI opens a page by address, which keeps it on the widget origin.
func (v *View) LoadURI(uri string) {
	cURI := C.CString(uri)
	defer C.free(unsafe.Pointer(cURI))
	C.lv_view_load_uri(C.uintptr_t(v.handle), cURI)
}

// SetHTML replaces the page content.
func (v *View) SetHTML(html string) {
	cHTML := C.CString(html)
	defer C.free(unsafe.Pointer(cHTML))
	C.lv_view_load_html(C.uintptr_t(v.handle), cHTML)
}

// Load replaces the page content and restarts the frame timer.
func (v *View) Load(html string) {
	v.SetHTML(html)
	v.mu.Lock()
	v.loaded = false
	v.mu.Unlock()
}

// Close tears the view down and releases its GTK objects. Calling it twice is
// harmless, which matters because a widget can be closed from both the shell
// and the cache.
func (v *View) Close() {
	v.closeOnce.Do(func() {
		C.lv_view_free(C.uintptr_t(v.handle))
		v.engine.mu.Lock()
		delete(v.engine.views, v.handle)
		v.engine.mu.Unlock()
	})
}

// Frame returns the most recent picture, if any.
func (v *View) Frame() Frame {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.frame
}

// Title returns the current document title.
func (v *View) Title() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.title
}

// Loaded reports whether the page finished loading.
func (v *View) Loaded() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.loaded
}

// Crashed reports whether the web process died and the page needs a reload.
func (v *View) Crashed() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.crashed
}

// Snapshot asks for one frame without touching the frame timer.
func (v *View) Snapshot() {
	C.lv_view_configure(C.uintptr_t(v.handle), C.int(0), C.int(0), C.int(0))
}

// Shots returns how many frames the view delivered.
func (v *View) Shots() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.shots
}

func (v *View) putFrame(data []byte, w, h int) {
	v.mu.Lock()
	v.frame = Frame{Width: w, Height: h, Pixels: data}
	v.shots++
	v.mu.Unlock()
}

func (v *View) putTitle(title string) {
	v.mu.Lock()
	v.title = title
	v.mu.Unlock()
}

func (v *View) putLoad(event int) {
	switch event {
	case loadFinished:
		v.mu.Lock()
		v.loaded = true
		v.crashed = false
		v.mu.Unlock()
	case loadStarted, loadRedirect, loadCommitted:
		v.mu.Lock()
		v.loaded = false
		v.mu.Unlock()
	}
}

func (v *View) putCrash(reason int) {
	v.engine.log("webview: web process terminated (reason %d)", reason)
	v.mu.Lock()
	v.crashed = true
	v.mu.Unlock()
}

// Load events, mirroring WebKitLoadEvent.
const (
	loadStarted   = 0
	loadRedirect  = 1
	loadCommitted = 2
	loadFinished  = 3
)

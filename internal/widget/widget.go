// Package widget hosts the small programs that live inside start screen
// tiles. Each widget is an HTML/CSS/JS page served over the lythrum scheme,
// talking to this package through the webview message bridge.
package widget

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed assets
var assetFS embed.FS

// Widget describes one mini program.
type Widget struct {
	// ID is the stable name used by desktop entries, X-Lythrum-Widget.
	ID string
	// Title is the name shown in the shell.
	Title string
	// Group is the start screen band the widget belongs to.
	Group string
	// Background is the colour the tile paints behind the page.
	Background string
	// Fullscreen marks widgets that open across the screen.
	Fullscreen bool
	// Accent colours the small header in the shell.
	Accent string
	// FPS is how often the tile needs a new frame. A clock is happy with a
	// few frames per second, a slideshow needs more.
	FPS int
}

// Registry holds every widget the shell knows about.
type Registry struct {
	mu      sync.RWMutex
	byID    map[string]*Widget
	order   []string
	api     map[string]func(Session, json.RawMessage) (any, error)
	session Session
}

// Session is what a widget backend can ask the shell about. The shell fills
// it in; tests use a stub.
type Session struct {
	// Now is the clock, so widgets can be tested at a fixed time.
	Now func() time.Time
	// Config returns the current shell settings as raw JSON.
	Config func() ([]byte, error)
	// SetConfig replaces part of the settings and answers with the whole
	// result, so the page can show what was really applied.
	SetConfig func(json.RawMessage) ([]byte, error)
	// OpenURL hands a link to the shell.
	OpenURL func(string)
	// Theme exposes the colours a page should follow.
	Theme func() map[string]string
	// Info describes the running shell, so the settings page can show what is
	// actually in use instead of guessing.
	Info func() map[string]any
	// Actions lists the actions a key binding may trigger.
	Actions func() []Action
}

// Action is something a key binding or the IPC can trigger.
type Action struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Group string `json:"group"`
	// NeedsArgument marks the actions that only make sense with a value.
	NeedsArgument bool `json:"needsArgument"`
}

// ErrUnknown marks a method nobody implements.
var ErrUnknown = errors.New("widget: unknown method")

// NewRegistry returns the registry with the built-in widgets and API.
func NewRegistry(session Session) *Registry {
	r := &Registry{
		byID:    map[string]*Widget{},
		api:     map[string]func(Session, json.RawMessage) (any, error){},
		session: session,
	}
	for _, w := range builtin() {
		r.Register(w)
	}
	registerAPI(r)
	return r
}

// Register adds a widget, replacing an earlier one with the same id.
func (r *Registry) Register(w Widget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := w
	if _, exists := r.byID[w.ID]; !exists {
		r.order = append(r.order, w.ID)
	}
	r.byID[w.ID] = &copied
}

// Handle registers a backend method.
func (r *Registry) Handle(method string, fn func(Session, json.RawMessage) (any, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.api[method] = fn
}

// Lookup returns a widget by id.
func (r *Registry) Lookup(id string) (*Widget, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	copied := *w
	return &copied, true
}

// All returns the widgets in registration order.
func (r *Registry) All() []*Widget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Widget, 0, len(r.order))
	for _, id := range r.order {
		if w, ok := r.byID[id]; ok {
			copied := *w
			out = append(out, &copied)
		}
	}
	return out
}

// Call runs a backend method and returns the JSON encoded answer.
func (r *Registry) Call(method string, params json.RawMessage) ([]byte, error) {
	r.mu.RLock()
	fn := r.api[method]
	r.mu.RUnlock()
	if fn == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknown, method)
	}
	result, err := fn(r.session, params)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// Serve answers a lythrum:// request. The shell uses two shapes:
// lythrum://app/<widget>/index.html for a page and lythrum://app/<file> for
// the assets the pages share.
func (r *Registry) Serve(rawurl string) (string, []byte, bool) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "", nil, false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	parts = removeEmpty(parts)
	if len(parts) == 0 {
		return serveAsset("clock", "index.html")
	}
	// A widget directory wins when it holds the requested file.
	if hasAssets(parts[0], "index.html") {
		return serveAsset(parts[0], strings.Join(parts[1:], "/"))
	}
	return serveAsset("", strings.Join(parts, "/"))
}

func removeEmpty(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ServeFile answers a lythrum://app/file/... request with the bytes of a file
// from the home directory, including byte ranges for media playback.
func (r *Registry) ServeFile(rawurl, fileRange string) (mime string, body []byte, status int, contentRange string, ok bool) {
	mime, body, status, contentRange, err := StreamFile(rawurl, fileRange)
	if err != nil {
		return "", nil, 404, "", false
	}
	return mime, body, status, contentRange, true
}

func serveAsset(dir, name string) (string, []byte, bool) {
	if name == "" {
		name = "index.html"
	}
	if strings.Contains(name, "..") || strings.Contains(dir, "..") {
		return "", nil, false
	}
	data, err := fs.ReadFile(assetFS, path.Join("assets", dir, name))
	if err != nil {
		return "", nil, false
	}
	return mimeFor(name), data, true
}

// hasAssets reports whether a widget ships the given file, which is how the
// router tells a widget directory from the shared assets one.
func hasAssets(dir, name string) bool {
	_, err := fs.Stat(assetFS, path.Join("assets", dir, name))
	return err == nil
}

func mimeFor(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".json":
		return "application/json"
	case ".woff2":
		return "font/woff2"
	}
	return "application/octet-stream"
}

// URL returns the address a widget page is loaded from.
func URL(widgetID string) string {
	return "lythrum://app/" + widgetID + "/index.html"
}

func builtin() []Widget {
	return []Widget{
		{
			ID:         "clock",
			Title:      "Часы и календарь",
			Group:      "tools",
			Background: "#10141c",
			Accent:     "#6ee7ff",
			Fullscreen: true,
			FPS:        4,
		},
		{
			ID:         "sysmon",
			Title:      "Система",
			Group:      "tools",
			Background: "#0d1117",
			Accent:     "#7ee787",
			Fullscreen: true,
			FPS:        2,
		},
		{
			ID:         "settings",
			Title:      "Настройки оболочки",
			Group:      "tools",
			Background: "#12121a",
			Accent:     "#ffd166",
			Fullscreen: true,
			FPS:        1,
		},
		{
			ID:         "photos",
			Title:      "Фото и видео",
			Group:      "media",
			Background: "#0b0f14",
			Accent:     "#f78c6c",
			Fullscreen: true,
			FPS:        6,
		},
		{
			ID:         "music",
			Title:      "Музыка",
			Group:      "media",
			Background: "#120b14",
			Accent:     "#e879f9",
			Fullscreen: true,
			FPS:        6,
		},
		{
			ID:         "search",
			Title:      "Поиск",
			Group:      "tools",
			Background: "#0e1116",
			Accent:     "#93c5fd",
			Fullscreen: true,
			FPS:        2,
		},
	}
}

// GroupOrder lists the bands widgets are placed in.
func GroupOrder() []string {
	groups := map[string]bool{}
	for _, w := range builtin() {
		groups[w.Group] = true
	}
	out := make([]string, 0, len(groups))
	for g := range groups {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

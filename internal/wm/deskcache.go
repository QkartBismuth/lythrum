package wm

import (
	"sync"
	"time"

	"lythrum/internal/desk"
)

// deskScanTTL is how long a tile list stays fresh. The shell rescans when the
// start screen opens after this time, and the "desktop_scan" action forces a
// scan right away.
const deskScanTTL = 30 * time.Second

// deskCache keeps the tiles of the start screen. Collecting them means reading
// every .desktop file in the system directories and walking the home folders,
// which is far too slow for the event loop, so the scan runs on its own
// goroutine and the loop picks the result up through poll.
type deskCache struct {
	wm *WM

	mu      sync.Mutex
	entries []desk.Entry
	err     error
	scanned time.Time
	running bool
	// generation counts finished scans; applied remembers the one the event
	// loop has already turned into tiles, so a result is never used twice.
	generation uint64
	applied    uint64
}

func newDeskCache(w *WM) *deskCache { return &deskCache{wm: w} }

// options reads the scan settings from the configuration the shell is running
// with.
func (c *deskCache) options() desk.CatalogOptions {
	cfg := c.wm.config()
	opts := desk.DefaultCatalogOptions(desk.Dir())
	opts.Applications = cfg.GridApps
	opts.HomeFiles = cfg.GridHomeFiles
	if cfg.GridFileLimit > 0 {
		opts.FileLimit = cfg.GridFileLimit
	}
	return opts
}

// list returns the last finished scan, which is empty until the first one lands.
func (c *deskCache) list() []desk.Entry {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries
}

// scan starts a background scan unless one is already running and the list is
// still fresh. A forced scan ignores the age of the list, which is what the
// desktop_scan action asks for.
func (c *deskCache) scan(force bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	if !force && !c.scanned.IsZero() && time.Since(c.scanned) < deskScanTTL {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	opts := c.options()
	go func() {
		entries, err := desk.Catalog(opts)
		c.mu.Lock()
		c.entries, c.err = entries, err
		c.scanned = time.Now()
		c.running = false
		c.generation++
		c.mu.Unlock()
	}()
}

// poll reports whether a scan finished since the last call.
func (c *deskCache) poll() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation == c.applied {
		return false
	}
	c.applied = c.generation
	return true
}

// err reports why the last scan failed, if it did.
func (c *deskCache) scanErr() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// refreshTiles picks up a finished scan and rebuilds everything that shows a
// tile. It is called from the event loop only.
func (w *WM) refreshTiles() {
	if w.start != nil {
		w.start.scan()
	}
	if w.desktop != nil {
		w.desktop.scan()
	}
}

// deskEntries returns the tiles the shell should show, falling back to the
// desktop directory alone when there is no cache, which is what the tests
// build by hand.
func (w *WM) deskEntries() []desk.Entry {
	if w.desk != nil {
		return w.desk.list()
	}
	entries, err := desk.Entries(desk.Dir())
	if err != nil {
		return nil
	}
	return entries
}

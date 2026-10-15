package wm

import (
	"context"
	"image"
	"sync"
	"time"

	"lythrum/internal/preview"
)

// previewQueue builds file previews in the background. Tiles ask it for a
// ready picture and never wait: while a video thumbnail is being extracted the
// tile keeps its glyph, and the start screen repaints when the picture lands.
type previewQueue struct {
	wm      *WM
	service *preview.Service
	jobs    chan previewJob
	ready   chan string
	mu      sync.Mutex
	running map[string]bool
}

type previewJob struct {
	path string
	size int
}

// previewCacheLimit keeps the queue from working on files nobody looks at.
const previewCacheLimit = 32

func newPreviewQueue(w *WM, service *preview.Service) *previewQueue {
	q := &previewQueue{
		wm:      w,
		service: service,
		jobs:    make(chan previewJob, 64),
		ready:   make(chan string, 64),
		running: map[string]bool{},
	}
	go q.worker()
	return q
}

// key identifies one request, so the same file is never built twice.
func previewKey(path string, size int) string {
	return preview.CacheKey(path, preview.Options{Width: size, Height: size})
}

// Get returns a picture that is ready, and asks for one that is not.
func (q *previewQueue) Get(path string, size int) *image.RGBA {
	if q == nil || q.service == nil {
		return nil
	}
	if img, ok := q.service.Cached(path, preview.Options{Width: size, Height: size}); ok {
		return img
	}
	q.Request(path, size)
	return nil
}

// Request schedules a preview, unless one is already on the way.
func (q *previewQueue) Request(path string, size int) {
	key := previewKey(path, size)
	q.mu.Lock()
	if q.running[key] || len(q.running) >= previewCacheLimit {
		q.mu.Unlock()
		return
	}
	q.running[key] = true
	q.mu.Unlock()
	select {
	case q.jobs <- previewJob{path: path, size: size}:
	default:
		q.mu.Lock()
		delete(q.running, key)
		q.mu.Unlock()
	}
}

// Poll reports whether a preview finished since the last call, which is the
// cue to repaint the tiles.
func (q *previewQueue) Poll() bool {
	if q == nil {
		return false
	}
	changed := false
	for {
		select {
		case key := <-q.ready:
			q.mu.Lock()
			delete(q.running, key)
			q.mu.Unlock()
			changed = true
		default:
			return changed
		}
	}
}

func (q *previewQueue) worker() {
	for job := range q.jobs {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		q.service.Produce(ctx, job.path, preview.Options{Width: job.size, Height: job.size})
		cancel()
		select {
		case q.ready <- previewKey(job.path, job.size):
		default:
		}
	}
}

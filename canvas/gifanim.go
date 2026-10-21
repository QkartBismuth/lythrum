package main

import (
	"image"
	"image/gif"
	"os"
	"sync/atomic"
	"time"
)

// The animated icons share one ticker: it wakes the board when a frame has to
// change.
var (
	animActive atomic.Bool
	animFps    atomic.Int32
)

// maxGIFFrames limits the memory a single animation may take.
const maxGIFFrames = 120

// gifAnim is an animated picture: the frames of a GIF and how long every one
// of them is shown.
type gifAnim struct {
	frames []image.Image
	delays []time.Duration
	total  time.Duration
	start  time.Time
}

// decodeGIF reads the frames of a GIF. The frames are scaled down to the size
// of an icon, so that a large animation does not eat the memory.
func decodeGIF(path string) *gifAnim {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil || len(g.Image) == 0 {
		return nil
	}
	a := &gifAnim{start: time.Now()}
	for i, img := range g.Image {
		if i >= maxGIFFrames {
			break
		}
		a.frames = append(a.frames, scaleImage(img, thumbSize))
		d := time.Duration(g.Delay[i]) * 10 * time.Millisecond
		if d <= 0 {
			d = 100 * time.Millisecond
		}
		a.delays = append(a.delays, d)
		a.total += d
	}
	if len(a.frames) == 0 {
		return nil
	}
	return a
}

// frameIndex returns the number of the frame that is shown at a given moment.
func (a *gifAnim) frameIndex(now time.Time) int {
	if len(a.frames) == 0 {
		return -1
	}
	if a.total <= 0 {
		return 0
	}
	elapsed := now.Sub(a.start) % a.total
	if elapsed < 0 {
		elapsed += a.total
	}
	var acc time.Duration
	for i, d := range a.delays {
		acc += d
		if elapsed < acc {
			return i
		}
	}
	return len(a.frames) - 1
}

// frameAt returns the frame that is shown at a given moment.
func (a *gifAnim) frameAt(now time.Time) image.Image {
	if len(a.frames) == 0 {
		return nil
	}
	if a.total <= 0 {
		return a.frames[0]
	}
	elapsed := now.Sub(a.start) % a.total
	if elapsed < 0 {
		elapsed += a.total
	}
	var acc time.Duration
	for i, d := range a.delays {
		acc += d
		if elapsed < acc {
			return a.frames[i]
		}
	}
	return a.frames[len(a.frames)-1]
}

// scaleImage shrinks a picture so that it fits into a square of the given
// size.
func scaleImage(src image.Image, max int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}
	if w <= max && h <= max {
		return src
	}
	scale := float64(max) / float64(maxOf(w, h))
	nw, nh := int(float64(w)*scale), int(float64(h)*scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

// maxOf returns the larger of two numbers.
func maxOf(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// startAnimTicker wakes the board while an animated icon is on the screen.
// The wake reuses the mechanism of the background workers: a message on the
// secondary connection.
func startAnimTicker() {
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		last := time.Now()
		for range tick.C {
			if !animActive.Load() {
				last = time.Now()
				continue
			}
			fps := int(animFps.Load())
			if fps <= 0 {
				fps = 12
			}
			step := time.Duration(float64(time.Second) / float64(fps))
			if time.Since(last) < step {
				continue
			}
			last = time.Now()
			wake()
		}
	}()
}

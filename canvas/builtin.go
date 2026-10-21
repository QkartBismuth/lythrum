package main

/*
#include <X11/Xlib.h>
*/
import "C"

import "math"

// The speeds of the board are read from the settings, so that the pointer
// feels the same way on every machine and can be tuned from the settings app.
func (v *view) panSpeed() float64 {
	s := v.cfg.Mouse.PanSpeed
	if s <= 0 {
		return 1
	}
	return s
}

func (v *view) wheelSpeed() float64 {
	s := v.cfg.Mouse.WheelSpeed
	if s <= 0 {
		return 1
	}
	return s
}

func (v *view) zoomSpeed() float64 {
	s := v.cfg.Mouse.ZoomSpeed
	if s <= 0 {
		return 1
	}
	return s
}

// setStatus shows a message in the status bar of the board.
func (v *view) setStatus(text string) {
	v.status = text
	v.redraw = true
}

// zoomWheel zooms the board at the pointer by one wheel step, honouring the
// zoom speed of the settings.
func (v *view) zoomWheel(x, y int, dir int) {
	factor := math.Pow(zWheel, v.zoomSpeed())
	if dir < 0 {
		factor = 1 / factor
	}
	if v.cfg.Gestures.Invert {
		factor = 1 / factor
	}
	v.zoomAt(float64(x), float64(y), factor)
}

// wakeUI asks the event loop to paint again. The background workers use it
// when they have something new to show.
func (v *view) wakeUI() {
	wake()
	v.redraw = true
}

// isqrt is the integer square root: the largest n with n*n <= v.
func isqrt(v int) int {
	if v <= 0 {
		return 0
	}
	n := v
	m := (n + 1) / 2
	for m < n {
		n = m
		m = (n + v/n) / 2
	}
	return n
}

// roundInset is how far the edge of a rounded corner is pushed in on the row
// dy below the top of the shape: the corner is a quarter circle of radius r.
func roundInset(r, dy int) int {
	if r <= 0 || dy >= r {
		return 0
	}
	d := r - dy
	return r - isqrt(r*r-d*d)
}

// fillRound fills a rectangle with rounded corners.
func (v *view) fillRound(x, y, w, h, r int, c color) {
	if r > h/2 {
		r = h / 2
	}
	if r > w/2 {
		r = w / 2
	}
	if r <= 0 {
		v.fill(x, y, w, h, c)
		return
	}
	for dy := 0; dy < h; dy++ {
		inset := 0
		switch {
		case dy < r:
			inset = roundInset(r, dy)
		case dy >= h-r:
			inset = roundInset(r, h-1-dy)
		}
		if inset*2 >= w {
			v.fill(x, y+dy, w, 1, c)
			continue
		}
		v.fill(x+inset, y+dy, w-inset*2, 1, c)
	}
}

// fillRoundTop fills a rectangle whose top corners are rounded and whose
// colour changes from top to bottom: the title bar of a window.
func (v *view) fillRoundTop(x, y, w, h, r int, top, bottom color) {
	if r > h/2 {
		r = h / 2
	}
	if r > w/2 {
		r = w / 2
	}
	if h < 1 {
		return
	}
	for dy := 0; dy < h; dy++ {
		col := mix(top, bottom, float64(dy)/float64(h-1))
		inset := roundInset(r, dy)
		if inset*2 >= w {
			v.fill(x, y+dy, w, 1, col)
			continue
		}
		v.fill(x+inset, y+dy, w-inset*2, 1, col)
	}
}

// bevel draws the raised or the sunken frame of the classic style.
func (v *view) bevel(x, y, w, h int, sunken bool) {
	light := color{0xff, 0xff, 0xff}
	dark := color{0x80, 0x80, 0x80}
	if sunken {
		light, dark = dark, light
	}
	// The top and the left edges catch the light.
	v.fg(light)
	C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x), C.int(y+h-1), C.int(x), C.int(y))
	C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x), C.int(y), C.int(x+w-1), C.int(y))
	// The bottom and the right edges are in the shadow.
	v.fg(dark)
	C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x), C.int(y+h-1), C.int(x+w-1), C.int(y+h-1))
	C.XDrawLine(v.dpy, v.pm, v.gc, C.int(x+w-1), C.int(y), C.int(x+w-1), C.int(y+h-1))
}

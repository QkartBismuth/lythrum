package webview

import (
	"image"
	"testing"
)

func TestFrameAtAndImage(t *testing.T) {
	frame := Frame{Width: 2, Height: 1, Pixels: []byte{
		1, 2, 3, 255, 4, 5, 6, 128,
	}}
	r, g, b, a, ok := frame.At(0, 0)
	if !ok || r != 1 || g != 2 || b != 3 || a != 255 {
		t.Fatalf("At(0,0) = %d,%d,%d,%d ok=%v", r, g, b, a, ok)
	}
	if _, _, _, _, ok := frame.At(5, 5); ok {
		t.Error("a point outside the frame must not report a pixel")
	}
	if _, _, _, _, ok := frame.At(-1, 0); ok {
		t.Error("a negative point must not report a pixel")
	}

	img := frame.Image()
	if img.Rect != image.Rect(0, 0, 2, 1) {
		t.Errorf("image rect = %v", img.Rect)
	}
	if img.Pix[0] != 1 || img.Pix[4] != 4 {
		t.Error("the image must keep the pixel order of the frame")
	}
}

func TestFrameAtWithoutPixels(t *testing.T) {
	var empty Frame
	if _, _, _, _, ok := empty.At(0, 0); ok {
		t.Error("an empty frame has no pixels")
	}
	if empty.Image() == nil {
		t.Error("an empty frame still converts to an image")
	}
}

func TestEngineDispatchDecodesMessages(t *testing.T) {
	engine := &Engine{views: map[uintptr]*View{}, nextID: 2}
	view := &View{engine: engine, handle: 1}
	engine.views[1] = view

	var got Message
	engine.messages = func(v *View, msg Message) {
		if v != view {
			t.Errorf("message went to the wrong view")
		}
		got = msg
	}
	engine.dispatch(view, `{"id":"7","method":"clock.now","params":{"tz":3}}`)
	if got.ID != "7" || got.Method != "clock.now" {
		t.Fatalf("message = %+v", got)
	}
	if string(got.Params) != `{"tz":3}` {
		t.Errorf("params = %s", got.Params)
	}

	// Broken input must not reach the handler.
	engine.dispatch(view, `not json`)
	if got.Method != "clock.now" {
		t.Error("a broken payload must be ignored")
	}
}

func TestEngineDispatchWithoutHandler(t *testing.T) {
	engine := &Engine{views: map[uintptr]*View{}}
	view := &View{engine: engine, handle: 1}
	engine.dispatch(view, `{"id":"1","method":"x"}`)
}

func TestViewStateIsSafeWithoutEngine(t *testing.T) {
	v := &View{title: "hello", loaded: true, shots: 3}
	if v.Title() != "hello" || !v.Loaded() || v.Shots() != 3 || v.Crashed() {
		t.Error("view state is not reported correctly")
	}
	if v.Frame().Pixels != nil {
		t.Error("a view without frames must report an empty frame")
	}
}

func TestLookupReturnsNilForUnknownView(t *testing.T) {
	engine := &Engine{views: map[uintptr]*View{}}
	if engine.lookup(99) != nil {
		t.Error("an unknown handle must not resolve to a view")
	}
}

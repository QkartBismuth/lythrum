package main

/*
#cgo pkg-config: x11
#include <X11/Xlib.h>
#include <stdlib.h>
static Display *probe_open(const char *name) { return XOpenDisplay(name); }
static unsigned long probe_root(Display *d) { return (unsigned long)DefaultRootWindow(d); }
*/
import "C"
import (
	"fmt"
	"os"
	"time"

	"lythrum/internal/webview"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "x11" {
		name := C.CString(os.Getenv("DISPLAY"))
		d := C.probe_open(name)
		fmt.Println("plain x11 open:", d != nil, "root:", uint64(C.probe_root(d)))
	}
	engine, err := webview.Start()
	if err != nil {
		fmt.Println("webview:", err)
		return
	}
	view, _ := engine.NewView(480, 320, 5, "<html><body style='background:#204080;color:#fff;font:20px sans-serif'>probe</body></html>")
	for i := 0; i < 100 && view.Shots() == 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println("frames:", view.Shots())
}

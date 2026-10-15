// Command wmprobe checks whether the web engine can start in a process that
// already holds an X11 connection, which is what the window manager does.
package main

import (
	"fmt"
	"os"
	"time"

	"lythrum/internal/webview"
	"lythrum/internal/x11"
)

func main() {
	d, err := x11.Open(os.Getenv("DISPLAY"))
	if err != nil {
		fmt.Println("cannot open the display:", err)
		os.Exit(1)
	}
	defer d.Close()
	fmt.Println("x11 connection open, root:", d.Root())

	engine, err := webview.Start()
	if err != nil {
		fmt.Println("webview:", err)
		os.Exit(1)
	}
	view, err := engine.NewView(320, 200, 5, "<html><body style='background:#204080;color:#fff;font:20px sans-serif'>probe</body></html>")
	if err != nil {
		fmt.Println("view:", err)
		os.Exit(1)
	}
	for i := 0; i < 150 && view.Shots() == 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println("frames:", view.Shots(), "loaded:", view.Loaded(), "crashed:", view.Crashed())
	if frame := view.Frame(); frame.Pixels != nil {
		r, g, b, a, _ := frame.At(10, 10)
		fmt.Println("pixel:", r, g, b, a, frame.Width, frame.Height)
	}
}

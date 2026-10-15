// Command xsnap saves a screenshot of the root window as a PNG. It exists to
// inspect a running window manager without extra X tooling installed.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"

	"lythrum/internal/x11"
)

func main() {
	var (
		display = flag.String("display", "", "X display (default $DISPLAY)")
		out     = flag.String("out", "screenshot.png", "where to write the PNG")
		window  = flag.String("window", "root", "\"root\" or a window id in hexadecimal")
	)
	flag.Parse()

	d, err := x11.Open(*display)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xsnap:", err)
		os.Exit(1)
	}
	defer d.Close()

	target := d.Root()
	if *window != "root" {
		var id uint64
		if _, err := fmt.Sscanf(*window, "0x%x", &id); err != nil {
			fmt.Fprintln(os.Stderr, "xsnap: window must be root or 0x…")
			os.Exit(1)
		}
		target = x11.Window(id)
	}

	geom, err := d.Geometry(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xsnap:", err)
		os.Exit(1)
	}
	w, h := int(geom.Width), int(geom.Height)
	img, err := d.Capture(target, 0, 0, w, h)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xsnap:", err)
		os.Exit(1)
	}

	file, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xsnap:", err)
		os.Exit(1)
	}
	defer file.Close()
	if err := png.Encode(file, image.Image(img)); err != nil {
		fmt.Fprintln(os.Stderr, "xsnap:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%dx%d)\n", *out, w, h)
}

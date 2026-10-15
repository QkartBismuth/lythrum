// Command uploadtest paints the same picture several times into one window,
// which is what the start screen does with tiles and live widgets.
package main

import (
	"fmt"
	"image"
	"image/color"
	"os"

	"lythrum/internal/x11"
)

func main() {
	d, err := x11.Open(os.Getenv("DISPLAY"))
	if err != nil {
		fmt.Println("open:", err)
		return
	}
	defer d.Close()
	win, err := d.CreateWindow(x11.CreateWindowParams{
		Parent: d.Root(), X: 0, Y: 0, Width: 600, Height: 200,
		Class: x11.ClassInputOutput, BackPixel: 0x00101014,
		OverrideRedirect: true, EventMask: x11.ExposureMask,
	})
	if err != nil {
		fmt.Println("window:", err)
		return
	}
	d.MapWindow(win)
	gc, err := d.CreateGC(win)
	if err != nil {
		fmt.Println("gc:", err)
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for i := range img.Pix {
		img.Pix[i] = uint8(120 + i%80)
	}
	for i := 0; i < 5; i++ {
		err := d.UploadImageOver(win, gc, img, 10+i*110, 50, color.RGBA{R: 0x20, G: 0x20, B: 0x30, A: 0xff})
		fmt.Printf("upload %d: %v\n", i, err)
	}
	d.Flush()
	d.Sync()
	fmt.Println("done")
	select {}
}

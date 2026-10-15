// Command xdemo opens plain X11 windows. It exists so the window manager can
// be exercised without depending on a particular terminal emulator.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"lythrum/internal/x11"
)

func main() {
	var (
		display  = flag.String("display", "", "X display (default $DISPLAY)")
		count    = flag.Int("n", 1, "number of windows to open")
		title    = flag.String("title", "xdemo", "window title")
		width    = flag.Int("w", 400, "window width")
		height   = flag.Int("h", 300, "window height")
		lifetime = flag.Duration("lifetime", 0, "exit after this long (0 means run until killed)")
	)
	flag.Parse()

	d, err := x11.Open(*display)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xdemo:", err)
		os.Exit(1)
	}
	defer d.Close()

	var windows []x11.Window
	for i := 0; i < *count; i++ {
		win, err := d.CreateWindow(x11.CreateWindowParams{
			Parent:    d.Root(),
			X:         50 + i*30,
			Y:         50 + i*30,
			Width:     uint(*width),
			Height:    uint(*height),
			Class:     x11.ClassInputOutput,
			BackPixel: 0x303030,
			EventMask: x11.StructureNotifyMask | x11.ExposureMask | x11.FocusChangeMask,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "xdemo:", err)
			os.Exit(1)
		}
		windows = append(windows, win)
		name := *title
		if *count > 1 {
			name = *title + " " + strconv.Itoa(i+1)
		}
		d.SetUTF8(win, x11.A.NETWMName, name)
		d.SetUTF8(win, x11.A.WMIconName, name)
		d.SetProperty(win, x11.A.WMClass, x11.A.String, 8, []byte("xdemo\x00XDemo\x00"))
		d.SetCardinal(win, x11.A.NETWMPID, x11.A.Cardinal, uint32(os.Getpid()))
		d.SetCardinals(win, x11.A.WMProtocols, x11.A.Atom, []uint32{uint32(x11.A.WMDeleteWindow)})
		d.MapRaised(win)
		fmt.Fprintf(os.Stderr, "xdemo: created and mapped 0x%x (%s)\n", uint32(win), name)
	}
	d.Sync()
	for _, win := range windows {
		if a, err := d.Attributes(win); err == nil {
			fmt.Fprintf(os.Stderr, "xdemo: window 0x%x map_state=%d viewable=%v\n", uint32(win), a.MapState, a.MapState == x11.StateViewable)
		} else {
			fmt.Fprintf(os.Stderr, "xdemo: window 0x%x attributes failed: %v\n", uint32(win), err)
		}
	}

	if *lifetime > 0 {
		time.Sleep(*lifetime)
		return
	}
	fmt.Println("xdemo: windows mapped, press Ctrl+C to close")
	for {
		ev, err := d.NextEvent()
		if err != nil {
			return
		}
		if _, ok := ev.(x11.MapRequest); ok {
			continue
		}
	}
}

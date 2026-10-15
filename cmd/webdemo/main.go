// Command webdemo renders an off screen page with WebKitGTK and writes the
// frames it produces as PNG files. It is the manual check for the webview
// runtime: run it on any X display and look at the frames.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"time"

	"lythrum/internal/webview"
)

const page = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
  html, body { margin: 0; height: 100%; background: linear-gradient(135deg, #16213e, #2b5876); color: #eaf6ff;
    font: 16px/1.4 sans-serif; display: grid; place-items: center; }
  #clock { font-size: 64px; font-variant-numeric: tabular-nums; }
  #api { font-size: 14px; opacity: .8; margin-top: 8px; }
  #ball { width: 40px; height: 40px; border-radius: 50%; background: #6ee7ff; position: absolute; }
</style>
</head>
<body>
  <div style="text-align:center">
    <div id="clock">--:--:--</div>
    <div id="api">запрос не выполнен</div>
  </div>
  <div id="ball"></div>
<script>
  // Minimal bridge to the Go backend: calls travel over the script message
  // handler, answers resolve the pending promise.
  let nextId = 0;
  const pending = new Map();
  window.__lythrumResolve = (id, payload) => {
    const entry = pending.get(id);
    if (!entry) return;
    pending.delete(id);
    entry(payload);
  };
  window.lythrum = {
    call(method, params) {
      const id = String(++nextId);
      return new Promise((resolve, reject) => {
        pending.set(id, resolve);
        window.webkit.messageHandlers.lythrum.postMessage(JSON.stringify({id, method, params}));
        setTimeout(() => { if (pending.delete(id)) reject('таймаут'); }, 4000);
      });
    }
  };
  const clock = document.getElementById('clock');
  const api = document.getElementById('api');
  const ball = document.getElementById('ball');
  let frames = 0, x = 0;
  const STATIC = new URLSearchParams(location.search).has('static');
  function tick() {
    frames++;
    if (STATIC && frames > 1) return;
    clock.textContent = new Date().toLocaleTimeString('ru-RU');
    x = (x + 7) % (window.innerWidth - 40);
    ball.style.transform = 'translateX(' + x + 'px)';
    // Ask the Go backend for something, to prove the bridge works.
    window.lythrum.call('stats', {frames: frames})
      .then(data => { api.textContent = 'кадров ' + frames + ', api ' + JSON.stringify(data); })
      .catch(e => { api.textContent = 'ошибка api: ' + e; });
    requestAnimationFrame(tick);
  }
  requestAnimationFrame(tick);
</script>
</body>
</html>`

func main() {
	out := flag.String("out", "/tmp/webdemo", "prefix for the written PNG frames")
	frames := flag.Int("frames", 3, "how many frames to keep")
	every := flag.Duration("every", 700*time.Millisecond, "delay between frames")
	width := flag.Int("width", 480, "viewport width")
	height := flag.Int("height", 320, "viewport height")
	fps := flag.Int("fps", 10, "frames per second")
	staticPage := flag.Bool("static", false, "load a page that does not animate")
	plainPage := flag.Bool("plain", false, "load a bare page with no scripts at all")
	flag.Parse()

	engine, err := webview.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "webview:", err)
		os.Exit(1)
	}
	if os.Getenv("DEMO_DELAY") != "" {
		d, _ := time.ParseDuration(os.Getenv("DEMO_DELAY"))
		time.Sleep(d)
	}
	engine.SetHandler(func(req webview.SchemeRequest) (webview.SchemeResponse, bool) {
		fmt.Println("scheme request:", req.URL)
		if strings.HasSuffix(req.URL, "index.html") {
			return webview.SchemeResponse{
				Mime: "text/html; charset=utf-8", Body: []byte(page),
			}, true
		}
		return webview.SchemeResponse{
			Mime: "application/json", Body: []byte(`{"frames":3,"ok":true}`),
		}, true
	})
	engine.SetLogger(func(line string) { fmt.Println("log:", line) })
	engine.SetMessageHandler(func(v *webview.View, msg webview.Message) {
		fmt.Println("message:", msg.Method, string(msg.Params))
		answer, _ := json.Marshal(map[string]any{"method": msg.Method, "frames": 42, "ok": true})
		v.Eval(fmt.Sprintf("window.__lythrumResolve(%q, %s)", msg.ID, answer))
	})

	html := page
	if *plainPage {
		html = "<html><body style='margin:0;background:#ff0000'><h1 style='color:#00ff00'>HELLO</h1></body></html>"
	}
	if *staticPage && !*plainPage {
		html += "<script>location.replace(location.href.split('?')[0] + '?static');</script>"
	}
	if os.Getenv("DEMO_MANY") != "" {
		n := 4
		var views []*webview.View
		for i := 0; i < n; i++ {
			v, err := engine.NewViewURI(*width, *height, *fps, "lythrum://app/index.html")
			if err != nil {
				fmt.Fprintln(os.Stderr, "webview:", err)
				os.Exit(1)
			}
			views = append(views, v)
		}
		time.Sleep(8 * time.Second)
		for i, v := range views {
			fmt.Printf("view %d: shots=%d loaded=%v\n", i, v.Shots(), v.Loaded())
		}
		for _, v := range views {
			v.Close()
		}
		return
	}

	view, err := engine.NewViewURI(*width, *height, *fps, "lythrum://app/index.html")
	if err != nil {
		fmt.Fprintln(os.Stderr, "webview:", err)
		os.Exit(1)
	}
	defer view.Close()

	deadline := time.Now().Add(8 * time.Second)
	for i := 0; i < *frames; i++ {
		if os.Getenv("DEMO_EVAL") != "" {
			view.Eval("console.log('viewport', window.innerWidth, window.innerHeight)")
		}
		// Wait for the first paint, otherwise the frame is still blank.
		for start := time.Now(); time.Since(start) < 10*time.Second; {
			if view.Loaded() && view.Shots() > 3 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		last := view.Shots()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && (last == view.Shots() || view.Shots() == 0) {
			time.Sleep(50 * time.Millisecond)
		}
		shot := view.Frame()
		r, g, b, a, _ := shot.At(3, 3)
		fmt.Printf("shot %d: px=%d,%d,%d,%d %s\n", view.Shots(), r, g, b, a, stats(shot))
		time.Sleep(*every)
		f := view.Frame()
		if f.Pixels == nil {
			fmt.Printf("frame %d: still nothing (crashed=%v loaded=%v)\n", i, view.Crashed(), view.Loaded())
			continue
		}
		name := fmt.Sprintf("%s-%d.png", *out, i)
		if err := writeFrame(name, f); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("frame %d: wrote %s (%dx%d, title %q, shots=%d) %s\n", i, name, f.Width, f.Height, view.Title(), view.Shots(), stats(f))
	}
	if time.Now().Before(deadline) {
		time.Sleep(time.Until(deadline))
	}
	fmt.Printf("shots=%d loaded=%v title=%q\n", view.Shots(), view.Loaded(), view.Title())
}

// stats summarises the frame so a broken renderer is obvious in the log.
func stats(f webview.Frame) string {
	var opaque, clear, other int
	for i := 3; i < len(f.Pixels); i += 4 {
		switch f.Pixels[i] {
		case 255:
			opaque++
		case 0:
			clear++
		default:
			other++
		}
	}
	samples := []string{}
	for _, p := range [][2]int{{10, 10}, {f.Width / 2, f.Height / 2}, {f.Width - 10, f.Height - 10}} {
		r, g, b, a, _ := f.At(p[0], p[1])
		samples = append(samples, fmt.Sprintf("(%d,%d,%d,%d)", r, g, b, a))
	}
	return fmt.Sprintf("opaque=%d clear=%d other=%d samples=%v", opaque, clear, other, samples)
}

func writeFrame(name string, f webview.Frame) error {
	img := image.NewNRGBA(image.Rect(0, 0, f.Width, f.Height))
	for i := 0; i+3 < len(f.Pixels); i += 4 {
		img.Pix[i+0] = f.Pixels[i+0]
		img.Pix[i+1] = f.Pixels[i+1]
		img.Pix[i+2] = f.Pixels[i+2]
		img.Pix[i+3] = f.Pixels[i+3]
	}
	file, err := os.Create(name)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}

package main

/*
#include <X11/Xlib.h>
#include <X11/keysym.h>
#include <X11/Xft/Xft.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The rows of the settings pages.
const (
	rowSection = iota
	rowText
	rowToggle
	rowSlider
	rowChoice
	rowButton
	rowKey
)

// setRow is one line of a settings page.
type setRow struct {
	kind  int
	label string
	value string
	rect  [4]int // the place of the row on the screen

	get    func() float64
	set    func(float64)
	toggle func() bool
	click  func()
	opts   []string
	idx    int
}

// settingsApp is the system settings application. The shell draws it itself,
// so it needs no window of its own: it is a frame of the board with a list of
// pages and a list of rows.
type settingsApp struct {
	v       *view
	page    int
	rows    []setRow
	scroll  int
	drag    int
	hover   int
	msg     string
	waiting string // the action that waits for a key press

	networks []string
	sinks    []string
	outputs  []string
	busy     bool
	loaded   bool

	// The interface is drawn into its own pixmap and scaled into the frame,
	// so the whole window follows the zoom of the board.
	pm  C.Pixmap
	gc  C.GC
	xft *C.XftDraw
	pic C.Picture
	pw  int
	ph  int
}

// settingsPathMarker is the path of the shortcut of the settings
// application: it is not a file, the shell opens the application itself.
const settingsPathMarker = "@settings"

// settingsIcon builds the shortcut of the settings application.
func settingsIcon() (boardIcon, bool) {
	return boardIcon{
		name:  "Параметры",
		path:  settingsPathMarker,
		isApp: true,
		label: "Параметры",
		col:   appGlyph,
	}, true
}

// settingsPages are the pages of the settings application.
var settingsPages = []string{
	"Клавиши",
	"Мышь",
	"Оформление",
	"Тема",
	"Стили",
	"Иконки",
	"Курсор",
	"Wi-Fi",
	"Bluetooth",
	"Дата и время",
	"Звук",
	"Экран",
	"Жесты",
	"Анимации",
}

// openSettings opens the settings window of the board.
func (v *view) openSettings() {
	if v.set == nil {
		v.set = &settingsApp{v: v}
	}
	if !v.setOpen {
		v.setOpen = true
		sw, sh := v.setSize()
		v.setWin = &managedWin{
			title:   "Настройки",
			bx:      v.ox + float64(v.w)/(2*v.zoom) - float64(sw)/(2*v.zoom),
			by:      v.oy + float64(v.h)/(2*v.zoom) - float64(sh)/(2*v.zoom),
			bw:      float64(sw) / v.zoom,
			bh:      float64(sh) / v.zoom,
			virtual: true,
		}
		v.wins = append(v.wins, *v.setWin)
	}
	v.set.load()
	v.set.build()
	v.redraw = true
}

// setSize is the size of the settings window, in screen pixels.
func (v *view) setSize() (int, int) {
	w, h := 720, 480
	if v.w > 0 && v.w-80 < w {
		w = v.w - 80
	}
	if v.h > 0 && v.h-80 < h {
		h = v.h - 80
	}
	if w < 420 {
		w = 420
	}
	if h < 320 {
		h = 320
	}
	return w, h
}

// closeSettings closes the settings window.
func (v *view) closeSettings() {
	v.setOpen = false
	for i := range v.wins {
		if v.wins[i].virtual && v.wins[i].title == "Настройки" {
			v.wins = append(v.wins[:i], v.wins[i+1:]...)
			break
		}
	}
	v.setWin = nil
	v.redraw = true
}

// settingsFocused reports whether the settings window has the keyboard.
func (v *view) settingsFocused() bool {
	if !v.setOpen || v.set == nil {
		return false
	}
	// The settings window is focused when it is the topmost window.
	return len(v.wins) > 0 && v.wins[len(v.wins)-1].virtual &&
		v.wins[len(v.wins)-1].title == "Настройки"
}

// build fills the rows of the current page.
func (s *settingsApp) build() {
	s.rows = nil
	switch s.page {
	case 0:
		s.buildKeys()
	case 1:
		s.buildMouse()
	case 2:
		s.buildStyles()
	case 3:
		s.buildTheme()
	case 4:
		s.buildLooks()
	case 5:
		s.buildIcons()
	case 6:
		s.buildCursor()
	case 7:
		s.buildNetwork()
	case 8:
		s.buildBluetooth()
	case 9:
		s.buildClock()
	case 10:
		s.buildSound()
	case 11:
		s.buildScreen()
	case 12:
		s.buildGestures()
	case 13:
		s.buildAnimations()
	}
	s.drag = -1
}

func (s *settingsApp) section(title string) {
	s.rows = append(s.rows, setRow{kind: rowSection, label: title})
}

func (s *settingsApp) text(label, value string) {
	s.rows = append(s.rows, setRow{kind: rowText, label: label, value: value})
}

func (s *settingsApp) toggle(label string, get func() bool, set func(bool)) {
	s.rows = append(s.rows, setRow{
		kind:   rowToggle,
		label:  label,
		toggle: get,
		click: func() {
			set(!get())
			s.build()
		},
	})
}

func (s *settingsApp) slider(label string, min, max, step float64,
	get func() float64, set func(float64)) {
	s.rows = append(s.rows, setRow{
		kind:  rowSlider,
		label: label,
		get:   get,
		set:   set,
		value: fmt.Sprintf("%.0f", get()),
		click: func() {
			s.drag = len(s.rows) - 1
		},
	})
	_ = step
}

func (s *settingsApp) choice(label string, opts []string, get func() int, set func(int)) {
	idx := get()
	if idx < 0 || idx >= len(opts) {
		idx = 0
	}
	s.rows = append(s.rows, setRow{
		kind:  rowChoice,
		label: label,
		opts:  opts,
		idx:   idx,
		value: opts[idx],
		click: func() {
			next := (get() + 1) % len(opts)
			set(next)
			s.build()
		},
	})
}

func (s *settingsApp) button(label string, click func()) {
	s.rows = append(s.rows, setRow{kind: rowButton, label: label, click: click})
}

func (s *settingsApp) keyRow(label, action string, get func() string, set func(string)) {
	s.rows = append(s.rows, setRow{
		kind:  rowKey,
		label: label,
		value: get(),
		click: func() {
			s.waiting = action
			s.msg = "Нажмите комбинацию клавиш…"
		},
	})
}

// buildKeys lists the actions of the shell and lets the user rebind them.
func (s *settingsApp) buildKeys() {
	s.section("Комбинации клавиш")
	actions := []struct{ act, name string }{
		{actCenter, "Центр доски"},
		{actTerminal, "Терминал"},
		{actSettings, "Настройки"},
		{actWidgets, "Виджеты"},
		{actFit, "Вписать доску"},
		{actReload, "Обновить"},
		{actRestart, "Перезапуск"},
		{actZoom100, "Масштаб 100%"},
		{actZoomIn, "Приблизить"},
		{actZoomOut, "Отдалить"},
		{actSwitch, "Переключить окно"},
		{actClose, "Закрыть окно"},
		{actDelete, "В корзину"},
		{actRename, "Переименовать"},
		{actCopy, "Копировать"},
		{actCut, "Вырезать"},
		{actPaste, "Вставить"},
		{actPanLeft, "Панорама влево"},
		{actPanRight, "Панорама вправо"},
		{actPanUp, "Панорама вверх"},
		{actPanDown, "Панорама вниз"},
		{actEscape, "Снять выделение"},
	}
	for _, a := range actions {
		act := a.act
		s.keyRow(a.name, act, func() string {
			return comboText(s.board(), act)
		}, func(text string) {
			s.board().rebind(act, text)
		})
	}
}

// buildMouse holds the speeds of the pointer.
func (s *settingsApp) buildMouse() {
	v := s.board()
	s.section("Скорость указателя")
	s.slider("Панорама", 0.2, 4, 0.1,
		func() float64 { return v.cfg.Mouse.PanSpeed },
		func(x float64) { v.cfg.Mouse.PanSpeed = x; v.cfg.save() })
	s.slider("Колесо", 0.2, 4, 0.1,
		func() float64 { return v.cfg.Mouse.WheelSpeed },
		func(x float64) { v.cfg.Mouse.WheelSpeed = x; v.cfg.save() })
	s.slider("Масштаб", 0.2, 3, 0.1,
		func() float64 { return v.cfg.Mouse.ZoomSpeed },
		func(x float64) { v.cfg.Mouse.ZoomSpeed = x; v.cfg.save() })
	s.text("Порог перетаскивания",
		fmt.Sprintf("%d px", v.cfg.Mouse.Threshold))
}

// buildStyles is the style manager: the list of the built-in styles.
func (s *settingsApp) buildStyles() {
	v := s.board()
	s.section("Стили оболочки")
	for _, name := range styleNames {
		st := styleByName(name)
		row := setRow{kind: rowChoice, label: st.Title, opts: styleNames,
			idx: indexOf(styleNames, v.cfg.Style), value: st.Note}
		if v.cfg.Style == name {
			row.value = "✓ " + st.Note
		}
		style := name
		row.click = func() {
			v.applyStyleByName(style)
			s.msg = "Стиль применён: " + styleByName(style).Title
			s.build()
		}
		s.rows = append(s.rows, row)
	}
	s.text("Текущий стиль", styleByName(v.cfg.Style).Title)
}

// buildTheme holds the colours of the board.
func (s *settingsApp) buildTheme() {
	v := s.board()
	s.section("Тема")
	s.choice("Оформление", []string{"Стиль", "Тёмная", "Светлая"}, func() int {
		switch v.cfg.Display.Theme {
		case "dark":
			return 1
		case "light":
			return 2
		}
		return 0
	}, func(i int) {
		switch i {
		case 1:
			v.cfg.Display.Theme = "dark"
		case 2:
			v.cfg.Display.Theme = "light"
		default:
			v.cfg.Display.Theme = ""
		}
		v.cfg.save()
	})
	s.text("Акцент", fmt.Sprintf("#%02x%02x%02x",
		styleAccent.r, styleAccent.g, styleAccent.b))
}

// buildLooks holds the details of the look.
func (s *settingsApp) buildLooks() {
	v := s.board()
	s.section("Стили")
	s.slider("Размер шрифта", 8, 16, 1,
		func() float64 { return float64(styleFontSize) },
		func(x float64) {
			styleFontSize = int(x)
			v.reopenFont()
			v.xcolors = map[color]*C.XftColor{}
			v.cfg.Display.FontSize = int(x)
			v.cfg.save()
		})
	s.toggle("Сетка", func() bool { return v.cfg.Display.ShowGrid },
		func(b bool) { v.cfg.Display.ShowGrid = b; v.cfg.save() })
	s.toggle("Оси", func() bool { return v.cfg.Display.ShowAxes },
		func(b bool) { v.cfg.Display.ShowAxes = b; v.cfg.save() })
	s.text("Скругление", fmt.Sprintf("%d px", styleRadius))
	s.text("Объёмные рамки", map[bool]string{true: "да", false: "нет"}[styleBevel])
}

// buildIcons holds the look of the shortcuts.
func (s *settingsApp) buildIcons() {
	v := s.board()
	s.section("Значки")
	s.slider("Размер значка", 48, 128, 4,
		func() float64 { return float64(v.cfg.Icons.Size) },
		func(x float64) {
			v.cfg.Icons.Size = int(x)
			v.cfg.save()
		})
	s.toggle("Подписи", func() bool { return v.cfg.Icons.Labels },
		func(b bool) { v.cfg.Icons.Labels = b; v.cfg.save() })
	s.choice("Рисунок значка",
		[]string{"Авто", "Плоский", "Контур", "Классический", "Глянец"},
		func() int {
			switch v.cfg.Icons.Style {
			case "flat":
				return 1
			case "outline":
				return 2
			case "classic":
				return 3
			case "glass":
				return 4
			}
			return 0
		}, func(i int) {
			switch i {
			case 1:
				v.cfg.Icons.Style = "flat"
			case 2:
				v.cfg.Icons.Style = "outline"
			case 3:
				v.cfg.Icons.Style = "classic"
			case 4:
				v.cfg.Icons.Style = "glass"
			default:
				v.cfg.Icons.Style = "auto"
			}
			v.cfg.save()
		})
}

// buildCursor chooses the shape of the pointer.
func (s *settingsApp) buildCursor() {
	v := s.board()
	s.section("Курсор")
	names := make([]string, 0, len(cursorShapes))
	for _, c := range cursorShapes {
		names = append(names, c.name)
	}
	// The shape of the active style is the default.
	shape := v.cfg.Cursor.Shape
	if shape == "" {
		shape = styleCursor
	}
	s.choice("Форма", names, func() int {
		return indexOf(names, shape)
	}, func(i int) {
		if i < 0 || i >= len(names) {
			return
		}
		v.cfg.Cursor.Shape = names[i]
		v.cfg.save()
		v.applyCursor()
	})
}

// buildNetwork drives the wireless network with NetworkManager.
func (s *settingsApp) buildNetwork() {
	s.section("Wi-Fi")
	if s.busy {
		s.text("Состояние", "обновление…")
		return
	}
	if _, err := exec.LookPath("nmcli"); err != nil {
		s.text("Состояние", "NetworkManager не установлен")
		return
	}
	s.text("Радио", s.netRadio())
	for _, n := range s.networks {
		s.text("Сеть", n)
	}
	s.button("Обновить список", func() {
		s.loadNetwork()
		s.build()
	})
}

// buildBluetooth drives the bluetooth adapter.
func (s *settingsApp) buildBluetooth() {
	s.section("Bluetooth")
	if _, err := exec.LookPath("bluetoothctl"); err != nil {
		s.text("Состояние", "bluetoothctl не установлен")
		return
	}
	out, _ := runCommand("bluetoothctl", "show")
	s.text("Состояние", firstLine(out))
	s.button("Обновить", func() {
		s.build()
	})
}

// buildClock shows the time settings of the system.
func (s *settingsApp) buildClock() {
	v := s.board()
	s.section("Дата и время")
	s.text("Время", time.Now().Format("02.01.2006 15:04:05"))
	tz := os.Getenv("TZ")
	if tz == "" {
		tz = "локальное"
	}
	s.text("Часовой пояс", tz)
	s.toggle("24-часовой формат", func() bool { return v.cfg.Clock.TwentyFour },
		func(b bool) { v.cfg.Clock.TwentyFour = b; v.cfg.save() })
	if _, err := exec.LookPath("timedatectl"); err == nil {
		out, _ := runCommand("timedatectl", "show")
		s.text("Синхронизация", timedateField(out, "NTPSynchronized"))
		s.button("Включить синхронизацию", func() {
			if _, err := runCommand("timedatectl", "set-ntp", "true"); err != nil {
				s.msg = "Нужны права администратора"
			} else {
				s.msg = "Синхронизация включена"
			}
			s.build()
		})
	}
}

// buildSound drives the sound with PulseAudio.
func (s *settingsApp) buildSound() {
	v := s.board()
	s.section("Звук")
	if _, err := exec.LookPath("pactl"); err != nil {
		s.text("Состояние", "PulseAudio не установлен")
		return
	}
	s.slider("Громкость", 0, 100, 5,
		func() float64 { return float64(v.cfg.Sound.Volume) },
		func(x float64) {
			v.cfg.Sound.Volume = int(x)
			v.cfg.save()
			applyVolume(int(x), v.cfg.Sound.Muted)
		})
	s.toggle("Без звука", func() bool { return v.cfg.Sound.Muted },
		func(b bool) {
			v.cfg.Sound.Muted = b
			v.cfg.save()
			applyVolume(v.cfg.Sound.Volume, b)
		})
	for _, sink := range s.sinks {
		s.text("Устройство", sink)
	}
	s.button("Обновить", func() {
		s.loadSinks()
		s.build()
	})
}

// buildScreen drives the display with xrandr and brightnessctl.
func (s *settingsApp) buildScreen() {
	v := s.board()
	s.section("Экран")
	if _, err := exec.LookPath("brightnessctl"); err == nil {
		out, _ := runCommand("brightnessctl", "get")
		s.slider("Яркость", 10, 100, 5,
			func() float64 { return brightnessValue(out) },
			func(x float64) {
				v.cfg.Screen.Brightness = int(x)
				v.cfg.save()
				runCommand("brightnessctl", "set", strconv.Itoa(int(x))+"%")
			})
	}
	if _, err := exec.LookPath("xrandr"); err == nil {
		for _, o := range s.outputs {
			s.text("Выход", o)
		}
		s.button("Обновить", func() {
			s.loadOutputs()
			s.build()
		})
	}
}

// buildGestures holds the touch settings.
func (s *settingsApp) buildGestures() {
	v := s.board()
	s.section("Жесты")
	s.toggle("Жесты сенсорного экрана", func() bool { return v.cfg.Gestures.Enabled },
		func(b bool) { v.cfg.Gestures.Enabled = b; v.cfg.save() })
	s.toggle("Обратный масштаб", func() bool { return v.cfg.Gestures.Invert },
		func(b bool) { v.cfg.Gestures.Invert = b; v.cfg.save() })
	s.slider("Чувствительность", 0.2, 3, 0.1,
		func() float64 { return v.cfg.Gestures.Sensitivity },
		func(x float64) { v.cfg.Gestures.Sensitivity = x; v.cfg.save() })
	s.text("Двухпальный скролл", "панорама и масштаб")
}

// buildAnimations holds the animation settings.
func (s *settingsApp) buildAnimations() {
	v := s.board()
	s.section("Анимации")
	s.toggle("Анимация значков GIF", func() bool { return v.cfg.Anim.GifEnabled },
		func(b bool) {
			v.cfg.Anim.GifEnabled = b
			v.cfg.save()
			v.icons = nil
			v.reload(false)
		})
	s.toggle("Видеообои", func() bool { return v.cfg.Anim.VideoWall },
		func(b bool) {
			v.cfg.Anim.VideoWall = b
			v.cfg.save()
			v.loadWallpaper()
		})
	s.slider("Частота кадров", 4, 30, 1,
		func() float64 { return float64(v.cfg.Anim.GifFps) },
		func(x float64) {
			v.cfg.Anim.GifFps = int(x)
			v.cfg.save()
			animFps.Store(int32(x))
		})
}

// board returns the board the settings belong to.
func (s *settingsApp) board() *view { return s.v }

// load reads the state of the system services the pages show.
func (s *settingsApp) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.loadNetwork()
	s.loadSinks()
	s.loadOutputs()
}

// loadNetwork reads the list of the wireless networks.
func (s *settingsApp) loadNetwork() {
	s.busy = true
	go func() {
		var list []string
		if _, err := exec.LookPath("nmcli"); err == nil {
			out, _ := runCommand("nmcli", "-t", "-f", "SSID,SIGNAL,SECURITY",
				"device", "wifi", "list")
			for _, line := range strings.Split(out, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				list = append(list, strings.ReplaceAll(line, ":", " · "))
			}
		}
		s.networks = list
		s.busy = false
		s.board().wakeUI()
	}()
}

// netRadio reads the state of the wireless radio.
func (s *settingsApp) netRadio() string {
	out, err := runCommand("nmcli", "radio", "wifi")
	if err != nil {
		return "неизвестно"
	}
	return strings.TrimSpace(out)
}

// loadSinks reads the list of the sound devices.
func (s *settingsApp) loadSinks() {
	var list []string
	if _, err := exec.LookPath("pactl"); err == nil {
		out, _ := runCommand("pactl", "list", "short", "sinks")
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				list = append(list, parts[1])
			}
		}
	}
	s.sinks = list
}

// loadOutputs reads the list of the screens.
func (s *settingsApp) loadOutputs() {
	var list []string
	if _, err := exec.LookPath("xrandr"); err == nil {
		out, _ := runCommand("xrandr", "--query")
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, " connected") {
				list = append(list, strings.TrimSpace(strings.Split(line, " connected")[0]))
			}
		}
	}
	s.outputs = list
}

// ------------------------------------------------------------- the window -- //

// settingsSize is the natural size of the interface of the settings.
const (
	settingsW = 720
	settingsH = 480
)

// settingsLythrum prepares the pixmap the interface is drawn into.
func (v *view) settingsLythrum(s *settingsApp) {
	if s.pm != 0 && s.pw == settingsW && s.ph == settingsH {
		return
	}
	if s.pm != 0 {
		C.XFreePixmap(v.dpy, s.pm)
		s.pm = 0
	}
	if s.pic != 0 {
		C.XRenderFreePicture(v.dpy, s.pic)
		s.pic = 0
	}
	s.pm = C.XCreatePixmap(v.dpy, v.win, C.uint(settingsW), C.uint(settingsH), v.depth)
	if s.pm == 0 {
		return
	}
	s.gc = C.XCreateGC(v.dpy, s.pm, 0, nil)
	s.xft = C.XftDrawCreate(v.dpy, C.Drawable(s.pm), v.visual, v.cmap)
	if format := C.XRenderFindVisualFormat(v.dpy, v.visual); format != nil {
		s.pic = C.XRenderCreatePicture(v.dpy, s.pm, format, 0, nil)
	}
	s.pw, s.ph = settingsW, settingsH
}

// renderSettings draws the interface of the settings into its own pixmap.
func (v *view) renderSettings() {
	if !v.setOpen || v.set == nil || v.setWin == nil {
		return
	}
	s := v.set
	v.settingsLythrum(s)
	if s.pm == 0 {
		return
	}
	// The interface is drawn on its own canvas, with its own metrics.
	oldPM, oldGC, oldDraw := v.pm, v.gc, v.xftDraw
	v.pm, v.gc, v.xftDraw = s.pm, s.gc, s.xft
	pw, ph := s.pw, s.ph
	v.fill(0, 0, pw, ph, panelBg)
	if styleBevel {
		v.bevel(0, 0, pw, ph, false)
	}
	// The list of the pages.
	side := 168
	v.fill(0, 0, side, ph, menuBg)
	v.fg(menuBorder)
	C.XDrawLine(v.dpy, v.pm, v.gc, 0, 0, 0, C.int(ph))
	for i, name := range settingsPages {
		ry := 10 + i*26
		if ry+24 > ph {
			break
		}
		if i == s.page {
			v.fill(6, ry, side-12, 24, selBg)
			v.fg(selBorder)
			C.XDrawRectangle(v.dpy, v.pm, v.gc, 6, C.int(ry),
				C.uint(side-13), C.uint(23))
		}
		col := hudCol
		if i == s.page {
			col = textCol
		}
		v.text(14, ry+17, v.fitText(name, side-24), col)
	}
	// The rows of the page.
	rx := side + 18
	rw := pw - side - 36
	ry := 12 - s.scroll
	for i := range s.rows {
		r := &s.rows[i]
		if r.kind == rowSection {
			if ry+16 > 0 && ry+16 < ph-34 {
				v.text(rx, ry+16, v.fitText(r.label, rw), textCol)
			}
			ry += 30
			continue
		}
		if ry+28 < 0 || ry > ph-34 {
			// The row is out of sight: it still keeps its place, so that the
			// pointer finds it when it is scrolled into view.
			r.rect = [4]int{rx, ry, rw, 28}
			ry += 32
			continue
		}
		r.rect = [4]int{rx, ry, rw, 28}
		switch r.kind {
		case rowText:
			v.text(rx, ry+19, v.fitText(r.label, rw-140), hudCol)
			v.text(rx+rw-v.textWidth(r.value), ry+19, r.value, textCol)
		case rowToggle:
			box := pw - 46
			v.text(rx, ry+19, v.fitText(r.label, box-rx-12), hudCol)
			on := r.toggle != nil && r.toggle()
			v.fill(box, ry+6, 38, 18, selBg)
			v.fg(selBorder)
			C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(box), C.int(ry+6), C.uint(37), C.uint(17))
			knob := box + 2
			if on {
				knob = box + 18
			}
			v.fill(knob, ry+8, 16, 14, selBorder)
		case rowSlider:
			bar := pw - 150
			v.text(rx, ry+19, v.fitText(r.label, bar-rx-12), hudCol)
			v.fill(bar, ry+10, 130, 6, selBg)
			v.fg(selBorder)
			C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(bar), C.int(ry+10), C.uint(129), C.uint(5))
			if r.get != nil {
				min, max := sliderRange(r.label)
				t := (r.get() - min) / (max - min)
				if t < 0 {
					t = 0
				}
				if t > 1 {
					t = 1
				}
				v.fill(bar, ry+10, int(130*t), 6, selBorder)
				v.fill(bar+int(130*t)-4, ry+6, 8, 14, selBorder)
			}
			v.text(rx, ry+19, "", hudCol)
		case rowChoice:
			v.text(rx, ry+19, v.fitText(r.label, rw-150), hudCol)
			v.text(rx+rw-v.textWidth(r.value), ry+19, v.fitText(r.value, 150), textCol)
		case rowButton:
			v.fill(rx, ry+4, 130, 22, selBg)
			v.fg(selBorder)
			C.XDrawRectangle(v.dpy, v.pm, v.gc, C.int(rx), C.int(ry+4), C.uint(129), C.uint(21))
			v.text(rx+10, ry+20, v.fitText(r.label, 110), textCol)
		case rowKey:
			v.text(rx, ry+19, v.fitText(r.label, rw-150), hudCol)
			val := r.value
			if s.waiting == r.label {
				val = "нажмите…"
			}
			v.text(rx+rw-v.textWidth(val), ry+19, val, textCol)
		}
		ry += 32
	}
	// The scroll bar of the page.
	if total := s.contentHeight(); total > ph-46 {
		bar := pw - 8
		v.fill(bar, 12, 4, ph-46, selBg)
		frac := float64(ph-46) / float64(total)
		h := int(float64(ph-46) * frac)
		if h < 20 {
			h = 20
		}
		pos := 0
		if total > 0 {
			pos = int(float64(s.scroll) / float64(total) * float64(ph-46))
		}
		if pos+h > ph-46 {
			pos = ph - 46 - h
		}
		v.fill(bar, 12+pos, 4, h, selBorder)
	}
	if s.msg != "" {
		v.text(rx, ph-12, v.fitText(s.msg, rw), hudCol)
	}
	v.pm, v.gc, v.xftDraw = oldPM, oldGC, oldDraw
}

// contentHeight is the height the rows of the page need.
func (s *settingsApp) contentHeight() int {
	total := 0
	for _, r := range s.rows {
		if r.kind == rowSection {
			total += 30
			continue
		}
		total += 32
	}
	return total
}

// scrollBy moves the page of the settings up or down.
func (v *view) settingsScroll(dir int) {
	if v.set == nil {
		return
	}
	s := v.set
	limit := s.contentHeight() - (settingsH - 46)
	if limit < 0 {
		limit = 0
	}
	s.scroll += dir * 48
	if s.scroll > limit {
		s.scroll = limit
	}
	if s.scroll < 0 {
		s.scroll = 0
	}
	v.redraw = true
}

// sliderRange returns the range of a slider of the settings.
func sliderRange(label string) (float64, float64) {
	switch label {
	case "Громкость":
		return 0, 100
	case "Панорама", "Колесо", "Масштаб":
		return 0.2, 4
	case "Чувствительность":
		return 0.2, 3
	case "Частота кадров":
		return 4, 30
	case "Размер шрифта":
		return 8, 16
	case "Размер значка":
		return 48, 128
	case "Яркость":
		return 10, 100
	}
	return 0, 1
}

// compositeSettings puts the interface into the frame of the window, scaled
// with the zoom of the board.
func (v *view) compositeSettings() {
	if !v.setOpen || v.set == nil || v.setWin == nil {
		return
	}
	s := v.set
	if s.pic == 0 || v.dpict == 0 || s.pw == 0 {
		return
	}
	x, y, sw, sh := v.winRect(v.setWin)
	if sw <= 0 || sh <= 0 {
		return
	}
	var t C.XTransform
	t.matrix[0][0] = C.XFixed(sw * 65536 / s.pw)
	t.matrix[1][1] = C.XFixed(sh * 65536 / s.ph)
	t.matrix[2][2] = 1 << 16
	C.XRenderSetPictureTransform(v.dpy, s.pic, &t)
	C.XRenderSetPictureFilter(v.dpy, s.pic, C.CString("bilinear"), nil, 0)
	C.XRenderComposite(v.dpy, C.PictOpSrc, s.pic, 0, v.dpict,
		0, 0, 0, 0, C.int(x), C.int(y), C.uint(sw), C.uint(sh))
}

// settingsAt finds the row under the pointer.
func (v *view) settingsAt(sx, sy int) (int, bool) {
	if !v.setOpen || v.set == nil {
		return 0, false
	}
	for i := range v.set.rows {
		r := v.set.rows[i].rect
		if r[2] == 0 {
			continue
		}
		if sx >= r[0] && sx < r[0]+r[2] && sy >= r[1] && sy < r[1]+r[3] {
			return i, true
		}
	}
	return 0, false
}

// settingsPageAt returns the page under the pointer in the list of the pages.
func (v *view) settingsPageAt(sx, sy int) int {
	if !v.setOpen || v.set == nil || v.setWin == nil {
		return -1
	}
	ux, uy, ok := v.settingsPoint(sx, sy)
	if !ok {
		return -1
	}
	side := 168
	if ux > side {
		return -1
	}
	idx := (uy - 10) / 26
	if idx < 0 || idx >= len(settingsPages) {
		return -1
	}
	return int(idx)
}

// settingsPoint converts the position of the pointer into the coordinates of
// the interface of the settings.
func (v *view) settingsPoint(sx, sy int) (int, int, bool) {
	if !v.setOpen || v.set == nil || v.setWin == nil {
		return 0, 0, false
	}
	s := v.set
	if s.pw == 0 || s.ph == 0 {
		return 0, 0, false
	}
	x, y, sw, sh := v.winRect(v.setWin)
	if sw <= 0 || sh <= 0 {
		return 0, 0, false
	}
	ux := (sx - x) * s.pw / sw
	uy := (sy - y) * s.ph / sh
	if ux < 0 || uy < 0 || ux >= s.pw || uy >= s.ph {
		return 0, 0, false
	}
	return ux, uy, true
}

// settingsClick reacts to a click inside the settings window.
func (v *view) settingsClick(sx, sy int) {
	if !v.setOpen || v.set == nil {
		return
	}
	s := v.set
	ux, uy, ok := v.settingsPoint(sx, sy)
	if !ok {
		return
	}
	if p := v.settingsPageAt(sx, sy); p >= 0 {
		if p != s.page {
			s.page = p
			s.build()
			v.redraw = true
		}
		return
	}
	i, found := v.settingsAt(ux, uy)
	if !found {
		return
	}
	r := &s.rows[i]
	switch r.kind {
	case rowToggle, rowChoice, rowButton:
		if r.click != nil {
			r.click()
			v.playSound("click")
		}
	case rowKey:
		if r.click != nil {
			r.click()
		}
	case rowSlider:
		// The value follows the pointer.
		s.drag = i
		v.settingsSlider(ux)
	}
	v.redraw = true
}

// settingsSlider moves the slider that is being dragged. The position is
// given in the coordinates of the interface.
func (v *view) settingsSlider(ux int) {
	if v.set == nil || v.set.drag < 0 || v.set.drag >= len(v.set.rows) {
		return
	}
	r := &v.set.rows[v.set.drag]
	if r.get == nil || r.set == nil {
		return
	}
	bar := settingsW - 150
	t := float64(ux-bar) / 130
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	min, max := sliderRange(r.label)
	r.set(min + t*(max-min))
	r.value = fmt.Sprintf("%.0f", r.get())
	v.redraw = true
}

// settingsKey reacts to a key press inside the settings window.
func (v *view) settingsKey(ke *C.XKeyEvent) {
	if v.set == nil {
		return
	}
	s := v.set
	ks := uint32(C.XLookupKeysym(ke, 0))
	if s.waiting != "" {
		text := comboFromEvent(ks, uint32(ke.state))
		if text != "" {
			v.rebind(s.waiting, text)
			s.waiting = ""
			s.msg = "Комбинация изменена"
			s.build()
		}
		v.redraw = true
		return
	}
	switch ks {
	case 0xff1b: // Escape closes the settings
		v.closeSettings()
	case 0xff09: // Tab switches the page
		s.page = (s.page + 1) % len(settingsPages)
		s.build()
		v.redraw = true
	}
}

// rebind changes the combination of an action.
func (v *view) rebind(action, text string) {
	c, ok := parseCombo(text)
	if !ok {
		return
	}
	_ = c
	list := v.cfg.Keybinds[action]
	if list == nil {
		list = []string{}
	}
	// The combination is used by one action only.
	for other, combos := range v.cfg.Keybinds {
		if other == action {
			continue
		}
		var kept []string
		for _, t := range combos {
			if t != text {
				kept = append(kept, t)
			}
		}
		v.cfg.Keybinds[other] = kept
	}
	found := false
	for _, t := range list {
		if t == text {
			found = true
		}
	}
	if !found {
		list = append(list, text)
	}
	v.cfg.Keybinds[action] = list
	v.cfg.save()
	v.rebuildKeyMap()
	v.regrabKeys()
}

// comboText returns the combination bound to an action.
func comboText(v *view, action string) string {
	if v == nil {
		return ""
	}
	for _, t := range v.cfg.Keybinds[action] {
		return t
	}
	return "—"
}

// comboFromEvent writes the combination of a key press as text.
func comboFromEvent(ks uint32, mods uint32) string {
	var parts []string
	if mods&maskControl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&maskMod1 != 0 {
		parts = append(parts, "Alt")
	}
	if mods&maskShift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&maskMod4 != 0 {
		parts = append(parts, "Super")
	}
	name := C.XKeysymToString(C.KeySym(ks))
	key := "?"
	if name != nil {
		key = C.GoString(name)
	}
	if len(parts) == 0 {
		return key
	}
	return strings.Join(parts, "+") + "+" + key
}

// ------------------------------------------------------------- helpers ---- //

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return 0
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func timedateField(out, key string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimPrefix(line, key+"=")
		}
	}
	return "неизвестно"
}

func brightnessValue(out string) float64 {
	out = strings.TrimSpace(out)
	out = strings.TrimSuffix(out, "%")
	f, err := strconv.ParseFloat(out, 64)
	if err != nil {
		return 50
	}
	return f
}

// applyVolume sets the volume of the default sink.
func applyVolume(vol int, muted bool) {
	if _, err := exec.LookPath("pactl"); err != nil {
		return
	}
	if muted {
		runCommand("pactl", "set-sink-mute", "@DEFAULT_SINK@", "1")
		return
	}
	runCommand("pactl", "set-sink-mute", "@DEFAULT_SINK@", "0")
	runCommand("pactl", "set-sink-volume", "@DEFAULT_SINK@",
		strconv.Itoa(vol)+"%")
}

// styleFilesDir is the folder the style sheets of the widgets live in.
func styleFilesDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "data", "styles")); err == nil {
			return filepath.Join(dir, "data", "styles")
		}
	}
	return "data/styles"
}

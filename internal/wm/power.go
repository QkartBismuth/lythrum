package wm

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"strings"
	"time"

	"lythrum/internal/layout"
	"lythrum/internal/x11"
)

// powerScreen is the Super+L menu: a short list that ends the session, puts
// the machine to sleep or powers it off. The rows are drawn by the shell
// itself so the menu works even while the start screen is open.
type powerScreen struct {
	wm *WM

	win  x11.Window
	gc   x11.GC
	text *x11.FontDraw
	area layout.Rect

	rows   []powerRow
	rects  []layout.Rect // row rectangles in root coordinates, for clicks
	cursor int
	status string
	busy   bool
}

// powerRow is one entry of the menu.
type powerRow struct {
	id      string
	title   string
	hint    string
	danger  bool
	enabled bool
}

// powerIDs is the fixed order of the menu, safest first.
var powerIDs = []string{"logout", "suspend", "hibernate", "reboot", "poweroff"}

func newPowerScreen(w *WM) *powerScreen { return &powerScreen{wm: w} }

// Toggle shows the menu, or hides it when it is already open.
func (s *powerScreen) Toggle() error {
	if s.win != 0 {
		s.Close()
		return nil
	}
	return s.Open()
}

// Visible reports whether the menu is on screen.
func (s *powerScreen) Visible() bool { return s.win != 0 }

// Open shows the menu and takes the keyboard.
func (s *powerScreen) Open() error {
	if s.win != 0 {
		s.repaint()
		return nil
	}
	if len(s.wm.monitors) == 0 {
		return fmt.Errorf("wm: no monitor to show the power menu on")
	}
	s.rows = s.describe()
	s.cursor = 0
	s.status = ""
	s.busy = false
	s.area = s.wm.workArea(0)
	win, err := s.wm.d.CreateWindow(x11.CreateWindowParams{
		Parent:           s.wm.d.Root(),
		X:                s.area.X,
		Y:                s.area.Y,
		Width:            uint(s.area.W),
		Height:           uint(s.area.H),
		Class:            x11.ClassInputOutput,
		BackPixel:        0x000d1117,
		OverrideRedirect: true,
		EventMask:        x11.ExposureMask | x11.KeyPressMask | x11.StructureNotifyMask,
	})
	if err != nil {
		return err
	}
	s.win = win
	gc, err := s.wm.d.CreateGC(win)
	if err != nil {
		s.wm.d.DestroyWindow(win)
		s.win = 0
		return err
	}
	s.gc = gc
	if draw, err := s.wm.d.NewFontDraw(win, s.wm.font()); err == nil {
		s.text = draw
	}
	s.wm.d.MapWindow(win)
	s.wm.d.RaiseWindow(win)
	s.wm.d.Focus(win, x11.RevertToPointerRoot, 0)
	if err := s.wm.d.GrabInput(s.win,
		x11.PointerMotionMask|x11.ButtonPressMask, x11.KeyPressMask); err != nil {
		s.wm.log.Debug("cannot grab the input for the power menu", "error", err)
	}
	s.repaint()
	s.wm.log.Info("power menu opened")
	return nil
}

// Close hides the menu and gives the keyboard back.
func (s *powerScreen) Close() {
	if s.win == 0 {
		return
	}
	s.wm.d.UngrabInput()
	if s.text != nil {
		s.text.Close(s.wm.d)
		s.text = nil
	}
	if s.gc.Valid() {
		s.wm.d.FreeGC(s.gc)
	}
	s.wm.d.DestroyWindow(s.win)
	s.win = 0
	s.wm.d.Focus(s.wm.d.Root(), x11.RevertToPointerRoot, 0)
	s.wm.restoreFocus()
}

// describe builds the rows, marking the ones this machine cannot do.
func (s *powerScreen) describe() []powerRow {
	titles := map[string][2]string{
		"logout":    {"Завершить сеанс", "выйти из учётной записи"},
		"suspend":   {"Сон", "погасить экран, programs остаются"},
		"hibernate": {"Гибернация", "сохранить сеанс на диск и выключить"},
		"reboot":    {"Перезагрузка", "перезапустить систему"},
		"poweroff":  {"Выключение", "завершить работу системы"},
	}
	rows := make([]powerRow, 0, len(powerIDs))
	for _, id := range powerIDs {
		pair := titles[id]
		enabled, hint := powerAvailable(id, pair[1])
		rows = append(rows, powerRow{
			id: id, title: pair[0], hint: hint, enabled: enabled,
			danger: id == "poweroff" || id == "reboot",
		})
	}
	return rows
}

// layoutRects places the card and every row, in root coordinates.
func (s *powerScreen) layoutRects() (card layout.Rect, rows []layout.Rect) {
	font := s.wm.font()
	rowH := 56
	if font != nil {
		rowH = font.LineHeight() + 34
	}
	cardW := maxInt(minInt(s.area.W*3/5, 560), 320)
	cardH := rowH*len(s.rows) + 132
	card = layout.Rect{
		X: s.area.X + (s.area.W-cardW)/2,
		Y: s.area.Y + (s.area.H-cardH)/2,
		W: cardW,
		H: cardH,
	}
	rows = make([]layout.Rect, 0, len(s.rows))
	for i := range s.rows {
		rows = append(rows, layout.Rect{
			X: card.X + 18,
			Y: card.Y + 104 + i*rowH,
			W: cardW - 36,
			H: rowH - 8,
		})
	}
	return card, rows
}

func (s *powerScreen) repaint() {
	if s.win == 0 || !s.gc.Valid() {
		return
	}
	d := s.wm.d
	colors := gridPalette(s.wm.cfg)
	d.SetForegroundOf(s.gc, 0x000d1117)
	d.FillRectangle(s.win, s.gc, 0, 0, s.area.W, s.area.H)

	card, rows := s.layoutRects()
	s.rects = rows
	// The card is drawn relative to the window, the window sits on the work
	// area, and the work area may start below a panel.
	ox, oy := card.X-s.area.X, card.Y-s.area.Y
	d.SetForegroundOf(s.gc, 0x00181d26)
	d.FillRectangle(s.win, s.gc, ox, oy, card.W, card.H)
	d.SetForegroundOf(s.gc, 0x002a3140)
	d.FillRectangle(s.win, s.gc, ox, oy, card.W, 2)
	d.FillRectangle(s.win, s.gc, ox, oy+card.H-2, card.W, 2)

	if s.text == nil {
		return
	}
	font := s.wm.font()
	s.text.SetColor(d, colors.Text)
	s.text.DrawText(d, ox+24, oy+34, "Завершение работы")
	s.text.SetColor(d, colors.TextDim)
	s.text.DrawText(d, ox+24, oy+58, "стрелки — выбрать, Enter — выполнить, Esc — закрыть")

	for i, row := range s.rows {
		r := rows[i]
		rx, ry := r.X-s.area.X, r.Y-s.area.Y
		if i == s.cursor {
			d.SetForegroundOf(s.gc, 0x00222b38)
			d.FillRectangle(s.win, s.gc, rx, ry, r.W, r.H)
			d.SetForegroundOf(s.gc, s.pixel(colors.Accent))
			d.FillRectangle(s.win, s.gc, rx, ry, 3, r.H)
		}
		label, hint := row.title, row.hint
		if !row.enabled {
			label += "  —  недоступно"
		}
		if i == s.cursor {
			s.text.SetColor(d, colors.Text)
		} else if row.enabled {
			s.text.SetColor(d, colors.TextDim)
		} else {
			s.text.SetColor(d, color.RGBA{0x60, 0x60, 0x70, 0xff})
		}
		base := ry + r.H/2 + font.Height()/3
		s.text.DrawText(d, rx+18, base, font.Truncate(label, r.W-40))
		if hint != "" {
			s.text.SetColor(d, color.RGBA{0x6f, 0x7a, 0x8c, 0xff})
			s.text.DrawText(d, rx+r.W-18-font.Width(font.Truncate(hint, 260)),
				base, font.Truncate(hint, 260))
		}
	}
	if s.status != "" {
		s.text.SetColor(d, colors.TextDim)
		s.text.DrawText(d, ox+24, oy+card.H-26, font.Truncate(s.status, card.W-48))
	}
	d.Flush()
}

// HandleKey feeds one key press to the menu and reports whether it was used.
func (s *powerScreen) HandleKey(e x11.KeyPress, binding x11.Binding) bool {
	if s.win == 0 {
		return false
	}
	switch x11.KeySymName(binding.Sym) {
	case "Escape":
		s.Close()
		return true
	case "Return", "KP_Enter":
		s.activate()
		return true
	case "Down":
		s.moveCursor(1)
		return true
	case "Up":
		s.moveCursor(-1)
		return true
	case "Left":
		s.moveCursor(-1)
		return true
	case "Right":
		s.moveCursor(1)
		return true
	}
	// The digits pick a row directly, which is quicker than walking the list.
	if text := x11.KeySymText(binding.Sym); len(text) == 1 {
		if n := int(text[0] - '1'); n >= 0 && n < len(s.rows) {
			s.cursor = n
			s.activate()
			return true
		}
	}
	return false
}

func (s *powerScreen) moveCursor(delta int) {
	if len(s.rows) == 0 {
		return
	}
	next := s.cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(s.rows) {
		next = len(s.rows) - 1
	}
	if next == s.cursor {
		return
	}
	s.cursor = next
	s.repaint()
}

// rowAt reports which row a root coordinate lands on, or -1.
func (s *powerScreen) rowAt(rootX, rootY int) int {
	for i, r := range s.rects {
		if rootX >= r.X && rootX < r.X+r.W && rootY >= r.Y && rootY < r.Y+r.H {
			return i
		}
	}
	return -1
}

// HandleButton runs a row the pointer landed on.
func (s *powerScreen) HandleButton(rootX, rootY int) {
	if s.win == 0 {
		return
	}
	i := s.rowAt(rootX, rootY)
	if i < 0 {
		return
	}
	s.cursor = i
	s.repaint()
	s.activate()
}

// HandleExpose repaints after the window was uncovered.
func (s *powerScreen) HandleExpose(win x11.Window) {
	if win == s.win {
		s.repaint()
		s.wm.d.Sync()
	}
}

// activate runs the row under the cursor.
func (s *powerScreen) activate() {
	if s.cursor < 0 || s.cursor >= len(s.rows) || s.busy {
		return
	}
	row := s.rows[s.cursor]
	if !row.enabled {
		s.status = row.title + ": это устройство такого не умеет"
		s.repaint()
		return
	}
	s.busy = true
	s.status = row.title + "…"
	s.repaint()
	// The grabs have to go before the machine starts going down, otherwise a
	// frozen input grab would keep the session alive.
	s.Close()
	id := row.id
	go func() {
		if err := s.run(id); err != nil {
			s.wm.log.Error("power action failed", "action", id, "error", err)
			s.reopenWithError(id, err)
			return
		}
		s.wm.log.Info("power action done", "action", id)
	}()
}

// reopenWithError puts the menu back with the reason it failed, so a denied
// action is visible instead of silently doing nothing.
func (s *powerScreen) reopenWithError(id string, err error) {
	rows := s.describe()
	for _, row := range rows {
		if row.id == id {
			row.enabled = false
		}
	}
	s.rows = rows
	s.busy = false
	s.status = "не получилось: " + err.Error()
	if err := s.Open(); err != nil {
		s.wm.log.Error("cannot reopen the power menu", "error", err)
	}
}

// run performs one power action, trying the commands of the system in order.
func (s *powerScreen) run(id string) error {
	if id == "logout" {
		return s.wm.logout()
	}
	var lastErr error
	for _, cmd := range powerCommands(id) {
		if len(cmd) == 0 {
			continue
		}
		if _, err := exec.LookPath(cmd[0]); err != nil {
			lastErr = err
			continue
		}
		ctx, cancel := commandContext(30 * time.Second)
		err := runCommand(ctx, cmd[0], cmd[1:]...)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("нет команды для действия %q", id)
	}
	return lastErr
}

// powerStatePath is the kernel file that lists the sleep modes; tests point it
// at a fixture.
var powerStatePath = "/sys/power/state"

// powerAvailable reports whether the machine supports an action and returns a
// hint that explains the answer.
func powerAvailable(id, hint string) (bool, string) {
	switch id {
	case "suspend", "hibernate":
		want := "mem"
		if id == "hibernate" {
			want = "disk"
		}
		if !powerModeAvailable(want) {
			if id == "hibernate" && powerModeAvailable("s2disk") {
				return true, hint
			}
			return false, "ядро не умеет этот режим"
		}
	case "logout", "reboot", "poweroff":
		if !hasAnyCommand("systemctl", "loginctl") {
			return false, "нет systemctl или loginctl"
		}
	}
	if id == "logout" && os.Getenv("XDG_SESSION_ID") == "" && !hasAnyCommand("loginctl") {
		return false, "нет loginctl"
	}
	return true, hint
}

// powerModeAvailable looks for a sleep mode in the kernel list.
func powerModeAvailable(want string) bool {
	data, err := os.ReadFile(powerStatePath)
	if err != nil {
		return false
	}
	for _, mode := range strings.Fields(string(data)) {
		if mode == want {
			return true
		}
	}
	return false
}

func hasAnyCommand(names ...string) bool {
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

// powerCommands lists the ways to ask the system for a power action, most
// direct first.
func powerCommands(id string) [][]string {
	switch id {
	case "suspend":
		return [][]string{{"systemctl", "suspend"}, {"loginctl", "suspend"}}
	case "hibernate":
		return [][]string{{"systemctl", "hibernate"}, {"loginctl", "hibernate"}}
	case "reboot":
		return [][]string{{"systemctl", "reboot"}, {"loginctl", "reboot"}}
	case "poweroff":
		return [][]string{{"systemctl", "poweroff"}, {"loginctl", "poweroff"}}
	case "logout":
		if id := os.Getenv("XDG_SESSION_ID"); id != "" {
			return [][]string{{"loginctl", "terminate-session", id}}
		}
	}
	return nil
}

// logout ends the session. It asks logind first and quits the shell when the
// session is not managed, which is what a bare X session needs.
func (w *WM) logout() error {
	var lastErr error
	for _, cmd := range powerCommands("logout") {
		if len(cmd) == 0 {
			continue
		}
		if _, err := exec.LookPath(cmd[0]); err != nil {
			lastErr = err
			continue
		}
		if err := runCommandNoWait(cmd[0], cmd[1:]...); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if user := os.Getenv("USER"); user != "" {
		if _, err := exec.LookPath("loginctl"); err == nil {
			if err := runCommandNoWait("loginctl", "terminate-user", user); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
	}
	if lastErr != nil {
		w.log.Warn("cannot terminate the session through logind", "error", lastErr)
	}
	// Nothing else works, so the shell ends the session by leaving.
	w.Quit()
	return nil
}

// commandContext bounds how long a power command may take before the menu
// reports it as failed.
func commandContext(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// runCommand runs a command and waits for it.
func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if msg := strings.TrimSpace(string(output)); msg != "" {
		return fmt.Errorf("%s: %s", name, firstLine(msg))
	}
	return fmt.Errorf("%s: %w", name, err)
}

// runCommandNoWait starts a command and does not wait for the system to go
// down, which is what terminating a session needs.
func runCommandNoWait(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	// The child must not keep the shell's pipes open, and it may outlive us.
	go func() { _ = cmd.Wait() }()
	return nil
}

// pixel converts a palette colour into the pixel value Xlib wants.
func (s *powerScreen) pixel(c color.RGBA) uint32 {
	return uint32(c.R) | uint32(c.G)<<8 | uint32(c.B)<<16
}

func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}

// powerAvailableID reports whether the machine can do the action at all.
func powerAvailableID(id string) bool {
	enabled, _ := powerAvailable(id, "")
	return enabled
}

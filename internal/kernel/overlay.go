// Оверлей-UI (Спринт 6): стек меню поверх кадра.
//
// Контракт: меню — модели ui.Menu в ЭКРАННЫХ пикселях (зум не участвует);
// владелец стека — main loop (как и всего холста). Значения настроек
// (громкость, порог, акцент) меняются ТОЛЬКО через события шины — панель
// обновляет подписи при возврате события, оставаясь чистым отображением.
package kernel

import (
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"

	"lythrum/internal/core"
	"lythrum/internal/module"
	"lythrum/internal/render"
	"lythrum/internal/ui"
)

// ClipboardHistoryCap — глубина истории буфера обмена.
const ClipboardHistoryCap = 10

// accentPreset — пресет акцента темы (Control Center, пункт «Акцент»).
type accentPreset struct {
	Name  string
	Edits map[string]string
}

// accentPresets — циклический список; первый совпадает с дефолтной темой.
var accentPresets = []accentPreset{
	{"Лиловый", map[string]string{"OverlayAccent": "#7C6CF6", "ButtonActive": "#8F7FFF"}},
	{"Морской", map[string]string{"OverlayAccent": "#00B4D8", "ButtonActive": "#48CAE4"}},
	{"Изумрудный", map[string]string{"OverlayAccent": "#00D4AA", "ButtonActive": "#38E8C6"}},
	{"Янтарный", map[string]string{"OverlayAccent": "#FFB703", "ButtonActive": "#FFD166"}},
}

// OpenOverlay открывает оверлей по центру экрана (экранные пиксели 1:1).
func (k *Kernel) OpenOverlay(m *ui.Menu) {
	k.dialog = nil // диалог и меню — взаимоисключимы
	vpW, vpH := k.State.ViewportW, k.State.ViewportH
	if k.overlay == nil {
		k.overlay = &ui.MenuStack{}
	}
	if m.W < ui.MenuMinWidth {
		m.W = ui.MenuMinWidth
	}
	m.Open((vpW-m.W)/2, (vpH-m.Height())/2, vpW, vpH)
	k.overlay.Push(m)
}

// HandleMenuNav — стрелки при открытом оверлее: Up/Down — перемещение
// по пунктам, Left/Right — правка значения. Без оверлея — no-op
// (стрелки никуда не уходят клиентам: хоткеи серверные).
func (k *Kernel) HandleMenuNav(dir core.Dir) {
	if k.overlay == nil {
		return
	}
	top := k.overlay.Top()
	if top == nil {
		return
	}
	switch dir {
	case core.DirUp:
		top.Move(-1)
	case core.DirDown:
		top.Move(1)
	case core.DirLeft, core.DirRight:
		k.adjustOverlay(top, dir)
	}
}

// ActivateOverlayItem — Enter: исполнить действие выбранного пункта.
func (k *Kernel) ActivateOverlayItem() {
	if k.overlay == nil {
		return
	}
	top := k.overlay.Top()
	if top == nil {
		return
	}
	id, ok := top.ActivateID()
	if !ok {
		return
	}
	k.dispatchOverlay(top, id)
}

// dispatchOverlay — цель действия по тегу и ID пункта.
func (k *Kernel) dispatchOverlay(top *ui.Menu, id string) {
	pub := k.Pub
	switch top.Tag {
	case ui.TagPower:
		switch id {
		case "lock":
			if pub != nil {
				pub.Publish(module.Event{Type: module.EvLockSession})
			}
		case "logout":
			if pub != nil {
				pub.Publish(module.Event{Type: module.EvQuit})
			}
		case "suspend":
			if k.Launch != nil {
				go k.Launch("systemctl suspend")
			}
		case "reboot":
			if k.Launch != nil {
				go k.Launch("systemctl reboot")
			}
		case "poweroff":
			if k.Launch != nil {
				go k.Launch("systemctl poweroff -i")
			}
		}

	case ui.TagControlCenter:
		switch id {
		case "mute":
			if pub != nil {
				pub.Publish(module.Event{Type: module.EvVolumeMute})
			}
		case "accent":
			k.cycleAccent(1)
		case "quit":
			if pub != nil {
				pub.Publish(module.Event{Type: module.EvQuit})
			}
		}

	case ui.TagClipboard:
		// clip:N — выбранная запись становится активным буфером композитора.
		if i, err := strconv.Atoi(strings.TrimPrefix(id, "clip:")); err == nil && i < len(k.clipHistory) {
			k.Clipboard = k.clipHistory[i]
		}
	}
}

// adjustOverlay — Left/Right на пункте панели: шаг значения через шину.
func (k *Kernel) adjustOverlay(top *ui.Menu, dir core.Dir) {
	if top.Tag != ui.TagControlCenter || top.Hover() < 0 {
		return
	}
	id := top.Items[top.Hover()].ID
	sign := 1.0
	if dir == core.DirLeft {
		sign = -1
	}
	switch id {
	case "volume":
		if k.Pub != nil {
			k.Pub.Publish(module.Event{Type: module.EvVolume, Num: 5 * sign})
		}
	case "snap":
		v := k.State.SnapThresholdPx + 2*sign
		if v < 0 {
			v = 0
		}
		if k.Pub != nil {
			k.Pub.Publish(module.Event{Type: module.EvSnapThreshold, Num: v})
		}
	case "accent":
		if sign > 0 {
			k.cycleAccent(1)
		} else {
			k.cycleAccent(-1)
		}
	}
}

// cycleAccent — переключает пресет акцента (направление ±1) событием шины.
func (k *Kernel) cycleAccent(d int) {
	k.accentIdx = ((k.accentIdx+d)%len(accentPresets) + len(accentPresets)) % len(accentPresets)
	if k.Pub == nil {
		return
	}
	edits, _ := json.Marshal(accentPresets[k.accentIdx].Edits)
	k.Pub.Publish(module.Event{Type: module.EvPaletteChanged, Str: string(edits)})
}

// ── Модели оверлеев ──────────────────────────────────────────────────────

// ControlCenterMenu — панель настроек по текущим значениям.
func (k *Kernel) ControlCenterMenu() *ui.Menu {
	vol, muted := k.controlValues()
	muteWord := "включён"
	if muted {
		muteWord = "ВЫКЛЮЧЕН"
	}
	m := ui.NewMenu([]ui.MenuItem{
		{ID: "volume", Icon: "volume_up", Shortcut: "← →",
			Label: fmt.Sprintf("Громкость: %d%%", vol)},
		{ID: "mute", Icon: "volume_off",
			Label: "Звук: " + muteWord},
		{ID: "snap", Icon: "grid_on", Shortcut: "← →",
			Label: fmt.Sprintf("Порог снапа: %.0f px", k.State.SnapThresholdPx)},
		{ID: "accent", Icon: "palette", Shortcut: "→",
			Label: "Акцент: " + accentPresets[k.accentIdx].Name},
		{Separator: true},
		{ID: "quit", Icon: "power_settings_new", Label: "Завершить сессию"},
	})
	m.Tag = ui.TagControlCenter
	m.W = 320
	return m
}

// controlValues — громкость/mute схемы (audio+config). nil-хук → дефолт.
func (k *Kernel) controlValues() (vol int, muted bool) {
	if k.ControlValues != nil {
		return k.ControlValues()
	}
	return 75, false
}

// TaskManagerMenu — статусы модулей под надзором (read-only список).
func (k *Kernel) TaskManagerMenu() *ui.Menu {
	items := []ui.MenuItem{}
	if k.Statuses != nil {
		for _, st := range k.Statuses() {
			state := "работает"
			if !st.Running {
				state = "остановлен"
			}
			label := fmt.Sprintf("%s: %s, перезапусков %d", st.Name, state, st.Restarts)
			if st.LastErr != "" {
				label += " — " + truncateRunes(st.LastErr, 40)
			}
			items = append(items, ui.MenuItem{Label: label, Disabled: true})
		}
	}
	if len(items) == 0 {
		items = append(items, ui.MenuItem{Label: "Нет данных о модулях", Disabled: true})
	}
	m := ui.NewMenu(items)
	m.Tag = ui.TagTaskManager
	m.W = 460
	return m
}

// ClipboardMenu — история буфера обмена (новые сверху).
func (k *Kernel) ClipboardMenu() *ui.Menu {
	items := make([]ui.MenuItem, 0, len(k.clipHistory))
	for i, text := range k.clipHistory {
		items = append(items, ui.MenuItem{
			ID:    fmt.Sprintf("clip:%d", i),
			Icon:  "content_copy",
			Label: truncateRunes(text, 48),
		})
	}
	if len(items) == 0 {
		items = append(items, ui.MenuItem{Label: "История пуста", Disabled: true})
	}
	m := ui.NewMenu(items)
	m.Tag = ui.TagClipboard
	m.W = 380
	return m
}

// refreshControlCenter обновляет подписи ОТКРЫТОЙ панели по текущим
// значениям (in-place: hover и позиция сохраняются). Вызывается после
// применения соответствующих событий.
func (k *Kernel) refreshControlCenter() {
	if k.overlay == nil {
		return
	}
	top := k.overlay.Top()
	if top == nil || top.Tag != ui.TagControlCenter {
		return
	}
	vol, muted := k.controlValues()
	muteWord := "включён"
	if muted {
		muteWord = "ВЫКЛЮЧЕН"
	}
	for i := range top.Items {
		switch top.Items[i].ID {
		case "volume":
			top.Items[i].Label = fmt.Sprintf("Громкость: %d%%", vol)
		case "mute":
			top.Items[i].Label = "Звук: " + muteWord
		case "snap":
			top.Items[i].Label = fmt.Sprintf("Порог снапа: %.0f px", k.State.SnapThresholdPx)
		case "accent":
			top.Items[i].Label = "Акцент: " + accentPresets[k.accentIdx].Name
		}
	}
}

// Frame — растеризация кадра: холст + оверлеи + диалог. Вызывается из
// main loop (скриншоты, вывод) и тестов; состояние — только своё.
func (k *Kernel) Frame() *image.RGBA {
	img := render.Paint(k.frameInput(), k.Face)
	if k.overlay != nil {
		for _, m := range k.overlay.All() {
			render.PaintOverlay(img, m, k.Face)
		}
	}
	if k.dialog != nil {
		title := "Поиск окон"
		if k.dialog.kind == dialogRun {
			title = "Запуск команды"
		}
		render.PaintDialog(img, title, k.dialog.text.Value(), k.Face)
	}
	return img
}

// frameInput — плоский снимок холста для render.Paint.
func (k *Kernel) frameInput() render.FrameInput {
	s := k.State
	in := render.FrameInput{
		Cam:       s.Cam,
		ViewportW: s.ViewportW,
		ViewportH: s.ViewportH,
		Guides:    s.Guides(),
	}
	for _, w := range s.Windows() {
		in.Windows = append(in.Windows, render.WindowFrame{
			Title:     w.Title,
			Rect:      w.Rect,
			Focused:   w.ID == s.Focus,
			Minimized: w.Minimized,
		})
	}
	for _, wg := range s.Widgets() {
		in.Widgets = append(in.Widgets, render.WindowFrame{
			Title: "виджет · " + shortName(wg.URL),
			Rect:  wg.Rect,
		})
	}
	for _, c := range s.Cards() {
		in.Cards = append(in.Cards, render.CardFrame{
			Title:  c.Title,
			Rect:   c.Rect,
			Poster: c.Poster,
		})
	}
	return in
}

// shortName — последний сегмент пути/URL.
func shortName(url string) string {
	if i := strings.LastIndexByte(url, '/'); i >= 0 {
		return url[i+1:]
	}
	return url
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

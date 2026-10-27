// Package canvas — состояние холста: окна, камера, магнитные направляющие,
// сон по фрустуму.
//
// Владелец состояния — main loop (единственная горутина), поэтому здесь нет
// мьютексов: конкурентный доступ исключён архитектурой. Подсистемы снапшоты
// состояния получают через шину событий или сериализацию (session).
package canvas

import (
	"sort"

	"lythrum/internal/core"
	"lythrum/internal/preview"
	"lythrum/internal/render"
	"lythrum/internal/session"
	"lythrum/internal/widgets"
)

// WID — идентификатор окна (Wayland surface id).
type WID uint32

// Window — окно приложения на бесконечном холсте.
type Window struct {
	ID        WID
	Title     string
	Rect      core.Rect // глобальные координаты холста, масштаб 1:1
	Minimized bool
	Sleeping  bool // вне фрустума: frame-events остановлены
}

// State — всё состояние холста. Не потокобезопасно by design (см. выше).
type State struct {
	Cam             core.Camera
	ViewportW       float64
	ViewportH       float64
	SnapThresholdPx float64 // магнитный порог в экранных пикселях
	Focus           WID     // активное окно (0 = нет)

	windows map[WID]*Window
	nextID  WID
	guides  []core.Guide

	cards   map[string]*preview.Card   // карточки Live Preview по id
	widgets map[string]*widgets.Widget // HTML5-виджеты по id
}

// New создаёт холст с вьюпортом vpW×vpH и дефолтной камерой.
func New(vpW, vpH float64) *State {
	return &State{
		Cam:             core.NewCamera(),
		ViewportW:       vpW,
		ViewportH:       vpH,
		SnapThresholdPx: core.DefaultSnapThresholdPx,
		windows:         make(map[WID]*Window),
		cards:           make(map[string]*preview.Card),
		widgets:         make(map[string]*widgets.Widget),
	}
}

// ── HTML5-виджеты (Спринт 4) ────────────────────────────────────────────

// AddWidget регистрирует виджет на холсте.
func (s *State) AddWidget(w *widgets.Widget) { s.widgets[w.ID] = w }

// Widget возвращает виджет по id.
func (s *State) Widget(id string) (*widgets.Widget, bool) {
	w, ok := s.widgets[id]
	return w, ok
}

// Widgets — виджеты в стабильном порядке (по id).
func (s *State) Widgets() []*widgets.Widget {
	ids := make([]string, 0, len(s.widgets))
	for id := range s.widgets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*widgets.Widget, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.widgets[id])
	}
	return out
}

// RemoveWidget убирает виджет с холста.
func (s *State) RemoveWidget(id string) bool {
	if _, ok := s.widgets[id]; !ok {
		return false
	}
	delete(s.widgets, id)
	return true
}

// MoveWidget перемещает виджет.
func (s *State) MoveWidget(id string, pos core.Vec2) bool {
	w, ok := s.widgets[id]
	if !ok {
		return false
	}
	w.Rect.X = pos.X
	w.Rect.Y = pos.Y
	return true
}

// ── Карточки Live Preview (Спринт 3) ────────────────────────────────────

// AddCard регистрирует карточку на холсте.
func (s *State) AddCard(c *preview.Card) { s.cards[c.ID] = c }

// Card возвращает карточку по id.
func (s *State) Card(id string) (*preview.Card, bool) {
	c, ok := s.cards[id]
	return c, ok
}

// Cards — карточки в стабильном порядке (по id).
func (s *State) Cards() []*preview.Card {
	ids := make([]string, 0, len(s.cards))
	for id := range s.cards {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*preview.Card, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.cards[id])
	}
	return out
}

// RemoveCard убирает карточку с холста.
func (s *State) RemoveCard(id string) bool {
	if _, ok := s.cards[id]; !ok {
		return false
	}
	delete(s.cards, id)
	return true
}

// MoveCard перемещает карточку (drag по холству).
func (s *State) MoveCard(id string, pos core.Vec2) bool {
	c, ok := s.cards[id]
	if !ok {
		return false
	}
	c.Rect.X = pos.X
	c.Rect.Y = pos.Y
	return true
}

// AddWindow регистрирует окно и возвращает его.
func (s *State) AddWindow(title string, r core.Rect) *Window {
	s.nextID++
	w := &Window{ID: s.nextID, Title: title, Rect: r}
	s.windows[w.ID] = w
	if s.Focus == 0 {
		s.Focus = w.ID
	}
	return w
}

// RemoveWindow закрывает окно. Если фокус был на нём — переезжает на другое
// живое (не свёрнутое) окно; холст без окон остаётся без фокуса (0).
func (s *State) RemoveWindow(id WID) bool {
	if _, ok := s.windows[id]; !ok {
		return false
	}
	delete(s.windows, id)
	if s.Focus != id {
		return true
	}
	// Фокус погиб: ближайшее живое окно по id (стабильный порядок).
	s.Focus = 0
	for _, w := range s.Windows() {
		if !w.Minimized {
			s.Focus = w.ID
			break
		}
	}
	return true
}

// Window возвращает окно по id.
func (s *State) Window(id WID) (*Window, bool) {
	w, ok := s.windows[id]
	return w, ok
}

// Windows — срез окон в порядке возрастания id (стабильный порядок рендера).
func (s *State) Windows() []*Window {
	ids := make([]WID, 0, len(s.windows))
	for id := range s.windows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]*Window, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.windows[id])
	}
	return out
}

// MoveWindow перемещает окно с примагничиванием к соседям (встывание кромок).
// Направляющие снапа сохраняются для рендера до EndDrag.
func (s *State) MoveWindow(id WID, target core.Rect) (core.SnapResult, bool) {
	w, ok := s.windows[id]
	if !ok {
		return core.SnapResult{}, false
	}
	neighbors := make([]core.Rect, 0, len(s.windows))
	for _, other := range s.windows {
		if other.ID != id && !other.Minimized {
			neighbors = append(neighbors, other.Rect)
		}
	}
	res := core.SnapRect(target, neighbors, s.SnapThresholdPx, s.Cam.Zoom)
	w.Rect = core.Rect{X: res.Pos.X, Y: res.Pos.Y, W: target.W, H: target.H}
	s.guides = res.Guides
	return res, true
}

// EndDrag завершает перетаскивание: магнитные линии гаснут.
func (s *State) EndDrag() { s.guides = nil }

// Guides — активные магнитные направляющие (слой холста).
func (s *State) Guides() []core.Guide { return s.guides }

// ApplyCulling помечает окна вне фрустума спящими (остановка frame-events).
// Свернутые окна считаются спящими всегда. Возвращает число видимых.
func (s *State) ApplyCulling() int {
	visible := 0
	for _, w := range s.windows {
		w.Sleeping = w.Minimized ||
			render.ShouldSleep(s.Cam, s.ViewportW, s.ViewportH, w.Rect)
		if !w.Sleeping {
			visible++
		}
	}
	return visible
}

// FocusWindow переключает фокус.
func (s *State) FocusWindow(id WID) {
	if _, ok := s.windows[id]; ok {
		s.Focus = id
	}
}

// CycleFocus переключает фокус по кольцу живых окон (порядок id) —
// действие Meta/Alt+Tab. step = +1 вперёд, -1 назад. Свернутые окна
// пропускаются. Возвращает новое активное окно.
func (s *State) CycleFocus(step int) (*Window, bool) {
	live := make([]*Window, 0, len(s.windows))
	for _, w := range s.Windows() {
		if !w.Minimized {
			live = append(live, w)
		}
	}
	if len(live) == 0 {
		s.Focus = 0
		return nil, false
	}

	idx := 0
	if s.Focus != 0 {
		for i, w := range live {
			if w.ID == s.Focus {
				idx = i
				break
			}
		}
		// Циклический сдвиг с корректной работой для отрицательного step.
		idx = ((idx+step)%len(live) + len(live)) % len(live)
	}
	s.Focus = live[idx].ID
	return live[idx], true
}

// CenterCamPos возвращает позицию камеры, центрирующую окно w в вьюпорте
// (камера = левый верхний угол, поэтому вычитаем половину вьюпорта).
func (s *State) CenterCamPos(w *Window) core.Vec2 {
	return core.Vec2{
		X: w.Rect.X + w.Rect.W/2 - s.ViewportW/2,
		Y: w.Rect.Y + w.Rect.H/2 - s.ViewportH/2,
	}
}

// ── Персистентность (Спринт 1: JSON-сессия для отладки и рестартов) ─────

// ToSnapshot сериализует состояние в снапшот сессии.
func (s *State) ToSnapshot() session.Snapshot {
	snap := session.Snapshot{
		Camera: session.CameraSnapshot{X: s.Cam.X, Y: s.Cam.Y, Zoom: s.Cam.Zoom},
	}
	for _, w := range s.Windows() {
		snap.Windows = append(snap.Windows, session.WindowSnapshot{
			ID:        uint32(w.ID),
			Title:     w.Title,
			X:         w.Rect.X,
			Y:         w.Rect.Y,
			W:         w.Rect.W,
			H:         w.Rect.H,
			Minimized: w.Minimized,
		})
	}
	for _, c := range s.Cards() {
		snap.Cards = append(snap.Cards, session.CardSnapshot{
			ID:    c.ID,
			Path:  c.Path,
			Kind:  string(c.Kind),
			Title: c.Title,
			X:     c.Rect.X,
			Y:     c.Rect.Y,
			W:     c.Rect.W,
			H:     c.Rect.H,
		})
	}
	for _, wg := range s.Widgets() {
		perms := make([]string, 0, len(wg.Perms.List()))
		for _, p := range wg.Perms.List() {
			perms = append(perms, string(p))
		}
		snap.Widgets = append(snap.Widgets, session.WidgetSnapshot{
			ID:    wg.ID,
			URL:   wg.URL,
			X:     wg.Rect.X,
			Y:     wg.Rect.Y,
			W:     wg.Rect.W,
			H:     wg.Rect.H,
			Perms: perms,
		})
	}
	return snap
}

// Restore применяет снапшот сессии к состоянию (после перезапуска).
// Карточки с исчезнувшими файлами восстанавливаются без постера —
// потеря файла не должна рушить сессию.
func Restore(s *State, snap session.Snapshot) {
	cam := core.Camera{X: snap.Camera.X, Y: snap.Camera.Y, Zoom: snap.Camera.Zoom}
	if cam.Zoom <= 0 {
		cam = core.NewCamera()
	}
	s.Cam = cam
	for _, ws := range snap.Windows {
		w := s.AddWindow(ws.Title, core.Rect{X: ws.X, Y: ws.Y, W: ws.W, H: ws.H})
		w.Minimized = ws.Minimized
	}
	for _, cs := range snap.Cards {
		rect := core.Rect{X: cs.X, Y: cs.Y, W: cs.W, H: cs.H}
		card, err := preview.OpenCard(cs.Path, rect)
		if err != nil {
			// Файл исчез/не декодируется: карточка-заглушка с прежней геометрией.
			card = &preview.Card{
				ID:    cs.ID,
				Path:  cs.Path,
				Kind:  preview.MediaKind(cs.Kind),
				Title: cs.Title,
				Rect:  rect,
			}
		} else {
			card.ID = cs.ID // сохранить ID сессии
		}
		s.AddCard(card)
	}
	for _, ws := range snap.Widgets {
		// Права виджета из сессии не «поднимаются»: интерпретируем только
		// известные имена, неизвестные отбрасываем (защита от чужого JSON).
		perms := widgets.NewPermissions()
		for _, p := range ws.Perms {
			if pp := widgets.Permission(p); isKnownPerm(pp) {
				perms[pp] = true
			}
		}
		s.AddWidget(&widgets.Widget{
			ID:    ws.ID,
			URL:   ws.URL,
			Rect:  core.Rect{X: ws.X, Y: ws.Y, W: ws.W, H: ws.H},
			Perms: perms,
			Token: core.NewToken(), // новый токен: старый мог утечь на диск
		})
	}
}

// isKnownPerm — право входит в белый набор разрешений.
func isKnownPerm(p widgets.Permission) bool {
	for _, k := range widgets.AllPermissions() {
		if k == p {
			return true
		}
	}
	return false
}

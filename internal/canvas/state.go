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
	"lythrum/internal/render"
	"lythrum/internal/session"
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
}

// New создаёт холст с вьюпортом vpW×vpH и дефолтной камерой.
func New(vpW, vpH float64) *State {
	return &State{
		Cam:             core.NewCamera(),
		ViewportW:       vpW,
		ViewportH:       vpH,
		SnapThresholdPx: core.DefaultSnapThresholdPx,
		windows:         make(map[WID]*Window),
	}
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
	return snap
}

// Restore применяет снапшот сессии к состоянию (после перезапуска).
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
}

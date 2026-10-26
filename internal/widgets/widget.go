// Package widgets — HTML5-виджеты на холсте: изолированные страницы,
// рендерящиеся в отдельных процессах (headless Chromium по CDP).
//
// Архитектура отказоустойчивости (требование Спринта 4):
//   - каждый виджет живёт в СВОЁМ процессе-браузере; крах кодека/JS
//     не задевает ни ядро, ни соседние виджеты;
//   - Manager запускает рендереры в горутинах с recover() и backoff'ом;
//   - состояние виджетов (геометрия) принадлежит main loop — как окна
//     и карточки; Manager — исполнитель, синхронизируемый по дельте.
//
// Безопасность (требование IPC): каждый виджет получает УНИКАЛЬНЫЙ токен,
// все вызовы в композитор идут через Handler с БЕЛЫМ СПИСКОМ команд;
// каждая команда требует конкретное право из PermissionSet.
package widgets

import (
	"lythrum/internal/core"
)

// Permission — право виджета. Минимальный набор Спринта 4.
type Permission string

const (
	PermCanvasWrite Permission = "canvas_write" // создавать/двигать карточки на холсте
	PermNotify      Permission = "notify"       // показывать системные уведомления
	PermFSRead      Permission = "fs_read"      // читать файлы по путям (санитайзится)
)

// AllPermissions — полный набор (для Control Center в Спринте 5).
func AllPermissions() []Permission {
	return []Permission{PermCanvasWrite, PermNotify, PermFSRead}
}

// PermissionSet — набор прав виджета.
type PermissionSet map[Permission]bool

// Allow — есть ли право.
func (ps PermissionSet) Allow(p Permission) bool { return ps[p] }

// List — права в стабильном порядке (для сериализации).
func (ps PermissionSet) List() []Permission {
	out := make([]Permission, 0, len(ps))
	for _, p := range AllPermissions() {
		if ps[p] {
			out = append(out, p)
		}
	}
	return out
}

// NewPermissions строит набор из среза; nil → пустой набор (без прав).
func NewPermissions(perms ...Permission) PermissionSet {
	ps := make(PermissionSet, len(perms))
	for _, p := range perms {
		ps[p] = true
	}
	return ps
}

// Widget — HTML5-виджет на холсте.
type Widget struct {
	ID    string        `json:"id"`
	URL   string        `json:"url"` // file://, http(s)://, data:
	Rect  core.Rect     `json:"rect"`
	Perms PermissionSet `json:"-"`
	Token string        `json:"-"` // IPC-токен; НЕ сериализуется в сессию

	// Status — заполнен Manager'ом (для Control Center).
	Status Status `json:"-"`
}

// Status — состояние жизненного цикла рендерера.
type Status string

const (
	StatusStopped  Status = "stopped"  // рендерера нет
	StatusStarting Status = "starting" // браузер запускается
	StatusRunning  Status = "running"  // жив, кадры идут
	StatusSleeping Status = "sleeping" // вне фрустума — рендер приостановлен
	StatusCrashed  Status = "crashed"  // ждёт backoff-перезапуска
)

// New строит виджет с уникальным ID и токеном.
func New(url string, rect core.Rect, perms ...Permission) *Widget {
	return &Widget{
		ID:    core.NewID(),
		URL:   url,
		Rect:  rect,
		Perms: NewPermissions(perms...),
		Token: core.NewToken(),
	}
}

// ── Маршрутизация ввода (тест 4.1) ──────────────────────────────────────

// HitAt находит виджет под экранным курсором и возвращает его и точку
// в ЛОКАЛЬНЫХ координатах виджета.
//
// Тест 4.1: преобразование делается в два шага с учётом зума:
//
//	screen → canvas:  canvas = screen/zoom + cam   (core.Camera.ToCanvas)
//	canvas → local:   local  = canvas − rect.Pos
//
// При zoom 0.5 клик по виджету попадает в цель: обратный зум компенсирует
// масштаб. Верхний по z-порядку — последний в срезе (как в HTML).
func HitAt(ws []*Widget, cam core.Camera, vpW, vpH float64, screen core.Vec2) (*Widget, core.Vec2, bool) {
	canvas := cam.ToCanvas(screen)
	for i := len(ws) - 1; i >= 0; i-- {
		w := ws[i]
		if w.Rect.Contains(canvas) {
			local := core.Vec2{X: canvas.X - w.Rect.X, Y: canvas.Y - w.Rect.Y}
			return w, local, true
		}
	}
	return nil, core.Vec2{}, false
}

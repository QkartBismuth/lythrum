// Package dnd — drag-and-drop композитора: перетаскивание данных между
// источником (окно/виджет) и целью (холст/карточка/виджет/окно).
//
// Ключевые свойства (тест 4.5): тип данных (MIME) сохраняется по всему
// пути, дроп идемпотентен — повторный Drop без активного Drag не создаёт
// дублей и не паникует.
package dnd

import (
	"lythrum/internal/core"
)

// Payload — переносимые данные с явным типом (MIME). Тип обязателен:
// цель решает по нему, как интерпретировать данные.
type Payload struct {
	MIME string // "text/uri-list", "text/plain", "image/png", "lythrum/card"…
	Data []byte
}

// TargetKind — класс цели дропа.
type TargetKind string

const (
	TargetCanvas TargetKind = "canvas" // свободный холст → карточка Live Preview
	TargetCard   TargetKind = "card"   // на карточку (например, замена постера)
	TargetWidget TargetKind = "widget" // в HTML5-виджет (через его рендерер)
	TargetWindow TargetKind = "window" // в окно Wayland-клиента (data device)
)

// Target — цель дропа с координатой в глобальных координатах холста.
type Target struct {
	Kind TargetKind
	ID   string // id карточки/виджета/окна; "" для canvas
	At   core.Vec2
}

// Result — результат дропа, доставляемый цели.
type Result struct {
	Target Target
	Data   Payload
}

// Manager — state-машина перетаскивания. НЕ потокобезопасна by design:
// владелец — main loop (ввод приходит только из него).
type Manager struct {
	drag    *Drag // активное перетаскивание; nil — ничего не тащат
	dropSeq int   // счётчик завершённых дропов (для тестов/метрик)
}

// Drag — активное перетаскивание.
type Drag struct {
	Source string // id источника (виджет/окно), "" — системный drag
	Data   Payload
	Pos    core.Vec2 // текущая позиция курсора (глобальные координаты холста)
}

// Start начинает перетаскивание. Активный drag перезапускается (последний
// ввод выигрывает — источник отвечает за своё поведение).
func (m *Manager) Start(source string, p Payload, at core.Vec2) {
	m.drag = &Drag{Source: source, Data: p, Pos: at}
}

// Move двигает курсор перетаскивания. Без активного drag — no-op.
func (m *Manager) Move(at core.Vec2) {
	if m.drag != nil {
		m.drag.Pos = at
	}
}

// Active — идёт ли сейчас drag.
func (m *Manager) Active() bool { return m.drag != nil }

// Drag_ — текущее перетаскивание (nil, если нет).
func (m *Manager) Drag_() *Drag { return m.drag }

// Drop завершает перетаскивание на цель. Возвращает результат и признак
// успеха. ИДЕМПОТЕНТНОСТЬ: без активного drag — (zero, false), состояние
// не меняется; данные доставляются ровно один раз.
func (m *Manager) Drop(t Target) (Result, bool) {
	if m.drag == nil {
		return Result{}, false
	}
	res := Result{Target: t, Data: m.drag.Data}
	m.drag = nil // drag закрыт ДО обработки цели: повторный вызов — no-op
	m.dropSeq++
	return res, true
}

// Cancel отменяет drag (Esc). Идемпотентен.
func (m *Manager) Cancel() { m.drag = nil }

// Drops — число завершённых дропов (метрика для Control Center).
func (m *Manager) Drops() int { return m.dropSeq }

// ── Классификация цели ──────────────────────────────────────────────────

// Targeter — чем main loop отвечает на вопрос «что под курсором».
// Реализация в canvas/kernel: карточки, виджеты, окна, пустой холст.
// Отдельный интерфейс — чтобы dnd не зависел от canvas (и цикла импорта).
type Targeter interface {
	// TargetAt классифицирует точку холста. topmost-объект выигрывает.
	TargetAt(canvasPos core.Vec2) Target
}

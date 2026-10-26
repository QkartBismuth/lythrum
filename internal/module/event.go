// Package module — каркас изолированных подсистем Lythrum: событийная шина,
// интерфейс модуля и Supervisor с рестартом упавших модулей.
//
// Краеугольный принцип отказоустойчивости: подсистемы (Input, Widgets,
// Wallpaper, DND, Control Center) живут в собственных горутинах и общаются
// с ядром ТОЛЬКО через эту шину. Падение модуля не может передаться ядру.
package module

import (
	"log"

	"lythrum/internal/core"
)

// EventType — тип события шины. Строка (а не int) удобнее в логах и IPC.
type EventType string

// События ядра. Поля Event интерпретируются по Type (комментарий у константы).
const (
	EvQuit           EventType = "quit"                 // сигнал завершения сессии
	EvCameraPan      EventType = "camera.pan"           // Vec: дельта в экранных пикселях
	EvCameraZoomAt   EventType = "camera.zoom_at"       // Vec: точка экрана; Num: множитель
	EvResetCamera    EventType = "camera.reset"         // одиночный Meta
	EvLayoutChanged  EventType = "input.layout"         // Str: "us"/"ru"
	EvToggleSearch   EventType = "ui.search"            // Meta+S
	EvPowerOverlay   EventType = "ui.power"             // Power
	EvOpenControlCenter EventType = "ui.control_center" // Meta+I
	EvMinimizeAll    EventType = "ui.minimize_all"      // Meta+D
	EvRunDialog      EventType = "ui.run"               // Meta+R
	EvTaskManager    EventType = "ui.task_manager"      // Ctrl+Shift+Esc
	EvCloseWindow    EventType = "ui.close_window"      // Alt+F4
	EvClipboardHistory EventType = "ui.clipboard"       // Meta+V
	EvScreenshot     EventType = "ui.screenshot"        // PrintScreen
	EvSnip           EventType = "ui.snip"              // Meta+Shift+S
	EvCycleFocus     EventType = "ui.cycle_focus"       // Meta/Alt+Tab; Num: ±1
	EvTile           EventType = "ui.tile"              // Meta+Стрелки; Dir
	EvLockSession    EventType = "session.lock"         // Meta+L → SDDM
	EvLaunch         EventType = "app.launch"           // Str: командная строка
	EvVolume         EventType = "audio.volume"         // Num: ±шаг
	EvVolumeMute     EventType = "audio.mute"
	EvBrightness     EventType = "screen.brightness"    // Num: ±шаг
	EvSessionSave    EventType = "session.save"
)

// Event — сообщение шины. Простые поля вместо interface{}: дешёвая передача,
// без аллокаций на горячем пути ввода.
type Event struct {
	Type EventType
	Str  string
	Num  float64
	Vec  core.Vec2
	Dir  core.Dir
}

// Publisher — потокобезопасный издатель в шину main loop.
type Publisher interface {
	Publish(e Event)
}

// ChanPublisher — неблокирующий издатель: при переполнении буфера событие
// сбрасывается с логом. Ввод никогда не должен блокироваться ядром —
// тем самым подсистема ввода изолирована от скорости рендера.
type ChanPublisher struct {
	C chan Event
}

func (p *ChanPublisher) Publish(e Event) {
	select {
	case p.C <- e:
	default:
		log.Printf("lythrum: шина переполнена, событие %q сброшено", e.Type)
	}
}

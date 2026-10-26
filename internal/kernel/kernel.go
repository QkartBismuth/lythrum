// Package kernel — сборка ядра Lythrum: логика main loop, вынесенная из
// cmd/lythrum для интеграционных тестов без дисплея (Спринт 2.8).
//
// Kernel НЕ владеет потоками: Apply/Tick вызываются из main loop (или теста),
// состояние холста по-прежнему принадлежит вызывающей стороне.
package kernel

import (
	"os"
	"os/exec"

	"lythrum/internal/canvas"
	"lythrum/internal/core"
	"lythrum/internal/module"
	"lythrum/internal/session"
)

// CameraTweenDuration — длительность плавного центрирования камеры (Meta/Alt+Tab).
const CameraTweenDuration = 0.35 // секунды

// Kernel — ядро композитора: применяет события шины к холсту и тикает
// периодическую работу (анимация камеры, фрустум-куллинг).
type Kernel struct {
	State *canvas.State
	sess  *session.Manager

	// tween — активная анимация центрирования камеры (Meta/Alt+Tab);
	// отменяется любым ручным движением камеры.
	tween *core.Tween

	// Launch — запуск системных команд (SDDM, проводник). Вынесен как
	// hook: интеграционные тесты подменяют его и не трогают реальную ОС.
	Launch func(cmdline string)
}

// New собирает ядро поверх холста и менеджера сессии.
func New(state *canvas.State, sess *session.Manager) *Kernel {
	return &Kernel{
		State:  state,
		sess:   sess,
		Launch: defaultLaunch,
	}
}

// Apply применяет событие шины к состоянию холста.
// Возвращает true, если сессию пора завершить (EvQuit).
func (k *Kernel) Apply(ev module.Event) bool {
	s := k.State
	switch ev.Type {
	case module.EvQuit:
		return true

	// ── Камера ──
	case module.EvCameraPan:
		k.tween = nil // ручное движение отменяет анимацию
		s.Cam.Pan(ev.Vec)
	case module.EvCameraZoomAt:
		k.tween = nil
		s.Cam.ZoomAt(ev.Vec, ev.Num)
	case module.EvResetCamera:
		// Одиночный Meta: мгновенный сброс камеры в дефолт.
		k.tween = nil
		s.Cam = core.NewCamera()

	// ── Управление окнами ──
	case module.EvMinimizeAll:
		for _, w := range s.Windows() {
			w.Minimized = true
		}
	case module.EvTile:
		// Meta+Стрелки: тайлинг в доли видимого экрана.
		if w, ok := focused(s); ok {
			vp := s.Cam.Frustum(s.ViewportW, s.ViewportH)
			w.Rect = core.TileDirection(ev.Dir, vp)
		}
	case module.EvCycleFocus:
		// Meta/Alt+Tab: кольцо фокуса + плавное центрирование камеры.
		if w, ok := s.CycleFocus(int(ev.Num)); ok {
			k.tween = core.NewTween(
				core.Vec2{X: s.Cam.X, Y: s.Cam.Y},
				s.CenterCamPos(w),
				CameraTweenDuration,
			)
		}

	// ── Сессия ──
	case module.EvSessionSave:
		k.SaveSession()

	// ── Системные действия (main loop — единственный, кто зовёт os/exec) ──
	case module.EvLaunch:
		if k.Launch != nil {
			go k.Launch(ev.Str)
		}
	case module.EvLockSession:
		// SDDM подхватывает блокировку сессии через loginctl.
		if k.Launch != nil {
			go k.Launch("loginctl lock-session")
		}

	// ── Заготовки подсистем Спринтов 3–5: оверлей-UI и звук (S3),
	// DND/виджеты (S4), Control Center (S5) ──
	case module.EvLayoutChanged, module.EvToggleSearch, module.EvPowerOverlay,
		module.EvOpenControlCenter, module.EvRunDialog, module.EvTaskManager,
		module.EvCloseWindow, module.EvClipboardHistory, module.EvScreenshot,
		module.EvSnip, module.EvVolume, module.EvVolumeMute, module.EvBrightness:
	}
	return false
}

// Tick — периодическая работа main loop: анимация камеры и куллинг окон.
func (k *Kernel) Tick(dt float64) {
	if k.tween != nil {
		pos := k.tween.Tick(dt)
		k.State.Cam.X = pos.X
		k.State.Cam.Y = pos.Y
		if k.tween.Done() {
			k.tween = nil
		}
	}
	k.State.ApplyCulling()
}

// SaveSession сериализует холст в файл сессии. Сбой сохранения не роняет
// композитор — только лог.
func (k *Kernel) SaveSession() {
	if k.sess == nil {
		return
	}
	if err := k.sess.Save(k.State.ToSnapshot()); err != nil {
		os.Stderr.WriteString("lythrum: save session: " + err.Error() + "\n")
	}
}

// focused — сфокусированное живое окно; fallback — первое живое.
func focused(s *canvas.State) (*canvas.Window, bool) {
	if w, ok := s.Window(s.Focus); ok && !w.Minimized {
		return w, true
	}
	for _, w := range s.Windows() {
		if !w.Minimized {
			return w, true
		}
	}
	return nil, false
}

// defaultLaunch запускает внешнюю команду. Ошибка команды НЕ роняет
// композитор — изоляция через отдельную горутину и лог.
func defaultLaunch(cmdline string) {
	cmd := exec.Command("sh", "-c", cmdline)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Stderr.WriteString("lythrum: launch " + cmdline + ": " + err.Error() + ": " + string(out) + "\n")
	}
}

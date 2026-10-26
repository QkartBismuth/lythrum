// Command lythrum — ядро Wayland-композитора Lythrum (фундамент Спринтов 1–2).
//
// Точка сборки подсистем: событийная шина, Supervisor, InputProcessor, холст,
// JSON-сессия. Main loop — единственный владелец состояния холста; все
// подсистемы мутуют его только через шину событий.
//
// Wayland-сервер (DisplayBackend) интегрируется в Спринте 2 за build-тегом
// wayland; ядро специально собирается и работает headless — это позволяет
// гонять полный цикл обработки событий в CI без дисплея.
package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"lythrum/internal/canvas"
	"lythrum/internal/core"
	"lythrum/internal/input"
	"lythrum/internal/module"
	"lythrum/internal/palette"
	"lythrum/internal/session"
)

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString("lythrum: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	// Палитра валидируется на старте: битый HEX = отказ запуска, а не
	// невидимый текст посреди сессии.
	if err := palette.Validate(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── Шина событий ──
	// Буфер 256: всплеск ввода (автоповтор + жесты) не должен теряться,
	// а Publish при переполнении всё равно неблокирующий.
	bus := &module.ChanPublisher{C: make(chan module.Event, 256)}

	// ── Холст ──
	// Виртуальный вьюпорт 1920×1080 до подключения реального дисплея;
	// DisplayBackend заменит эти константы на геометрию output'ов.
	state := canvas.New(1920, 1080)

	// ── Ввод: маршрутизатор хоткеев ──
	ip := input.NewProcessor(bus, input.NewLayoutMachine())
	_ = ip // input-горутина подключается в Спринте 2 вместе с libxkbcommon

	// ── Сессия: восстановление и автосохранение ──
	sess := session.NewManager(sessionPath())
	if snap, err := sess.Load(); err == nil {
		canvas.Restore(state, snap)
	} else if !os.IsNotExist(err) {
		return err // битый JSON — это диагностируемая проблема, не тихий пропуск
	}

	// ── Supervisor модулей ──
	sup := module.NewSupervisor(ctx)
	defer sup.Stop()

	// Автосейв: раз в 10 с и при завершении — событием в шину, чтобы запись
	// шла из main loop (владельца состояния), без гонок за снапшотом.
	sup.Go(module.NewFunc("session.autosave", func(ctx context.Context) error {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				bus.Publish(module.Event{Type: module.EvSessionSave})
				return nil
			case <-t.C:
				bus.Publish(module.Event{Type: module.EvSessionSave})
			}
		}
	}))

	// Демо-окно для ручной проверки холста (до подключения Wayland-клиентов).
	if len(state.Windows()) == 0 {
		state.AddWindow("Terminal", core.Rect{X: 120, Y: 120, W: 640, H: 400})
		state.AddWindow("Music", core.Rect{X: 800, Y: 160, W: 360, H: 240})
	}

	// ── MAIN LOOP ──
	// Единственный владелец canvas.State. Тик ~60 Гц: фрустум-куллинг
	// (сон окон) и обслуживание модулей.
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			saveSession(sess, state)
			return nil

		case ev := <-bus.C:
			if quit := handleEvent(state, ev, sess); quit {
				saveSession(sess, state)
				return nil
			}

		case <-ticker.C:
			state.ApplyCulling()
		}
	}
}

// handleEvent применяет событие шины к состоянию холста.
// Возвращает true, если сессию пора завершить.
func handleEvent(state *canvas.State, ev module.Event, sess *session.Manager) bool {
	switch ev.Type {
	case module.EvQuit:
		return true

	// ── Камера ──
	case module.EvCameraPan:
		state.Cam.Pan(ev.Vec)
	case module.EvCameraZoomAt:
		state.Cam.ZoomAt(ev.Vec, ev.Num)
	case module.EvResetCamera:
		state.Cam = core.NewCamera() // одиночный Meta

	// ── Управление окнами ──
	case module.EvMinimizeAll:
		for _, w := range state.Windows() {
			w.Minimized = true
		}
	case module.EvTile:
		// Спринт 1: тайлим первое сфокусированное окно в долю фрустума.
		if w, ok := focusedWindow(state); ok {
			vp := state.Cam.Frustum(state.ViewportW, state.ViewportH)
			w.Rect = core.TileDirection(ev.Dir, vp)
		}
	case module.EvCycleFocus:
		// Спринт 2: порядок фокуса + плавная анимация центра камеры.

	// ── Сессия ──
	case module.EvSessionSave:
		saveSession(sess, state)

	// ── Системные действия (main loop — единственный, кто зовёт os/exec) ──
	case module.EvLaunch:
		go launch(ev.Str)
	case module.EvLockSession:
		// SDDM подхватывает блокировку сессии через loginctl.
		go launch("loginctl lock-session")

	// ── Заготовки подсистем Спринтов 2–5 ──
	case module.EvLayoutChanged, module.EvToggleSearch, module.EvPowerOverlay,
		module.EvOpenControlCenter, module.EvRunDialog, module.EvTaskManager,
		module.EvCloseWindow, module.EvClipboardHistory, module.EvScreenshot,
		module.EvSnip, module.EvVolume, module.EvVolumeMute, module.EvBrightness:
		// Обработчики появятся в соответствующих спринтах:
		// оверлей-UI и звук (S3), DND/виджеты (S4), Control Center (S5).
	}
	return false
}

// focusedWindow — окно под фокусом (Спринт 2: полноценное кольцо фокуса).
func focusedWindow(state *canvas.State) (*canvas.Window, bool) {
	if w, ok := state.Window(state.Focus); ok && !w.Minimized {
		return w, true
	}
	for _, w := range state.Windows() {
		if !w.Minimized {
			return w, true
		}
	}
	return nil, false
}

// launch запускает внешнюю команду. Ошибка команды НЕ роняет композитор —
// изоляция через отдельную горутину и лог.
func launch(cmdline string) {
	cmd := exec.Command("sh", "-c", cmdline)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Stderr.WriteString("lythrum: launch " + cmdline + ": " + err.Error() + ": " + string(out) + "\n")
	}
}

func saveSession(sess *session.Manager, state *canvas.State) {
	if err := sess.Save(state.ToSnapshot()); err != nil {
		// Сбой сохранения не должен рушить сессию — только лог.
		os.Stderr.WriteString("lythrum: save session: " + err.Error() + "\n")
	}
}

// sessionPath — расположение файла сессии (переопределяется для тестов).
func sessionPath() string {
	if p := os.Getenv("LYTHRUM_SESSION"); p != "" {
		return p
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "lythrum", "session.json")
}

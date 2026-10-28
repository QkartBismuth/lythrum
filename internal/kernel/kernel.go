// Package kernel — сборка ядра Lythrum: логика main loop, вынесенная из
// cmd/lythrum для интеграционных тестов без дисплея (Спринт 2.8).
//
// Kernel НЕ владеет потоками: Apply/Tick вызываются из main loop (или теста),
// состояние холста по-прежнему принадлежит вызывающей стороне.
package kernel

import (
	"encoding/json"
	"image"
	"os"
	"os/exec"
	"strings"

	"lythrum/internal/canvas"
	"lythrum/internal/core"
	"lythrum/internal/module"
	"lythrum/internal/palette"
	"lythrum/internal/preview"
	"lythrum/internal/render"
	"lythrum/internal/session"
	"lythrum/internal/ui"
	"lythrum/internal/widgets"
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

	// Hook — побочный эффект после Apply (звуки, индикаторы). Вызывается
	// из main loop; nil — отключён. Держится вне Apply, чтобы ядро
	// не зависело от аудио-подсистемы.
	Hook func(ev module.Event)

	// OnVolume — громкость схемы 0–100 (Fn-клавиши, Control Center).
	// Заполняет main.go (AudioManager + ConfigManager); nil — тихо игнор.
	OnVolume func(v int)

	// OnMute — переключение mute (Fn-клавиша, Control Center).
	OnMute func()

	// Pub — своя шина: ядро публикует события-эффекты (оверлеи, акценты).
	// nil — эффекты тихо пропускаются (юнит-тесты).
	Pub module.Publisher

	// Face — шрифт кадра (nil → рамки без текста, композитор работает
	// даже без assets).
	Face *render.Face

	// Screenshot — приёмник кадров скриншота (main пишет PNG); nil — игнор.
	Screenshot func(img image.Image)

	// ControlValues — громкость/mute схемы для Control Center (audio+config).
	ControlValues func() (volume int, muted bool)

	// Statuses — статусы модулей для диспетчера задач (Supervisor).
	Statuses func() []module.Status

	// ── Оверлей-UI (Спринт 6): владелец — main loop ──
	overlay     *ui.MenuStack // стек открытых меню
	clipHistory []string      // история буфера (новые сверху)
	accentIdx   int           // текущий пресет акцента
	dialog      *dialog       // открытый текстовый диалог (поиск/запуск)
	// Clipboard — активный буфер композитора (set_clip и выбор из истории;
	// wayland-буфер появится вместе с реальными клиентами).
	Clipboard string
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
	case module.EvCloseWindow:
		// Alt+F4: закрыть фокусированное окно; фокус переезжает сам
		// (RemoveWindow). Свёрнутое окно под фокусом закрывается тоже —
		// это честное «закрой текущее».
		s.RemoveWindow(s.Focus)

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

	case module.EvDroppedFile:
		// DND: файл из окна приложения сброшен на холст → карточка
		// Live Preview (тест 4.6). Путь падает вместе с точкой холста.
		card, err := preview.OpenCard(ev.Str, core.Rect{
			X: ev.Vec.X, Y: ev.Vec.Y, W: 160, H: 120,
		})
		if err != nil {
			// Неизвестный/недоступный файл: карточку не создаём — лог.
			os.Stderr.WriteString("lythrum: dnd drop " + ev.Str + ": " + err.Error() + "\n")
			return false
		}
		s.AddCard(card)

	case module.EvCardMove:
		s.MoveCard(ev.Str, ev.Vec)
	case module.EvCardRemove:
		s.RemoveCard(ev.Str)

	// ── HTML5-виджеты (Спринт 4) ──
	case module.EvWidgetCreate:
		// Виджет создаётся по URL с дефолтным размером; права — по умолчанию
		// минимальные (notify): поднять их можно только из Control Center.
		s.AddWidget(widgets.New(ev.Str, core.Rect{
			X: ev.Vec.X, Y: ev.Vec.Y, W: 480, H: 360,
		}, widgets.PermNotify))
	case module.EvWidgetRemove:
		s.RemoveWidget(ev.Str)
	case module.EvWidgetMove:
		s.MoveWidget(ev.Str, ev.Vec)

	// ── Горячие настройки (Спринт 5): применяет только main loop ──
	case module.EvPaletteChanged:
		// Правки приходят JSON'ом {"Роль": "#HEX"}; отказобезопасно:
		// битый пакет не меняет тему вовсе. Ошибка — лог, не паника.
		var edits map[string]string
		if err := json.Unmarshal([]byte(ev.Str), &edits); err != nil {
			os.Stderr.WriteString("lythrum: palette json: " + err.Error() + "\n")
			return false
		}
		if unknown, err := palette.Apply(edits); err != nil {
			os.Stderr.WriteString("lythrum: palette: " + err.Error() + "\n")
		} else if len(unknown) > 0 {
			os.Stderr.WriteString("lythrum: palette: неизвестные роли: " +
				strings.Join(unknown, ", ") + "\n")
		}
		k.refreshControlCenter()
	case module.EvSnapThreshold:
		s.SnapThresholdPx = ev.Num
		k.refreshControlCenter()

	// ── Громкость: применение делегировано хукам (audio + config) ──
	case module.EvVolume:
		// ev.Num — шаг (±5): Fn-клавиши и панель CC шлют дельту.
		if k.OnVolume != nil {
			k.OnVolume(int(ev.Num))
		}
		k.refreshControlCenter()
	case module.EvVolumeMute:
		if k.OnMute != nil {
			k.OnMute()
		}
		k.refreshControlCenter()

	// ── Отложено (Спринт 7+): backlight-протокол для яркости ──
	case module.EvBrightness:
		os.Stderr.WriteString("lythrum: " + string(ev.Type) +
			": отложено — нужен backlight (см. ARCHITECTURE.md)\n")

	// ── Текстовые диалоги (Спринт 7): поиск окон, запуск команд ──
	case module.EvToggleSearch:
		if k.dialog != nil && k.dialog.kind == dialogSearch {
			k.dialog = nil // Meta+S повторно — закрыть
		} else {
			k.OpenSearch()
		}
	case module.EvRunDialog:
		if k.dialog != nil && k.dialog.kind == dialogRun {
			k.dialog = nil
		} else {
			k.OpenRun()
		}
	case module.EvChar:
		k.HandleChar(ev.Str)
	case module.EvBackspace:
		k.HandleBackspace()

	// ── Оверлей-UI (Спринт 6): стек меню поверх кадра ──
	case module.EvPowerOverlay:
		k.OpenOverlay(ui.PowerOverlay())
	case module.EvOpenControlCenter:
		k.OpenOverlay(k.ControlCenterMenu())
	case module.EvTaskManager:
		k.OpenOverlay(k.TaskManagerMenu())
	case module.EvClipboardHistory:
		k.OpenOverlay(k.ClipboardMenu())
	case module.EvMenuNav:
		// Диалог: Up/Down — совпадения поиска; иначе — навигация меню.
		if k.dialog != nil {
			switch ev.Dir {
			case core.DirUp:
				k.CycleMatch(-1)
			case core.DirDown:
				k.CycleMatch(1)
			}
			return false
		}
		k.HandleMenuNav(ev.Dir)
	case module.EvMenuActivate:
		if k.dialog != nil {
			k.ActivateDialog()
			return false
		}
		k.ActivateOverlayItem()
	case module.EvMenuClose:
		// Esc: сначала диалог, затем оверлей-стек.
		if k.dialog != nil {
			k.CloseDialog()
			return false
		}
		if k.overlay != nil {
			k.overlay.Pop()
			if k.overlay.Top() == nil {
				k.overlay = nil // стек опустел — оверлей-режим выключен
			}
		}
	case module.EvClipboardPush:
		// Виджет положил текст в буфер (set_clip): копим историю.
		if ev.Str != "" {
			k.clipHistory = append([]string{ev.Str}, k.clipHistory...)
			if len(k.clipHistory) > ClipboardHistoryCap {
				k.clipHistory = k.clipHistory[:ClipboardHistoryCap]
			}
		}
	case module.EvScreenshot, module.EvSnip:
		// Скриншот и «снип» (пока — весь кадр; выбор области придёт вместе
		// с text-input). Рендер здесь, запись — в хуке main.go.
		if k.Screenshot != nil {
			k.Screenshot(k.Frame())
		}
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

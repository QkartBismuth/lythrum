package input

import (
	"lythrum/internal/core"
	"lythrum/internal/module"
)

// Publisher — шина событий ядра (алиас для читаемости).
type Publisher = module.Publisher

// Ctx — контекст действия хоткея. В Спринтах 3–5 расширится
// (фокус, активное окно, менеджеры медиа).
type Ctx struct {
	Pub Publisher
}

// InputProcessor — маршрутизатор клавиатуры.
//
// Потокобезопасность: HandleKeyEvent вызывается из единственной input-горутины
// (горутины: one seat = one processor). Любые мутации состояния холста — только
// через публикацию событий в main loop, никаких прямых ссылок на canvas.
//
// Особые клавиши:
//   - Caps Lock — перехват: переключает раскладку, до клиента не доходит;
//   - одиночный Meta — press запоминает состояние, любая другая клавиша
//     отменяет «одиночность», release без отмены сбрасывает камеру.
type InputProcessor struct {
	pub    Publisher
	layout *LayoutMachine
	hot    map[Combo]func(*Ctx)

	metaDown bool
	otherKey bool
}

// NewProcessor строит процессор с таблицей хоткеев Windows 11 / KDE Plasma.
func NewProcessor(pub Publisher, layout *LayoutMachine) *InputProcessor {
	if layout == nil {
		layout = NewLayoutMachine()
	}
	ip := &InputProcessor{pub: pub, layout: layout}

	// Таблица хоткеев — данные, а не switch-лес: сочетание → действие.
	ip.hot = map[Combo]func(*Ctx){
		// ── Системные (Windows 11 / KDE Plasma) ──
		{Mods: ModMeta, Key: KeyS}: act(module.EvToggleSearch),
		{Mods: ModMeta, Key: KeyL}: act(module.EvLockSession), // SDDM: main loop зовёт os/exec
		{Mods: ModMeta, Key: KeyI}: act(module.EvOpenControlCenter),
		{Mods: ModMeta, Key: KeyD}: act(module.EvMinimizeAll),
		{Mods: ModMeta, Key: KeyR}: act(module.EvRunDialog),
		{Mods: ModCtrl | ModShift, Key: KeyEscape}: act(module.EvTaskManager),
		{Mods: ModAlt, Key: KeyF4}:                 act(module.EvCloseWindow),
		{Mods: ModMeta, Key: KeyV}:                 act(module.EvClipboardHistory),
		{Mods: ModMeta, Key: KeyTab}:               actNum(module.EvCycleFocus, 1),
		{Mods: ModAlt, Key: KeyTab}:                actNum(module.EvCycleFocus, 1),
		{Mods: 0, Key: KeyPrint}:                   act(module.EvScreenshot),
		{Mods: ModMeta | ModShift, Key: KeyS}:      act(module.EvSnip),

		// ── Тайлинг Meta+Стрелки (доли видимого экрана) ──
		{Mods: ModMeta, Key: KeyLeft}:  actDir(module.EvTile, core.DirLeft),
		{Mods: ModMeta, Key: KeyRight}: actDir(module.EvTile, core.DirRight),
		{Mods: ModMeta, Key: KeyUp}:    actDir(module.EvTile, core.DirUp),
		{Mods: ModMeta, Key: KeyDown}:  actDir(module.EvTile, core.DirDown),

		// ── Мультимедийные Fn-клавиши ──
		{Mods: 0, Key: KeyXF86AudioRaiseVolume}:  actNum(module.EvVolume, 5),
		{Mods: 0, Key: KeyXF86AudioLowerVolume}:  actNum(module.EvVolume, -5),
		{Mods: 0, Key: KeyXF86AudioMute}:         act(module.EvVolumeMute),
		{Mods: 0, Key: KeyXF86MonBrightnessUp}:   actNum(module.EvBrightness, 5),
		{Mods: 0, Key: KeyXF86MonBrightnessDown}: actNum(module.EvBrightness, -5),

		// ── Клавиша питания ──
		{Mods: 0, Key: KeyPower}: act(module.EvPowerOverlay),

		// ── Meta+E: проводник через xdg-open ──
		{Mods: ModMeta, Key: KeyE}: func(c *Ctx) {
			c.Pub.Publish(module.Event{Type: module.EvLaunch, Str: "xdg-open ."})
		},

		// ── Meta+Пробел: альтернативное переключение Ru/En ──
		{Mods: ModMeta, Key: KeySpace}: func(*Ctx) {
			ip.pub.Publish(module.Event{Type: module.EvLayoutChanged, Str: ip.layout.Toggle()})
		},
	}
	return ip
}

// Layout — текущая раскладка (для индикатора в оверлей-слое).
func (ip *InputProcessor) Layout() string { return ip.layout.Current() }

// HandleKeyEvent — единственная точка входа клавиатурных событий.
//
// Порядок обработки:
//  1. автоповтор — игнорируется (хоткеи не должны «стрелять» очередями);
//  2. Caps Lock — переключение раскладки с поглощением события;
//  3. press — детектор одиночного Meta, затем таблица хоткеев;
//  4. release Meta — финализация «одиночного Meta».
func (ip *InputProcessor) HandleKeyEvent(ev KeyEvent) {
	if ev.State == Repeat {
		return
	}

	// Caps Lock — переключатель Ru/En вместо фиксатора регистра:
	// событие поглощается и не доходит ни до xkb state, ни до клиента.
	if ev.Key == KeyCapsLock {
		if ev.State == Press {
			ip.pub.Publish(module.Event{Type: module.EvLayoutChanged, Str: ip.layout.Toggle()})
		}
		return
	}

	switch ev.State {
	case Press:
		if ev.Key == KeyMeta {
			ip.metaDown = true
			ip.otherKey = false
			return
		}
		if ip.metaDown {
			ip.otherKey = true // любая другая клавиша отменяет «одиночный Meta»
		}
		if act, ok := ip.hot[Combo{Mods: ev.Mods, Key: ev.Key}]; ok {
			act(&Ctx{Pub: ip.pub})
			return
		}

	case Release:
		if ev.Key == KeyMeta {
			ip.metaDown = false
			if !ip.otherKey {
				// Одиночный Meta: мгновенный сброс камеры в дефолт.
				ip.pub.Publish(module.Event{Type: module.EvResetCamera})
			}
			return
		}
		// Release-действия (залипание переключателей) появятся в Спринте 3;
		// пока release пропускается к клиенту без обработки.
	}
}

// ── Фабрики действий: хоткеи — данные, а не код в switch ────────────────

func act(t module.EventType) func(*Ctx) {
	return func(c *Ctx) { c.Pub.Publish(module.Event{Type: t}) }
}

func actNum(t module.EventType, n float64) func(*Ctx) {
	return func(c *Ctx) { c.Pub.Publish(module.Event{Type: t, Num: n}) }
}

func actDir(t module.EventType, d core.Dir) func(*Ctx) {
	return func(c *Ctx) { c.Pub.Publish(module.Event{Type: t, Dir: d}) }
}

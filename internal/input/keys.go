// Package input — подсистема ввода: таблица горячих клавиш
// (Windows 11 / KDE Plasma), переключение раскладки Caps Lock'ом.
//
// Реальные скан-коды приходят из Wayland-сервера через libxkbcommon
// (интеграция в Спринте 2). Здесь определены нейтральные keysym-константы,
// чтобы вся логика хоткеев была тестируемой без дисплея — это позволяет
// гнать CI на headless-машине.
package input

// Mods — битовая маска модификаторов сочетания.
type Mods uint8

const (
	ModCtrl Mods = 1 << iota
	ModAlt
	ModMeta
	ModShift
)

// KeySym — логическая клавиша (подмножество xkb keysyms, значимое для ядра;
// полный маппинг скан-кодов появится с интеграцией libxkbcommon).
type KeySym uint32

const (
	KeyNone KeySym = iota
	KeyMeta
	KeyCtrl
	KeyAlt
	KeyShift
	KeySpace
	KeyTab
	KeyEscape
	KeyEnter
	KeyF4
	KeyPrint
	KeyCapsLock
	KeyPower
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	// Буквенный ряд: только те, что участвуют в хоткеях ядра.
	KeyS
	KeyL
	KeyE
	KeyI
	KeyD
	KeyR
	KeyV
	// Мультимедийные Fn-клавиши (xkb keysyms XF86*).
	KeyXF86AudioRaiseVolume
	KeyXF86AudioLowerVolume
	KeyXF86AudioMute
	KeyXF86MonBrightnessUp
	KeyXF86MonBrightnessDown
)

// KeyState — фаза события клавиатуры.
type KeyState uint8

const (
	Press KeyState = iota
	Release
	Repeat // автоповтор
)

// KeyEvent — нормализованное событие клавиатуры от бэкенда.
type KeyEvent struct {
	Key   KeySym
	Mods  Mods
	State KeyState
}

// Combo — ключ таблицы хоткеев.
type Combo struct {
	Mods Mods
	Key  KeySym
}

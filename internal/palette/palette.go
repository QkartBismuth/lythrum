// Package palette — изолированная цветовая схема (тема) Lythrum.
//
// Все цвета объявлены переменными и заданы HEX-строками: пользователь
// редактирует их вручную. Формат "#RRGGBB" или "#RRGGBBAA".
//
// Принадлежность потокам: палитру читает только main loop (единственный
// владелец рендера), поэтому отдельной блокировки нет; Control Center
// в Спринте 5 будет менять значения через событие шины, а не из своей
// горутины.
package palette

import (
	"fmt"
	"strconv"
	"strings"
)

// Color — RGBA-цвет (каждый канал 0–255).
type Color struct{ R, G, B, A uint8 }

// Hex разбирает строку вида "#RRGGBB" или "#RRGGBBAA".
func Hex(s string) (Color, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	switch len(s) {
	case 6:
		s += "FF"
	case 8:
	default:
		return Color{}, fmt.Errorf("palette: неверный HEX %q: ждётся #RRGGBB или #RRGGBBAA", s)
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return Color{}, fmt.Errorf("palette: нечисловой HEX %q", s)
	}
	return Color{
		R: uint8(v >> 24),
		G: uint8(v >> 16),
		B: uint8(v >> 8),
		A: uint8(v),
	}, nil
}

// CSS возвращает цвет строкой #RRGGBBAA (для логов и экспорта темы).
func (c Color) CSS() string {
	return fmt.Sprintf("#%02X%02X%02X%02X", c.R, c.G, c.B, c.A)
}

func mustHex(s string) Color {
	c, err := Hex(s)
	if err != nil {
		panic(err.Error())
	}
	return c
}

// ─── Холст ────────────────────────────────────────────────────────────────
var (
	CanvasBackground = mustHex("#14151A") // фон бесконечного холста
	CanvasGrid       = mustHex("#1E2028") // опорная сетка (опционально)
)

// ─── Окна: границы в фокусе и без ────────────────────────────────────────
var (
	WindowBorderActive   = mustHex("#7C6CF6") // окно в фокусе
	WindowBorderInactive = mustHex("#2A2C38") // окно без фокуса
	WindowTitleText      = mustHex("#E8E9F0")
)

// ─── Контекстное меню (оверлей-слой, масштаб 1:1, стиль Windows) ─────────
var (
	MenuBackground = mustHex("#F2F3F7")
	MenuHoverItem  = mustHex("#D9DBE5")
	MenuText       = mustHex("#1B1C22")
	MenuSeparator  = mustHex("#C4C7D2")
	MenuShortcut   = mustHex("#6E7180") // текст хоткея справа от пункта
)

// ─── Оверлеи: поиск и питание ────────────────────────────────────────────
var (
	OverlayBackground = mustHex("#0E0F14E6") // с альфой
	OverlayAccent     = mustHex("#7C6CF6")
	ButtonActive      = mustHex("#8F7FFF")
	OverlayText       = mustHex("#F5F6FA")
)

// ─── Магнитная линия выравнивания ────────────────────────────────────────
var (
	SnapGuideLine = mustHex("#00D4AA")
)

// All — снапшот палитры по именам: для Control Center (Спринт 5) и отладки.
func All() map[string]Color {
	return map[string]Color{
		"CanvasBackground":     CanvasBackground,
		"CanvasGrid":           CanvasGrid,
		"WindowBorderActive":   WindowBorderActive,
		"WindowBorderInactive": WindowBorderInactive,
		"WindowTitleText":      WindowTitleText,
		"MenuBackground":       MenuBackground,
		"MenuHoverItem":        MenuHoverItem,
		"MenuText":             MenuText,
		"MenuSeparator":        MenuSeparator,
		"MenuShortcut":         MenuShortcut,
		"OverlayBackground":    OverlayBackground,
		"OverlayAccent":        OverlayAccent,
		"ButtonActive":         ButtonActive,
		"OverlayText":          OverlayText,
		"SnapGuideLine":        SnapGuideLine,
	}
}

// Validate — стартовые инварианты темы. Падать лучше на старте композитора,
// чем ловить невидимый текст посреди сессии.
func Validate() error {
	if CanvasBackground.A != 0xFF {
		return fmt.Errorf("palette: фон холста должен быть непрозрачным, получил %s", CanvasBackground.CSS())
	}
	if CanvasBackground == MenuBackground {
		return fmt.Errorf("palette: фон холста и фон меню совпадают — меню будет нечитаемо")
	}
	return nil
}

// ── Hot-reload (Спринт 5) ────────────────────────────────────────────────

// registry — роль → переменная. Заполняется один раз; переменные меняются
// только из main loop (через событие шины EvPaletteChanged) — отдельной
// блокировки не нужно, читатель тоже main loop.
var registry = map[string]*Color{
	"CanvasBackground":     &CanvasBackground,
	"CanvasGrid":           &CanvasGrid,
	"WindowBorderActive":   &WindowBorderActive,
	"WindowBorderInactive": &WindowBorderInactive,
	"WindowTitleText":      &WindowTitleText,
	"MenuBackground":       &MenuBackground,
	"MenuHoverItem":        &MenuHoverItem,
	"MenuText":             &MenuText,
	"MenuSeparator":        &MenuSeparator,
	"MenuShortcut":         &MenuShortcut,
	"OverlayBackground":    &OverlayBackground,
	"OverlayAccent":        &OverlayAccent,
	"ButtonActive":         &ButtonActive,
	"OverlayText":          &OverlayText,
	"SnapGuideLine":        &SnapGuideLine,
}

// Apply применяет правки палитры по именам ролей. ОТКАЗОБЕЗОПАСНОСТЬ:
// сначала валидируются ВСЕ правки; при хотя бы одной битой — тема не
// меняется вовсе (никаких «наполовину перекрашенных» кадров).
// Возвращает имена неизвестных ролей отдельным списком (не фатально).
func Apply(edits map[string]string) (unknown []string, err error) {
	// Валидация всего пакета правок.
	parsed := make(map[string]Color, len(edits))
	for role, hex := range edits {
		if _, ok := registry[role]; !ok {
			unknown = append(unknown, role)
			continue
		}
		c, err := Hex(hex)
		if err != nil {
			return nil, fmt.Errorf("palette: %s: %w", role, err)
		}
		parsed[role] = c
	}
	if err := validateWith(parsed); err != nil {
		return unknown, err
	}
	// Все правки валидны — применяем разом.
	for role, c := range parsed {
		*registry[role] = c
	}
	return unknown, nil
}

// validateWith — инварианты с учётом правок (поверх текущей темы).
func validateWith(edits map[string]Color) error {
	bg := CanvasBackground
	menu := MenuBackground
	if c, ok := edits["CanvasBackground"]; ok {
		bg = c
	}
	if c, ok := edits["MenuBackground"]; ok {
		menu = c
	}
	if bg.A != 0xFF {
		return fmt.Errorf("palette: фон холста должен быть непрозрачным, получил %s", bg.CSS())
	}
	if bg == menu {
		return fmt.Errorf("palette: фон холста и фон меню совпадают — меню будет нечитаемо")
	}
	return nil
}

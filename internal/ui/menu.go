// Package ui — модели фиксированного UI-слоя (LayerOverlay): контекстные
// меню, оверлей питания, поиск.
//
// Ключевой контракт слоя (требование 13): координаты и размеры здесь —
// ЭКРАННЫЕ пиксели, масштаб всегда 1:1. Меню НЕ знает о камере холста:
// при зуме 8x текст меню остаётся того же размера (тест 3.8). Преобразование
// «курсор холста → курсор экрана» делает ядро при вызове Open.
package ui

// MenuItem — пункт меню.
type MenuItem struct {
	Label     string // текст пункта
	Shortcut  string // подпись хоткея справа (например, "Ctrl+Shift+S")
	Icon      string // имя Material Symbol (render.IconGlyph)
	Separator bool   // пункт-разделитель (не интерактивен)
	Disabled  bool
}

// Геометрия меню в экранных пикселях.
const (
	ItemHeight   = 32.0 // высота строки пункта
	SepHeight    = 9.0  // высота разделителя
	MenuPadding  = 4.0  // внутренние отступы сверху/снизу
	MenuMinWidth = 180.0
	ScreenMargin = 8.0 // не прилипать к краю экрана вплотную
)

// Menu — открытая модель контекстного меню.
type Menu struct {
	Items []MenuItem

	X, Y float64 // позиция левого верхнего угла на экране
	W    float64 // ширина (высота вычисляется из items)

	hover int // индекс пункта под курсором/выделенного клавишами
}

// NewMenu создаёт меню (без позиции — её задаёт Open).
func NewMenu(items []MenuItem) *Menu {
	return &Menu{Items: items, W: MenuMinWidth, hover: firstSelectable(items)}
}

// firstSelectable — первый интерактивный пункт (навигация начинается с него).
func firstSelectable(items []MenuItem) int {
	for i, it := range items {
		if selectable(it) {
			return i
		}
	}
	return -1
}

func selectable(it MenuItem) bool { return !it.Separator && !it.Disabled }

// Height — полная высота меню.
func (m *Menu) Height() float64 {
	h := 2 * MenuPadding
	for _, it := range m.Items {
		if it.Separator {
			h += SepHeight
		} else {
			h += ItemHeight
		}
	}
	return h
}

// Open позиционирует меню в точке экрана (x, y) с флипом у краёв
// (право/низ переворачивают якорь, чтобы меню осталось видимым).
// screenW/H — размер экрана; всё в экранных пикселях, зум холста не участвует.
func (m *Menu) Open(x, y, screenW, screenH float64) {
	if m.W < MenuMinWidth {
		m.W = MenuMinWidth
	}
	if x+m.W+ScreenMargin > screenW {
		x = screenW - m.W - ScreenMargin
	}
	h := m.Height()
	if y+h+ScreenMargin > screenH {
		y = screenH - h - ScreenMargin
	}
	if x < 0 {
		x = ScreenMargin
	}
	if y < 0 {
		y = ScreenMargin
	}
	m.X, m.Y = x, y
	m.hover = firstSelectable(m.Items)
}

// HoverAt устанавливает выделение по координате мыши; возвращает индекс
// (или -1, если точка вне пунктов).
func (m *Menu) HoverAt(my float64) int {
	rel := my - m.Y - MenuPadding
	for i, it := range m.Items {
		h := ItemHeight
		if it.Separator {
			h = SepHeight
		}
		if rel >= 0 && rel < h {
			if selectable(it) {
				m.hover = i
			}
			return m.hover
		}
		rel -= h
	}
	return -1
}

// Move двигает выделение стрелками; разделители и disabled пропускаются.
// dir = +1 (вниз) / -1 (вверх).
func (m *Menu) Move(dir int) {
	if len(m.Items) == 0 {
		return
	}
	cur := m.hover
	for range len(m.Items) {
		cur = ((cur+dir)%len(m.Items) + len(m.Items)) % len(m.Items)
		if selectable(m.Items[cur]) {
			m.hover = cur
			return
		}
	}
}

// Activate — Enter/клик: возвращает Label выбранного пункта.
// ok=false, если выбран разделитель/disabled/ничего не выбрано.
func (m *Menu) Activate() (label string, ok bool) {
	if m.hover < 0 || m.hover >= len(m.Items) {
		return "", false
	}
	it := m.Items[m.hover]
	if !selectable(it) {
		return "", false
	}
	return it.Label, true
}

// Hover — индекс выделенного пункта (для рендера).
func (m *Menu) Hover() int { return m.hover }

// ── Стек меню (подменю): Esc закрывает верхнее ──────────────────────────

// MenuStack — открытые меню (корневое + подменю).
type MenuStack struct {
	stack []*Menu
}

// Push открывает меню поверх (подменю).
func (s *MenuStack) Push(m *Menu) { s.stack = append(s.stack, m) }

// Pop закрывает верхнее меню; возвращает false, если стек пуст.
func (s *MenuStack) Pop() bool {
	if len(s.stack) == 0 {
		return false
	}
	s.stack = s.stack[:len(s.stack)-1]
	return true
}

// Top — верхнее меню (nil, если стек пуст).
func (s *MenuStack) Top() *Menu {
	if len(s.stack) == 0 {
		return nil
	}
	return s.stack[len(s.stack)-1]
}

// Key обрабатывает навигацию клавишами: Up/Down/Enter/Escape.
// Возвращает (action, ok): action = Label выбранного пункта; ok = меню
// осталось открытым (false = стек опустел — меню закрыто).
func (s *MenuStack) Key(up, down, enter, escape bool) (string, bool) {
	top := s.Top()
	if top == nil {
		return "", false
	}
	switch {
	case escape:
		s.Pop()
	case up:
		top.Move(-1)
	case down:
		top.Move(1)
	case enter:
		label, ok := top.Activate()
		if ok {
			return label, true // действие меню
		}
	}
	if s.Top() == nil {
		return "", false
	}
	return "", true
}

// ── Оверлей питания (клавиша Power) ─────────────────────────────────────

// PowerOverlay — модель меню питания (реиспользует Menu с иконками).
func PowerOverlay() *Menu {
	return NewMenu([]MenuItem{
		{Label: "Заблокировать", Icon: "lock"},
		{Label: "Выйти", Icon: "logout"},
		{Label: "Спящий режим", Icon: "bedtime"},
		{Label: "Перезагрузка", Icon: "restart_alt"},
		{Label: "Выключение", Icon: "power_settings_new"},
	})
}

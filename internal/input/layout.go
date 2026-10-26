package input

import "sync"

// LayoutMachine — машина раскладок клавиатуры (Ru/En).
//
// По требованиям Lythrum Caps Lock — НЕ фиксатор регистра, а переключатель
// раскладки (вдобавок к Meta+Пробел). Стандартное поведение xkb отключается:
// событие Caps Lock перехватывается InputProcessor'ом и до клиента не доходит.
type LayoutMachine struct {
	mu      sync.Mutex
	layouts []string
	idx     int
}

// NewLayoutMachine создаёт машину раскладок; без аргументов — En→Ru.
func NewLayoutMachine(layouts ...string) *LayoutMachine {
	if len(layouts) == 0 {
		layouts = []string{"us", "ru"}
	}
	return &LayoutMachine{layouts: layouts}
}

// Toggle переключает раскладку по кольцу и возвращает новую.
func (l *LayoutMachine) Toggle() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.idx = (l.idx + 1) % len(l.layouts)
	return l.layouts[l.idx]
}

// Current возвращает активную раскладку.
func (l *LayoutMachine) Current() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.layouts[l.idx]
}

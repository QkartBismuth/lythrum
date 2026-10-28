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

// Char — печатаемый символ клавиши в текущей раскладке.
// shift учитывается (заглавные, знаки второго ряда). ok=false — клавиша
// не печатаемая (функциональная), символ не публикуется.
//
// Контракт: это модель того же, что делает xkb в реальном бэкенде,
// поэтому таблицы статичны и проверяются тестами.
func (l *LayoutMachine) Char(k KeySym, shift bool) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.layouts[l.idx] == "ru" {
		return ruChar(k, shift)
	}
	return usChar(k, shift)
}

// usChar — латиница (позиции совпадают с names).
func usChar(k KeySym, shift bool) (string, bool) {
	// Буквы: KeyA..KeyZ подряд не идут (хоткейные вставлены в середину) —
	// таблица явная, чтобы порядок iota не имел значения.
	latin := map[KeySym]rune{
		KeyQ: 'q', KeyW: 'w', KeyE: 'e', KeyR: 'r', KeyT: 't',
		KeyY: 'y', KeyU: 'u', KeyI: 'i', KeyO: 'o', KeyP: 'p',
		KeyA: 'a', KeyS: 's', KeyD: 'd', KeyF: 'f', KeyG: 'g',
		KeyH: 'h', KeyJ: 'j', KeyK: 'k', KeyL: 'l',
		KeyZ: 'z', KeyX: 'x', KeyC: 'c', KeyV: 'v', KeyB: 'b',
		KeyN: 'n', KeyM: 'm',
	}
	if r, ok := latin[k]; ok {
		if shift {
			r = r - 'a' + 'A'
		}
		return string(r), true
	}
	if !shift {
		digits := map[KeySym]rune{
			Key0: '0', Key1: '1', Key2: '2', Key3: '3', Key4: '4',
			Key5: '5', Key6: '6', Key7: '7', Key8: '8', Key9: '9',
		}
		if r, ok := digits[k]; ok {
			return string(r), true
		}
		punct := map[KeySym]rune{
			KeyMinus: '-', KeyEqual: '=', KeyComma: ',',
			KeyPeriod: '.', KeySlash: '/',
		}
		if r, ok := punct[k]; ok {
			return string(r), true
		}
	}
	// Знаки второго ряда us-раскладки (Shift+цифра).
	if shift {
		shifted := map[KeySym]rune{
			Key1: '!', Key2: '@', Key3: '#', Key4: '$', Key5: '%',
			Key6: '^', Key7: '&', Key8: '*', Key9: '(', Key0: ')',
		}
		if r, ok := shifted[k]; ok {
			return string(r), true
		}
	}
	if k == KeySpace {
		return " ", true
	}
	return "", false
}

// ruChar — ЙЦУКЕН: буквы транслитерируются с латинских позиций,
// «б»/«ю» встают на запятую/точку, «.» — на слэш.
func ruChar(k KeySym, shift bool) (string, bool) {
	jcuken := map[KeySym]rune{
		KeyQ: 'й', KeyW: 'ц', KeyE: 'у', KeyR: 'к', KeyT: 'е',
		KeyY: 'н', KeyU: 'г', KeyI: 'ш', KeyO: 'щ', KeyP: 'з',
		KeyA: 'ф', KeyS: 'ы', KeyD: 'в', KeyF: 'а', KeyG: 'п',
		KeyH: 'р', KeyJ: 'о', KeyK: 'л', KeyL: 'д',
		KeyZ: 'я', KeyX: 'ч', KeyC: 'с', KeyV: 'м', KeyB: 'и',
		KeyN: 'т', KeyM: 'ь',
	}
	if r, ok := jcuken[k]; ok {
		if shift {
			// Заглавная: юникод-блок кириллицы плотный (а..я → А..Я, −32);
			// «ё» в таблице нет — исключений не возникает.
			r -= 32
		}
		return string(r), true
	}
	// Пунктуация ru-раскладки.
	punct := map[KeySym]rune{
		KeyComma: 'б', KeyPeriod: 'ю', KeySlash: '.',
	}
	if r, ok := punct[k]; ok && !shift {
		return string(r), true
	}
	if k == KeySlash && shift {
		return ",", true // Shift+слэш в ru — запятая
	}
	// Цифры, минус, равенство, пробел — как в us.
	return usChar(k, shift)
}

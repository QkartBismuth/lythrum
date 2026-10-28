package ui

// TextInput — модель строки ввода диалогов (поиск, запуск, Спринт 7).
//
// Сознательно минимальна: каретка всегда в конце (курсор стрелками внутри
// строки появится вместе с pointer-захватом и построчным рендером).
type TextInput struct {
	buffer []rune
}

// Type дописывает символ(ы) в конец.
func (t *TextInput) Type(s string) {
	t.buffer = append(t.buffer, []rune(s)...)
}

// Backspace стирает последний символ; на пустой строке — no-op.
func (t *TextInput) Backspace() bool {
	if len(t.buffer) == 0 {
		return false
	}
	t.buffer = t.buffer[:len(t.buffer)-1]
	return true
}

// Value — текущее содержимое.
func (t *TextInput) Value() string { return string(t.buffer) }

// Empty — буфер пуст.
func (t *TextInput) Empty() bool { return len(t.buffer) == 0 }

// Reset очищает буфер.
func (t *TextInput) Reset() { t.buffer = t.buffer[:0] }

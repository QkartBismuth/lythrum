package core

import "sync/atomic"

// Глобальный генератор уникальных ID (карточки, виджеты, меню).
// Достаточно uint64-счётчика: ID живут в пределах сессии и файла сессии.
var idCounter = new(atomic.Uint64)

// NewID — монотонный уникальный строковый идентификатор.
func NewID() string {
	return "id-" + itoa(idCounter.Add(1))
}

func itoa(n uint64) string {
	var buf [24]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

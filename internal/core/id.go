package core

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync/atomic"
)

// Глобальный генератор уникальных ID (карточки, виджеты, меню).
// Достаточно uint64-счётчика: ID живут в пределах сессии и файла сессии.
var idCounter = new(atomic.Uint64)

// NewID — монотонный уникальный строковый идентификатор.
func NewID() string {
	return "id-" + strconv.FormatUint(idCounter.Add(1), 10)
}

// NewToken — криптостойкий случайный токен (IPC-аутентификация виджетов):
// 128 бит энтропии, hex. Ошибка crypto/rand — паника допустима: токены
// нужны для БЕЗОПАСНОСТИ, работать с предсказуемыми токенами нельзя.
func NewToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("core: crypto/rand недоступен: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

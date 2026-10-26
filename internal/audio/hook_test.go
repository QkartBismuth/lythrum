package audio

import (
	"testing"

	"lythrum/internal/module"
)

// Hook: события ядра отображаются на правильные звуки схемы.
func TestSoundHookMapping(t *testing.T) {
	m := NewManager("nonexistent", nil) // файлы не нужны: ловим имена из очереди
	go func() { _ = m.Run(t.Context()) }()

	hook := SoundHook(m)
	cases := []struct {
		ev   module.EventType
		want string
	}{
		{module.EvLayoutChanged, SoundClick},
		{module.EvResetCamera, SoundClick},
		{module.EvToggleSearch, SoundNotify},
		{module.EvDroppedFile, SoundNotify},
		{module.EvPowerOverlay, SoundDevice},
		{module.EvLockSession, SoundDevice},
		{module.EvVolumeMute, SoundDevice},
		{module.EvQuit, SoundPower},
	}
	for _, c := range cases {
		hook(module.Event{Type: c.ev})
	}

	// Все имена ушли в очередь в правильном порядке.
	for _, c := range cases {
		select {
		case got := <-m.queue:
			if got != c.want {
				t.Fatalf("%v: звук %q, want %q", c.ev, got, c.want)
			}
		default:
			t.Fatalf("%v: событие не дошло до очереди", c.ev)
		}
	}
}

// Hook с nil-менеджером безопасен (беззвучный режим).
func TestSoundHookNilSafe(t *testing.T) {
	hook := SoundHook(nil)
	hook(module.Event{Type: module.EvQuit}) // не должно паниковать
}

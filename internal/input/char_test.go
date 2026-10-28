package input

import (
	"testing"

	"lythrum/internal/module"
)

// collectBus — шина-сборщик событий для тестов.
func collectBus(cap int) (*module.ChanPublisher, func() []module.Event) {
	bus := &module.ChanPublisher{C: make(chan module.Event, cap)}
	return bus, func() []module.Event {
		var out []module.Event
		for {
			select {
			case ev := <-bus.C:
				out = append(out, ev)
			default:
				return out
			}
		}
	}
}

// 7.1 — печать латиницы: символ уходит в шину, хоткеи не ломаются.
func TestCharTyping(t *testing.T) {
	bus, drain := collectBus(16)
	ip := NewProcessor(bus, NewLayoutMachine())

	// «foot»: f-o-o-t.
	for _, k := range []KeySym{KeyF, KeyO, KeyO, KeyT} {
		ip.HandleKeyEvent(KeyEvent{Key: k, State: Press})
	}
	events := drain()
	if len(events) != 4 || events[0].Type != module.EvChar {
		t.Fatalf("событий: %d, want 4 × EvChar", len(events))
	}
	if got := joinChars(events); got != "foot" {
		t.Fatalf("набрано: %q, want %q", got, "foot")
	}

	// Meta+F — хоткей (не символ): в шине EvLaunch от Meta+E… нет, F не забинден —
	// значит ни символа, ни события.
	ip.HandleKeyEvent(KeyEvent{Key: KeyF, Mods: ModMeta, State: Press})
	if events := drain(); len(events) != 0 {
		t.Fatalf("Meta+F дал %v — символ не должен публиковаться", events)
	}

	// Ctrl+C — не символ (команда клиента в будущем).
	ip.HandleKeyEvent(KeyEvent{Key: KeyC, Mods: ModCtrl, State: Press})
	if events := drain(); len(events) != 0 {
		t.Fatalf("Ctrl+C дал %v", events)
	}
}

// 7.1 — раскладка: us → латиница, ru → ЙЦУКЕН, Shift — верхний регистр.
func TestCharLayouts(t *testing.T) {
	bus, drain := collectBus(32)
	ip := NewProcessor(bus, NewLayoutMachine())

	ip.HandleKeyEvent(KeyEvent{Key: KeyS, State: Press}) // us: "s"
	if got := joinChars(drain()); got != "s" {
		t.Fatalf("us: %q, want s", got)
	}

	ip.HandleKeyEvent(KeyEvent{Key: KeyCapsLock, State: Press}) // Caps: us→ru
	if ev := drain(); len(ev) != 1 || ev[0].Type != module.EvLayoutChanged {
		t.Fatalf("Caps не переключил раскладку: %v", ev)
	}

	ip.HandleKeyEvent(KeyEvent{Key: KeyF, State: Press})                 // ru: а
	ip.HandleKeyEvent(KeyEvent{Key: KeyO, State: Press})                 // ru: щ
	ip.HandleKeyEvent(KeyEvent{Key: KeyF, Mods: ModShift, State: Press}) // А
	events := drain()
	if got := joinChars(events); got != "ащА" {
		t.Fatalf("ru: %q, want ащА", got)
	}

	// Пунктуация ЙЦУКЕН: запятая-клавиша → б, точка-клавиша → ю.
	ip.HandleKeyEvent(KeyEvent{Key: KeyComma, State: Press})
	ip.HandleKeyEvent(KeyEvent{Key: KeyPeriod, State: Press})
	if got := joinChars(drain()); got != "бю" {
		t.Fatalf("пунктуация ru: %q, want бю", got)
	}
}

// Backspace публикуется отдельным событием.
func TestBackspaceEvent(t *testing.T) {
	bus, drain := collectBus(8)
	ip := NewProcessor(bus, NewLayoutMachine())

	ip.HandleKeyEvent(KeyEvent{Key: KeyBackspace, State: Press})
	events := drain()
	if len(events) != 1 || events[0].Type != module.EvBackspace {
		t.Fatalf("событие: %v, want EvBackspace", events)
	}
}

// Цифры и Shift-знаки us.
func TestDigitsAndShifted(t *testing.T) {
	bus, drain := collectBus(16)
	ip := NewProcessor(bus, NewLayoutMachine())

	ip.HandleKeyEvent(KeyEvent{Key: Key5, State: Press})
	ip.HandleKeyEvent(KeyEvent{Key: Key5, Mods: ModShift, State: Press}) // %
	ip.HandleKeyEvent(KeyEvent{Key: KeySpace, State: Press})
	events := drain()
	if got := joinChars(events); got != "5% " {
		t.Fatalf("цифры/знаки: %q, want %q", got, "5% ")
	}
}

func joinChars(events []module.Event) string {
	s := ""
	for _, ev := range events {
		if ev.Type == module.EvChar {
			s += ev.Str
		}
	}
	return s
}

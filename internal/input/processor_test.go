package input

import (
	"sync"
	"testing"

	"lythrum/internal/module"
)

// catcher собирает опубликованные события для asserts.
type catcher struct {
	mu  sync.Mutex
	evs []module.Event
}

func (c *catcher) Publish(e module.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evs = append(c.evs, e)
}

func (c *catcher) of(t module.EventType) []module.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []module.Event
	for _, e := range c.evs {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func (c *catcher) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.evs)
}

// 2.1 — Caps Lock переключает Ru/En и ПОГЛОЩАЕТСЯ (до клиента не доходит).
func TestCapsLockTogglesLayout(t *testing.T) {
	var c catcher
	ip := NewProcessor(&c, nil) // по умолчанию us→ru

	ip.HandleKeyEvent(KeyEvent{Key: KeyCapsLock, State: Press})
	ip.HandleKeyEvent(KeyEvent{Key: KeyCapsLock, State: Release}) // релиз не должен ничего делать

	got := c.of(module.EvLayoutChanged)
	if len(got) != 1 || got[0].Str != "ru" {
		t.Fatalf("первое нажатие: %v, want [layout=ru]", got)
	}

	ip.HandleKeyEvent(KeyEvent{Key: KeyCapsLock, State: Press})
	if got := c.of(module.EvLayoutChanged); len(got) != 2 || got[1].Str != "us" {
		t.Fatalf("второе нажатие: %v, want [layout=us]", got)
	}
	if total := c.total(); total != 2 {
		t.Fatalf("Caps Lock пропустил лишние события: всего %d", total)
	}
}

// 2.2 — одиночный Meta (press → release без других клавиш) сбрасывает камеру.
func TestSingleMetaResetsCamera(t *testing.T) {
	var c catcher
	ip := NewProcessor(&c, nil)

	ip.HandleKeyEvent(KeyEvent{Key: KeyMeta, State: Press})
	ip.HandleKeyEvent(KeyEvent{Key: KeyMeta, State: Release})

	if got := c.of(module.EvResetCamera); len(got) != 1 {
		t.Fatalf("want 1 событие camera.reset, got %d", len(got))
	}
}

// 2.3 — Meta, использованный в сочетании, НЕ считается одиночным:
// Meta+S публикует ui.search, релиз Meta — ничего.
func TestMetaComboCancelsSingleMeta(t *testing.T) {
	var c catcher
	ip := NewProcessor(&c, nil)

	ip.HandleKeyEvent(KeyEvent{Key: KeyMeta, State: Press})
	ip.HandleKeyEvent(KeyEvent{Key: KeyS, Mods: ModMeta, State: Press})
	ip.HandleKeyEvent(KeyEvent{Key: KeyS, Mods: ModMeta, State: Release})
	ip.HandleKeyEvent(KeyEvent{Key: KeyMeta, State: Release})

	if got := c.of(module.EvToggleSearch); len(got) != 1 {
		t.Fatalf("want 1 ui.search, got %d", len(got))
	}
	if got := c.of(module.EvResetCamera); len(got) != 0 {
		t.Fatalf("сброс камеры не должен был случиться, got %d", len(got))
	}
}

// 2.4 — Fn-мультимедиа клавиши и Power маршрутизируются в шину.
func TestFnKeysAndPower(t *testing.T) {
	var c catcher
	ip := NewProcessor(&c, nil)

	cases := []struct {
		key  KeySym
		want module.EventType
		num  float64
	}{
		{KeyXF86AudioRaiseVolume, module.EvVolume, 5},
		{KeyXF86AudioLowerVolume, module.EvVolume, -5},
		{KeyXF86AudioMute, module.EvVolumeMute, 0},
		{KeyXF86MonBrightnessUp, module.EvBrightness, 5},
		{KeyXF86MonBrightnessDown, module.EvBrightness, -5},
		{KeyPower, module.EvPowerOverlay, 0},
	}
	for _, tc := range cases {
		ip.HandleKeyEvent(KeyEvent{Key: tc.key, State: Press})
		got := c.of(tc.want)
		if len(got) == 0 {
			t.Fatalf("клавиша %d не опубликовала %q", tc.key, tc.want)
		}
		last := got[len(got)-1]
		if last.Num != tc.num {
			t.Fatalf("клавиша %d: Num=%v, want %v", tc.key, last.Num, tc.num)
		}
	}
}

// 2.5 — автоповтор не триггерит хоткеи.
func TestRepeatIgnored(t *testing.T) {
	var c catcher
	ip := NewProcessor(&c, nil)

	ip.HandleKeyEvent(KeyEvent{Key: KeyS, Mods: ModMeta, State: Repeat})
	if c.total() != 0 {
		t.Fatalf("Repeat пробился в шину: %d событий", c.total())
	}
}

// 2.6 — Meta+Стрелки публикуют ui.tile с корректным направлением.
func TestMetaArrowsTile(t *testing.T) {
	var c catcher
	ip := NewProcessor(&c, nil)

	cases := []struct {
		key KeySym
		dir module.Event
	}{
		{KeyLeft, module.Event{Type: module.EvTile}},
		{KeyRight, module.Event{Type: module.EvTile}},
		{KeyUp, module.Event{Type: module.EvTile}},
		{KeyDown, module.Event{Type: module.EvTile}},
	}
	dirs := []uint8{0, 1, 2, 3} // DirLeft..DirDown
	for i, tc := range cases {
		ip.HandleKeyEvent(KeyEvent{Key: KeyMeta, State: Press})
		ip.HandleKeyEvent(KeyEvent{Key: tc.key, Mods: ModMeta, State: Press})
		got := c.of(module.EvTile)
		if len(got) != i+1 {
			t.Fatalf("стрелка %d: want %d событий ui.tile, got %d", i, i+1, len(got))
		}
		if uint8(got[i].Dir) != dirs[i] {
			t.Fatalf("стрелка %d: Dir=%d, want %d", i, got[i].Dir, dirs[i])
		}
	}
}

// 2.6b — Meta+Пробел — альтернативный переключатель раскладки.
func TestMetaSpaceTogglesLayout(t *testing.T) {
	var c catcher
	ip := NewProcessor(&c, nil)

	ip.HandleKeyEvent(KeyEvent{Key: KeyMeta, State: Press})
	ip.HandleKeyEvent(KeyEvent{Key: KeySpace, Mods: ModMeta, State: Press})

	if got := c.of(module.EvLayoutChanged); len(got) != 1 || got[0].Str != "ru" {
		t.Fatalf("want [layout=ru], got %v", got)
	}
}

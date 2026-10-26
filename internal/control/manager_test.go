package control

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lythrum/internal/module"
)

// 5.3 — запись настроек из другой горутины: гонок нет, hot-reload доставлен.
func TestConfigHotReload(t *testing.T) {
	m := New()

	var delivered atomic.Int64
	var lastSnap atomic.Value
	unsub := m.Subscribe(func(c Config) {
		lastSnap.Store(c)
		delivered.Add(1)
	})
	defer unsub()

	// Обновление из «чужой горутины» (как это делает UI Control Center).
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 50 {
			vol := 50 + i
			m.Update(func(c *Config) { c.Volume = vol })
		}
	}()
	<-done

	// Все 50 версий доставлены подписчику, финальный снапшот свежий.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && delivered.Load() < 50 {
		time.Sleep(1 * time.Millisecond)
	}
	if got := delivered.Load(); got != 50 {
		t.Fatalf("доставлено %d обновлений, want 50", got)
	}
	snap, _ := lastSnap.Load().(Config)
	if snap.Volume != 99 {
		t.Fatalf("финальный снапшот: volume=%d, want 99", snap.Volume)
	}
	if got := m.Get().Volume; got != 99 {
		t.Fatalf("Get: volume=%d, want 99", got)
	}
	if m.Version() != 50 {
		t.Fatalf("версия: %d, want 50", m.Version())
	}
}

// Конкурентные писатели и читатели: -race чисто, снапшоты консистентны.
func TestConfigConcurrentAccess(t *testing.T) {
	m := New()

	var wg sync.WaitGroup
	for w := range 8 { // 8 писателей
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 40 {
				m.Update(func(c *Config) {
					c.SnapThresholdPx = float64(w*100 + i)
					c.Muted = i%2 == 0
				})
			}
		}()
	}
	for range 4 { // 4 читателя снапшотов
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 40 {
				c := m.Get()
				if c.SnapThresholdPx < 0 || c.Volume > 100 {
					t.Errorf("неконсистентный снапшот: %+v", c)
				}
			}
		}()
	}
	wg.Wait()
}

// Отписка: после unsubscribe обновления не доставляются.
func TestConfigUnsubscribe(t *testing.T) {
	m := New()
	var n atomic.Int64
	unsub := m.Subscribe(func(Config) { n.Add(1) })

	m.Update(func(c *Config) { c.Volume = 10 })
	unsub()
	m.Update(func(c *Config) { c.Volume = 20 })

	// Доставка синхронная внутри Update — счётчик уже финальный.
	if n.Load() != 1 {
		t.Fatalf("после отписки доставок: %d, want 1", n.Load())
	}
}

// Снапшот иммутабелен: мутация среза подписчиком не портит хранилище.
func TestConfigSnapshotIsolation(t *testing.T) {
	m := New()
	m.Update(func(c *Config) { c.WidgetDefaultPerms = []string{"notify"} })

	snap := m.Get()
	snap.WidgetDefaultPerms[0] = "испорчено"
	if m.Get().WidgetDefaultPerms[0] != "notify" {
		t.Fatal("снапшот не изолирован от хранилища")
	}
}

// Сеттеры: валидный HEX уходит в шину, битый — нет (шина не дёргается).
func TestSettersPublishValidated(t *testing.T) {
	m := New()
	bus := &module.ChanPublisher{C: make(chan module.Event, 16)}

	if err := m.SetPalette(bus, "WindowBorderActive", "#FF8800"); err != nil {
		t.Fatalf("SetPalette: %v", err)
	}
	if err := m.SetPalette(bus, "NoSuchRole", "#FF8800"); err != nil {
		t.Fatalf("неизвестная роль не валидируется здесь — применит main loop: %v", err)
	}
	if err := m.SetPalette(bus, "WindowBorderActive", "не hex"); err == nil {
		t.Fatal("битый HEX должен быть отклонён")
	}
	m.SetSnapThreshold(bus, 24)
	m.SetVolume(bus, 150) // зажим в 0..100
	m.SetVolume(bus, -5)  // тоже публикуется (с зажимом в 0)

	if got := len(bus.C); got != 5 {
		t.Fatalf("событий в шине: %d, want 5 (валидные; битый HEX отсеян)", got)
	}
	ev := <-bus.C
	if ev.Type != module.EvPaletteChanged || ev.Str == "" {
		t.Fatalf("первое событие: %+v", ev)
	}
	if ev := <-bus.C; ev.Type != module.EvPaletteChanged {
		t.Fatalf("второе событие: %+v", ev)
	}
	if ev := <-bus.C; ev.Type != module.EvSnapThreshold || ev.Num != 24 {
		t.Fatalf("порог: %+v", ev)
	}
	if ev := <-bus.C; ev.Type != module.EvVolume || ev.Num != 100 {
		t.Fatalf("громкость должна зажаться в 100: %+v", ev)
	}
	if ev := <-bus.C; ev.Type != module.EvVolume || ev.Num != 0 {
		t.Fatalf("громкость должна зажаться в 0: %+v", ev)
	}
}

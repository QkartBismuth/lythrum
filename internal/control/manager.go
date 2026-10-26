// Package control — Control Center: горячие настройки композитора с
// hot-reload (Спринт 5).
//
// Контракт гонок: настройки могут менять ЛЮБЫЕ горутины (Control Center
// UI, виджеты через IPC, CLI), а читают они снапшоты. Всё под
// sync.RWMutex; подписчики hot-reload вызываются ВНЕ блокировки и
// обязаны быть неблокирующими (типично — публикация события в шину
// main loop, чтобы применение осталось за владельцем состояния).
package control

import (
	"encoding/json"
	"sync"

	"lythrum/internal/module"
	"lythrum/internal/palette"
)

// Config — снимок настроек. Все поля — простые значения; строки —
// пользовательские имена (палитра, пути) для ручного редактирования.
type Config struct {
	// Холст.
	SnapThresholdPx float64 // магнитный порог в экранных пикселях

	// Жесты.
	AlphaPan        float64 // сглаживание панорамирования [0..1]
	ZoomSensitivity float64 // множитель пинча

	// Звук и обои.
	Volume    int // 0–100
	Muted     bool
	Wallpaper string // путь; "" — без обоев

	// Виджеты: права по умолчанию для новых (белый список имён).
	WidgetDefaultPerms []string
}

// Default — настройки по умолчанию (совпадают с дефолтами подсистем).
func Default() Config {
	return Config{
		SnapThresholdPx: 8,
		AlphaPan:        0.6,
		ZoomSensitivity: 0.005,
		Volume:          75,
	}
}

// Manager — потокобезопасное хранилище конфигурации с hot-reload.
type Manager struct {
	mu   sync.RWMutex
	cfg  Config
	ver  uint64
	subs map[uint64]func(Config)
	next uint64 // id-генератор подписок (под mu)
}

// New создаёт менеджер с настройками по умолчанию.
func New() *Manager {
	return &Manager{cfg: Default(), subs: make(map[uint64]func(Config))}
}

// Get — снапшот конфигурации (копия; срез копируется — снапшот иммутабелен).
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := m.cfg
	c.WidgetDefaultPerms = append([]string(nil), m.cfg.WidgetDefaultPerms...)
	return c
}

// Version — номер редакции (растёт при каждом Update; для отладки hot-reload).
func (m *Manager) Version() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ver
}

// Update применяет мутацию под замком, инкрементирует версию и рассылает
// новый снапшот подписчикам. Рассылка — ВНЕ mu (подписчик может звать
// Get/Update — иначе дедлок), в горутине вызывающего.
func (m *Manager) Update(fn func(*Config)) {
	m.mu.Lock()
	fn(&m.cfg)
	m.ver++
	// Снапшот-копия ПОД тем же write-локом (Get() нельзя: RLock внутри
	// Lock — дедлок; RWMutex не реентерабелен).
	cfg := m.cfg
	cfg.WidgetDefaultPerms = append([]string(nil), m.cfg.WidgetDefaultPerms...)
	m.mu.Unlock()

	m.mu.RLock()
	subs := make([]func(Config), 0, len(m.subs))
	for _, fn := range m.subs {
		subs = append(subs, fn)
	}
	m.mu.RUnlock()

	for _, fn := range subs {
		fn(cfg)
	}
}

// Subscribe добавляет подписчика hot-reload; возвращает функцию отписки.
func (m *Manager) Subscribe(fn func(Config)) (unsubscribe func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	id := m.next
	m.subs[id] = fn
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.subs, id)
	}
}

// ── Высокоуровневые сеттеры Control Center ──────────────────────────────
//
// Применение настроек выполняет ТОЛЬКО main loop (владелец холста и
// темы): сеттеры валидируют значение и публикуют событие в шину.
// Ошибка валидации — возврат, шина не дёргается.

// SetPalette публикует правку палитры (роль → HEX). Валидация HEX здесь —
// чтобы CC показывал ошибку сразу, а не после круга через main loop.
func (m *Manager) SetPalette(bus module.Publisher, role, hex string) error {
	if _, err := palette.Hex(hex); err != nil {
		return err
	}
	edits, _ := json.Marshal(map[string]string{role: hex})
	bus.Publish(module.Event{Type: module.EvPaletteChanged, Str: string(edits)})
	return nil
}

// SetSnapThreshold публикует магнитный порог (экранные пиксели).
func (m *Manager) SetSnapThreshold(bus module.Publisher, px float64) {
	bus.Publish(module.Event{Type: module.EvSnapThreshold, Num: px})
}

// SetVolume публикует громкость (0–100); mute отдельно.
func (m *Manager) SetVolume(bus module.Publisher, volume int) {
	if volume < 0 {
		volume = 0
	}
	if volume > 100 {
		volume = 100
	}
	bus.Publish(module.Event{Type: module.EvVolume, Num: float64(volume)})
}

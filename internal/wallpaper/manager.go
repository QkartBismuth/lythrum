package wallpaper

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"lythrum/internal/module"
)

// Manager — модуль обоев: владеет провайдером, изолирует его отказы и
// хранит последний кадр для рендера.
//
// Паттерн отказоустойчивости (требование Спринта 3.6): провайдер работает
// в дочерней горутине с recover()'ом; падение/ошибка → backoff → перезапуск
// источника. Пан холста при этом не замечает ничего.
type Manager struct {
	// source — путь к файлу обоев ("" — обоев нет).
	source string

	// decoder — декодер видео (подменяется в тестах).
	decoder VideoDecoder

	// Размер вьюпорта рендера обоев (обновляется при смене output'ов).
	vw, vh int

	// backoff конфигурируется для тестов.
	backoffBase time.Duration
	backoffMax  time.Duration

	mu      sync.RWMutex
	current *Frame // последний кадр (latest-wins; nil — фон не готов)

	// restarts — метрика здоровья для Control Center.
	restarts int
}

// NewManager создаёт менеджер обоев (без источника — пустой фон).
func NewManager(decoder VideoDecoder) *Manager {
	return &Manager{
		decoder:     decoder,
		vw:          1920,
		vh:          1080,
		backoffBase: 200 * time.Millisecond,
		backoffMax:  5 * time.Second,
	}
}

// SetViewport обновляет размер рендера обоев (при смене output'ов).
func (m *Manager) SetViewport(w, h int) {
	if w > 0 {
		m.vw = w
	}
	if h > 0 {
		m.vh = h
	}
}

// SetSource меняет обои «на лету» (Control Center). Формат пути см. KindOf.
func (m *Manager) SetSource(path string) {
	m.mu.Lock()
	m.source = path
	m.current = nil
	m.mu.Unlock()
}

// Source — текущий путь обоев.
func (m *Manager) Source() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.source
}

// Restarts — число перезапусков провайдера (метрика для Control Center).
func (m *Manager) Restarts() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.restarts
}

// Current — последний кадр (снапшот указателя; кадры иммутабельны после
// публикации — рендер читает без блокировки данных).
func (m *Manager) Current() *Frame {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// Name — module.Module.
func (m *Manager) Name() string { return "wallpaper" }

// Run — жизненный цикл модуля: пока контекст жив, поддерживает провайдер
// обоев: запуск → приём кадров → ошибка/паника → backoff → перезапуск.
func (m *Manager) Run(ctx context.Context) error {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		m.mu.RLock()
		src := m.source
		m.mu.RUnlock()

		if src == "" {
			// Обоев нет: ждём SetSource, не крутя холостой цикл.
			if wait(ctx, 200*time.Millisecond) {
				return nil
			}
			attempt = 0
			continue
		}

		if err := m.pump(ctx, src); err != nil {
			m.mu.Lock()
			m.restarts++
			m.mu.Unlock()
			log.Printf("wallpaper: источник %q упал: %v", src, err)
			if wait(ctx, backoff(m.backoffBase, m.backoffMax, attempt)) {
				return nil
			}
			attempt++
			continue
		}
		attempt = 0 // штатное завершение (ctx) — сброс
	}
}

// pump запускает провайдер и перекачивает кадры в current. Паника
// декодера перехватывается и превращается в ошибку модуля.
func (m *Manager) pump(ctx context.Context, src string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("wallpaper: паника провайдера: %v", r)
		}
	}()

	kind := KindOf(src)
	var prov Provider
	switch kind {
	case KindImage:
		prov = &ImageProvider{Path: src}
	case KindGIF:
		prov = &GifProvider{Path: src}
	case KindVideo:
		prov = &VideoProvider{Path: src, Decoder: m.decoder}
	case KindShader:
		prov = &ShaderProvider{}
	default:
		return errUnknownKind
	}

	frames, err := prov.Frames(ctx, m.vw, m.vh)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case f, ok := <-frames:
			if !ok {
				// Источник закончился (например, видео доиграло) —
				// цикл перезапустит его, если провайдер не зациклен.
				return errSourceEnded
			}
			m.mu.Lock()
			m.current = &f
			m.mu.Unlock()
		}
	}
}

var (
	errUnknownKind = errors.New("wallpaper: неизвестный тип обоев")
	errSourceEnded = errors.New("wallpaper: источник кадров закончился")
)

func wait(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(d):
		return false
	}
}

func backoff(base, max time.Duration, attempt int) time.Duration {
	d := base << uint(min(attempt, 20))
	if d <= 0 || d > max {
		d = max
	}
	return d
}

// Заглушки интерфейсов компиляции: Manager удовлетворяет module.Module.
var _ module.Module = (*Manager)(nil)

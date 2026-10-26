// Package session — сохранение и восстановление сессии холста (JSON).
//
// Требование Спринта 1/5: координаты камеры, зум, окна (и в перспективе
// карточки/виджеты) переживают перезапуск композитора. Запись АТОМАРНА
// (tmp + rename): kill -9 в момент автосохранения не должен портить файл.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// CurrentVersion — версия формата снапшота (для будущих миграций).
const CurrentVersion = 1

// CameraSnapshot — сериализуемая камера.
type CameraSnapshot struct {
	X, Y, Zoom float64
}

// WindowSnapshot — сериализуемое окно.
type WindowSnapshot struct {
	ID        uint32  `json:"id"`
	Title     string  `json:"title"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	W         float64 `json:"w"`
	H         float64 `json:"h"`
	Minimized bool    `json:"minimized"`
}

// CardSnapshot — сериализуемая карточка Live Preview.
type CardSnapshot struct {
	ID    string  `json:"id"`
	Path  string  `json:"path"`
	Kind  string  `json:"kind"`
	Title string  `json:"title"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
}

// WidgetSnapshot — сериализуемый HTML5-виджет. Права восстанавливаются
// из белого списка по умолчанию: сессия не может поднять себе права.
type WidgetSnapshot struct {
	ID    string   `json:"id"`
	URL   string   `json:"url"`
	X     float64  `json:"x"`
	Y     float64  `json:"y"`
	W     float64  `json:"w"`
	H     float64  `json:"h"`
	Perms []string `json:"perms"`
}

// Snapshot — полное состояние сессии.
type Snapshot struct {
	Version int              `json:"version"`
	Camera  CameraSnapshot   `json:"camera"`
	Windows []WindowSnapshot `json:"windows"`
	Cards   []CardSnapshot   `json:"cards,omitempty"`
	Widgets []WidgetSnapshot `json:"widgets,omitempty"`
}

// Manager владеет файлом сессии. Потокобезопасен: автосейв-горутина и
// main loop могут звать Save/Load параллельно.
type Manager struct {
	path string
	mu   sync.Mutex
}

// NewManager создаёт менеджер сессии с файлом по пути path.
func NewManager(path string) *Manager {
	return &Manager{path: path}
}

// Save атомарно сохраняет снапшот: сначала во временный файл в том же
// каталоге, затем rename поверх целевого.
func (m *Manager) Save(snap Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap.Version = CurrentVersion
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return fmt.Errorf("session: mkdir: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("session: write: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return fmt.Errorf("session: rename: %w", err)
	}
	return nil
}

// Load читает снапшот. Отсутствие файла — обычная ошибка (первый запуск).
func (m *Manager) Load() (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var snap Snapshot
	data, err := os.ReadFile(m.path)
	if err != nil {
		return snap, err
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		return snap, fmt.Errorf("session: unmarshal: %w", err)
	}
	return snap, nil
}

// Path — путь к файлу сессии (для Control Center).
func (m *Manager) Path() string { return m.path }

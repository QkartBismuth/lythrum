package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// assetsPath — каталог ресурсов от каталога теста (internal/audio → корень).
func assetsPath(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join("..", "..", rel)
}

// craftWAV собирает валидный PCM16 WAV: mono 8kHz, 4 сэмпла.
func craftWAV(samples []int16) []byte {
	dataLen := len(samples) * 2
	buf := &bytes.Buffer{}
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+dataLen))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))
	binary.Write(buf, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(buf, binary.LittleEndian, uint16(1)) // mono
	binary.Write(buf, binary.LittleEndian, uint32(8000))
	binary.Write(buf, binary.LittleEndian, uint32(8000*2))
	binary.Write(buf, binary.LittleEndian, uint16(2))
	binary.Write(buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataLen))
	for _, s := range samples {
		binary.Write(buf, binary.LittleEndian, s)
	}
	return buf.Bytes()
}

// 3.3 — WAV-декодер: поля формата и сэмплы.
func TestDecodeWAV(t *testing.T) {
	clip, err := DecodeWAV(craftWAV([]int16{100, -100, 500, -500}))
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	if clip.Channels != 1 || clip.Rate != 8000 {
		t.Fatalf("формат: %+v", clip)
	}
	if len(clip.Data) != 8 {
		t.Fatalf("данных %d байт, want 8", len(clip.Data))
	}
	if got := binary.LittleEndian.Uint16(clip.Data[0:2]); got != 100 {
		t.Fatalf("сэмпл 0 = %d, want 100", got)
	}
	if clip.Duration() != 0 { // 4 сэмпла @8k = 0.5 мс → целочисленно 0
		t.Fatalf("длительность: %d мс", clip.Duration())
	}
}

// 3.3 — OGG-декодер на реальных ассетах схемы Chrome OS Flex.
func TestDecodeOGGAssets(t *testing.T) {
	for _, name := range []string{"click", "error", "notify", "device", "power"} {
		clip, err := DecodeFile(assetsPath(t, "assets/sounds/"+name+".ogg"))
		if err != nil {
			t.Fatalf("%s.ogg: %v", name, err)
		}
		if clip.Rate != 44100 || clip.Channels != 1 {
			t.Fatalf("%s: rate=%d ch=%d", name, clip.Rate, clip.Channels)
		}
		if len(clip.Data) == 0 {
			t.Fatalf("%s: пустой PCM", name)
		}
	}
}

// 3.3 — отсутствие файла НЕ роняет модуль: очередь живёт, счётчик растёт.
func TestManagerMissingFile(t *testing.T) {
	dir := t.TempDir()
	p := &recorder{}
	m := NewManager(dir, p)

	m.Play("nonexistent")
	m.Play("also_missing")
	m.Play("nonexistent") // повтор — по кэшу missing без обращений к диску

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := m.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if p.count() != 0 {
		t.Fatalf("несуществующие звуки не должны играться: %d", p.count())
	}
}

// Очередь доигрывает звуки через Player.
func TestManagerPlaysScheme(t *testing.T) {
	p := &recorder{}
	m := NewManager(assetsPath(t, "assets/sounds"), p)

	m.Play("click")
	m.Play("notify")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = m.Run(ctx) }() // очередь живёт до таймаута

	// Ждём, пока оба звука будут обслужены (запас под медленный CI).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p.count() >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := p.count(); got != 2 {
		t.Fatalf("проиграно %d звуков, want 2", got)
	}
	if !m.clipCached("click") {
		t.Fatal("клик должен быть закэширован после декода")
	}
}

// SubprocessPlayer: без аудио-сервера ошибка возвращается аккуратно.
func TestSubprocessPlayerGraceful(t *testing.T) {
	clip, err := DecodeFile(assetsPath(t, "assets/sounds/click.ogg"))
	if err != nil {
		t.Skip("ассеты недоступны")
	}
	p := SubprocessPlayer{Kind: AutoPlayer}
	if err := p.Play(clip); err != nil {
		t.Logf("аудио-стек недоступен (ожидаемо в CI): %v", err)
	}
}

// recorder — потокобезопасный тестовый Player.
type recorder struct {
	mu    sync.Mutex
	plays int
}

func (r *recorder) Play(c Clip) error {
	r.mu.Lock()
	r.plays++
	r.mu.Unlock()
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.plays
}

// clipCached — доступ к кэшу для теста.
func (m *AudioManager) clipCached(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.cache[name]
	return ok
}

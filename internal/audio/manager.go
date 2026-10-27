package audio

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"lythrum/internal/module"
)

// Player — изолированный вывод звука.
type Player interface {
	// Play проигрывает клип; вызывается из очереди менеджера.
	Play(c Clip) error
}

// ── SubprocessPlayer: вывод через внешний плеер ─────────────────────────

// SubprocessKind — бэкенд вывода.
type SubprocessKind string

const (
	AutoPlayer  SubprocessKind = "auto" // paplay → pw-play → aplay
	PulsePlayer SubprocessKind = "paplay"
	PipePlayer  SubprocessKind = "pw-play"
	ALSAPlayer  SubprocessKind = "aplay"
)

// SubprocessPlayer играет PCM через отдельный процесс — крах аудио-стека
// системы не затрагивает композитор.
type SubprocessPlayer struct {
	Kind SubprocessKind
}

// Play реализует Player. Ошибка процесса — возвращается, не паникует.
func (p SubprocessPlayer) Play(c Clip) error {
	kind := p.Kind
	if kind == AutoPlayer {
		kind = detectPlayer()
	}

	var cmd *exec.Cmd
	switch kind {
	case PulsePlayer:
		cmd = exec.Command("paplay", "--raw", "--format=s16le",
			"--rate="+strconv.Itoa(c.Rate), "--channels="+strconv.Itoa(c.Channels), "-")
	case PipePlayer:
		cmd = exec.Command("pw-play", "--format=s16", "--rate="+strconv.Itoa(c.Rate),
			"--channels="+strconv.Itoa(c.Channels), "-")
	case ALSAPlayer:
		cmd = exec.Command("aplay", "-q", "-f", "S16_LE", "-r", strconv.Itoa(c.Rate),
			"-c", strconv.Itoa(c.Channels), "-t", "raw", "-")
	default:
		return errNoPlayer
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Запись с таймаутом: зависший аудио-сервер не блокирует очередь.
	done := make(chan error, 1)
	go func() {
		_, err := stdin.Write(c.Data)
		stdin.Close()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			cmd.Process.Kill()
			cmd.Wait()
			return err
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		return errPlayerTimeout
	}
	return cmd.Wait()
}

func detectPlayer() SubprocessKind {
	for _, k := range []SubprocessKind{PulsePlayer, PipePlayer, ALSAPlayer} {
		if _, err := exec.LookPath(string(k)); err == nil {
			return k
		}
	}
	return ""
}

// NullPlayer — заглушка для тестов и беззвучного режима.
type NullPlayer struct{}

func (NullPlayer) Play(Clip) error { return nil }

// ── AudioManager ─────────────────────────────────────────────────────────

// AudioManager — модуль системных звуков: очередь воспроизведения + кэш
// декодированных клипов.
//
// Ошибки (нет файла, нет аудио-сервера) изолируются: лог + счётчик,
// композитор продолжает работу.
type AudioManager struct {
	dir    string // каталог звуковой схемы
	player Player

	mu      sync.RWMutex
	cache   map[string]Clip
	missing map[string]bool // проверенные и не найденные (не спамим диск)
	drops   int             // неудачные воспроизведения (метрика)
	volume  int             // 0–100, громкость схемы
	muted   bool            // mute: аттенюация в ноль, очередь живёт

	queue chan string
}

// NewManager создаёт менеджер схемы из dir; player == nil → NullPlayer
// (беззвучный режим, например в CI).
func NewManager(dir string, player Player) *AudioManager {
	if player == nil {
		player = NullPlayer{}
	}
	return &AudioManager{
		dir:     dir,
		player:  player,
		cache:   make(map[string]Clip),
		missing: make(map[string]bool),
		volume:  75,
		queue:   make(chan string, 32),
	}
}

// ── Громкость (Спринт 6): горячие EvVolume/EvVolumeMute из main loop ──

// SetVolume задаёт громкость схемы 0–100 (вне диапазона — зажим).
func (m *AudioManager) SetVolume(v int) {
	m.mu.Lock()
	m.volume = clampVolume(v)
	m.mu.Unlock()
}

// Volume — текущая громкость 0–100.
func (m *AudioManager) Volume() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.volume
}

// SetMuted включает/выключает mute. Очередь не очищается: «вернуть звук»
// продолжает с того места — как в Chrome OS.
func (m *AudioManager) SetMuted(muted bool) {
	m.mu.Lock()
	m.muted = muted
	m.mu.Unlock()
}

// ToggleMute инвертирует mute, возвращает новое состояние.
func (m *AudioManager) ToggleMute() bool {
	m.mu.Lock()
	m.muted = !m.muted
	m.mu.Unlock()
	return m.muted
}

// Muted — состояние mute.
func (m *AudioManager) Muted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.muted
}

func clampVolume(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// Play ставит звук в очередь; неблокирующе (ввод не должен ждать аудио).
// name — имя схемы: click, error, notify, device, power.
func (m *AudioManager) Play(name string) {
	select {
	case m.queue <- name:
	default:
		m.mu.Lock()
		m.drops++
		m.mu.Unlock()
		log.Printf("audio: очередь переполнена, %q сброшен", name)
	}
}

// Drops — счётчик сброшенных звуков (Control Center).
func (m *AudioManager) Drops() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.drops
}

// Name — module.Module.
func (m *AudioManager) Name() string { return "audio" }

// Run обслуживает очередь воспроизведения до отмены контекста.
func (m *AudioManager) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case name := <-m.queue:
			m.playNow(name)
		}
	}
}

func (m *AudioManager) playNow(name string) {
	clip, err := m.clip(name)
	if err != nil {
		log.Printf("audio: %v", err)
		return
	}
	// Аттенюация ПОСЛЕ кэша: оригинал в cache не трогаем, плееру уходит
	// копия с коэффициентом. Mute — честная тишина (плеер получает нули).
	if f := m.gain(); f != 1.0 {
		clip = attenuate(clip, f)
	}
	if err := m.player.Play(clip); err != nil {
		m.mu.Lock()
		m.drops++
		m.mu.Unlock()
		log.Printf("audio: воспроизведение %q: %v", name, err)
	}
}

// gain — коэффициент амплитуды по громкости/mute (под mu).
func (m *AudioManager) gain() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.muted || m.volume == 0 {
		return 0
	}
	return float64(m.volume) / 100
}

// attenuate масштабирует s16le-сэмплы клипа, возвращая НОВЫЙ буфер.
// Кэш менеджера остаётся нетронутым — клипы переиспользуются.
func attenuate(c Clip, factor float64) Clip {
	out := Clip{Channels: c.Channels, Rate: c.Rate, Data: make([]byte, len(c.Data))}
	for i := 0; i+1 < len(c.Data); i += 2 {
		s := int16(uint16(c.Data[i]) | uint16(c.Data[i+1])<<8)
		v := int(float64(s) * factor)
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		out.Data[i] = byte(int16(v))
		out.Data[i+1] = byte(int16(v) >> 8)
	}
	return out
}

// clip декодирует (и кэширует) звуковой файл схемы.
func (m *AudioManager) clip(name string) (Clip, error) {
	m.mu.RLock()
	if c, ok := m.cache[name]; ok {
		m.mu.RUnlock()
		return c, nil
	}
	if m.missing[name] {
		m.mu.RUnlock()
		return Clip{}, errSoundNotFound
	}
	m.mu.RUnlock()

	// OGG приоритетнее (схема Chrome OS Flex); .wav — fallback.
	var lastErr error
	for _, ext := range []string{".ogg", ".wav"} {
		c, err := DecodeFile(filepath.Join(m.dir, name+ext))
		if err == nil {
			m.mu.Lock()
			m.cache[name] = c
			m.mu.Unlock()
			return c, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			lastErr = err // формата нет — пробуем следующий
			continue
		}
		return Clip{}, err // файл найден, но битый — не тянем дальше
	}

	m.mu.Lock()
	m.missing[name] = true
	m.mu.Unlock()
	if lastErr == nil {
		lastErr = errSoundNotFound
	}
	return Clip{}, lastErr
}

var (
	errSoundNotFound = errors.New("audio: звук не найден в схеме")
	errNoPlayer      = errors.New("audio: аудио-плеер не найден в системе")
	errPlayerTimeout = errors.New("audio: таймаут воспроизведения")
)

// Компиляционная проверка: AudioManager — модуль.
var _ module.Module = (*AudioManager)(nil)

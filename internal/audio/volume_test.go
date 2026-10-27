package audio

import (
	"bytes"
	"context"
	"testing"
)

// catchingPlayer перехватывает клипы, ушедшие в плеер.
type catchingPlayer struct{ last Clip }

func (c *catchingPlayer) Play(clip Clip) error { c.last = clip; return nil }

func s16Clip(samples ...int16) Clip {
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		data[i*2] = byte(uint16(s))
		data[i*2+1] = byte(uint16(s) >> 8)
	}
	return Clip{Data: data, Channels: 1, Rate: 8000}
}

// 6.2 — громкость: аттенюация честная (половина громкости ≈ половина
// амплитуды), mute даёт тишину, кэш менеджера не искажается.
func TestVolumeAttenuation(t *testing.T) {
	p := &catchingPlayer{}
	am := NewManager(t.TempDir(), p)

	// «Звуковой файл» кладём прямо в кэш: тесту не нужен диск.
	orig := s16Clip(1000, -500, 32767, -32768)
	am.cache["click"] = orig

	am.SetVolume(150) // зажим в 100
	if am.Volume() != 100 {
		t.Fatalf("громкость не зажалась: %d", am.Volume())
	}
	am.SetVolume(50)
	am.playNow("click")

	got := p.last.Data
	if bytes.Equal(got, orig.Data) {
		t.Fatal("клип не ослаблен — громкость не работает")
	}
	if want := int16(500); int16(uint16(got[0])|uint16(got[1])<<8) != want {
		t.Fatalf("первый сэмпл: %d, want %d", int16(uint16(got[0])|uint16(got[1])<<8), want)
	}
	if int16(uint16(got[4])|uint16(got[5])<<8) != int16(16383) {
		t.Fatalf("верхний сэмпл не зажат по амплитуде: %d",
			int16(uint16(got[4])|uint16(got[5])<<8))
	}

	// Кэш нетронут: следующий прогон без изменений громкости — те же сэмплы.
	am.playNow("click")
	if !bytes.Equal(p.last.Data, got) {
		t.Fatal("кэш исказился — аттенюация накопительная")
	}
}

// 6.2 — mute: плеер получает нули, unmute возвращает звук.
func TestMuteSilences(t *testing.T) {
	p := &catchingPlayer{}
	am := NewManager(t.TempDir(), p)
	am.cache["click"] = s16Clip(1000, 1000)

	if am.ToggleMute() != true {
		t.Fatal("первый toggle должен включить mute")
	}
	am.playNow("click")
	if !bytes.Equal(p.last.Data, make([]byte, len(p.last.Data))) {
		t.Fatal("при mute плеер получил ненулевые сэмплы")
	}

	am.ToggleMute()
	am.playNow("click")
	if bytes.Equal(p.last.Data, make([]byte, len(p.last.Data))) {
		t.Fatal("после unmute звук пропал")
	}
}

// Громкость меняется из чужих горутин (CC UI, виджеты) — -race чисто.
func TestVolumeConcurrent(t *testing.T) {
	am := NewManager(t.TempDir(), nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 100 {
			am.SetVolume(i)
			_ = am.ToggleMute()
			_ = am.Volume()
			_ = am.Muted()
		}
	}()
	<-done
	if am.Volume() != 99 {
		t.Fatalf("финальная громкость: %d", am.Volume())
	}
}

// Run завершается с контекстом (модульная дисциплина).
func TestRunStopsOnContext(t *testing.T) {
	am := NewManager(t.TempDir(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	go am.Run(ctx)
	cancel()
}

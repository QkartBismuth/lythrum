// Package audio — звуковая подсистема Lythrum: системные звуки в духе
// Chrome OS Flex (короткие OGG/WAV) и изолированный вывод.
//
// Отказоустойчивость: вывод через ОТДЕЛЬНЫЙ ПРОЦЕСС (paplay/pw-play/aplay)
// — тот же паттерн изоляции, что и у HTML5-виджетов: крах аудио-сервера
// не роняет композитор. Отсутствие файла/устройства — лог, не паника.
package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jfreymuth/oggvorbis"
)

// Clip — декодированный PCM-звук: interleaved S16LE.
type Clip struct {
	Data     []byte
	Channels int
	Rate     int
}

// Duration — длительность клипа.
func (c Clip) Duration() int64 {
	if c.Channels == 0 || c.Rate == 0 {
		return 0
	}
	samples := int64(len(c.Data)/2) / int64(c.Channels)
	return samples * 1000 / int64(c.Rate) // мс
}

// ── Декодеры ─────────────────────────────────────────────────────────────

// DecodeWAV разбирает RIFF/WAVE с PCM 16-bit (формат 1).
func DecodeWAV(data []byte) (Clip, error) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return Clip{}, fmt.Errorf("audio: это не RIFF/WAVE")
	}

	var clip Clip
	pos := 12
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		body := data[pos+8:]
		if pos+8+size > len(data) {
			return Clip{}, fmt.Errorf("audio: обрезанный чанк %q", id)
		}

		switch id {
		case "fmt ":
			format := binary.LittleEndian.Uint16(body[0:2])
			clip.Channels = int(binary.LittleEndian.Uint16(body[2:4]))
			clip.Rate = int(binary.LittleEndian.Uint32(body[4:8]))
			bits := int(binary.LittleEndian.Uint16(body[14:16]))
			if format != 1 || bits != 16 {
				return Clip{}, fmt.Errorf("audio: WAV только PCM 16-bit (format=%d bits=%d)", format, bits)
			}
		case "data":
			clip.Data = append([]byte{}, body[:size]...)
		}
		pos += 8 + size + (size & 1) // чанк выравнивается по 2 байтам
	}
	if clip.Data == nil || clip.Channels == 0 || clip.Rate == 0 {
		return Clip{}, fmt.Errorf("audio: WAV без fmt/data")
	}
	return clip, nil
}

// DecodeOGG разбирает Vorbis в контейнере OGG и приводит к S16LE.
func DecodeOGG(data []byte) (Clip, error) {
	r, err := oggvorbis.NewReader(bytesReader(data))
	if err != nil {
		return Clip{}, fmt.Errorf("audio: ogg: %w", err)
	}
	clip := Clip{
		Channels: r.Channels(),
		Rate:     r.SampleRate(),
	}
	floats := make([]float32, 4096)
	var out []byte
	for {
		n, err := r.Read(floats)
		if n > 0 {
			out = append(out, f32ToS16LE(floats[:n])...)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return Clip{}, fmt.Errorf("audio: ogg read: %w", err)
		}
	}
	clip.Data = out
	return clip, nil
}

// DecodeFile декодирует файл по расширению.
func DecodeFile(path string) (Clip, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Clip{}, err
	}
	switch ext := filepath.Ext(path); ext {
	case ".wav":
		return DecodeWAV(data)
	case ".ogg", ".oga":
		return DecodeOGG(data)
	default:
		return Clip{}, fmt.Errorf("audio: неподдерживаемый формат %q", ext)
	}
}

func f32ToS16LE(f []float32) []byte {
	out := make([]byte, 2*len(f))
	for i, v := range f {
		if v > 1 {
			v = 1
		}
		if v < -1 {
			v = -1
		}
		binary.LittleEndian.PutUint16(out[2*i:2*i+2], uint16(int16(v*32767)))
	}
	return out
}

func bytesReader(b []byte) io.Reader { return &byteR{b: b} }

type byteR struct {
	b []byte
	i int
}

func (r *byteR) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

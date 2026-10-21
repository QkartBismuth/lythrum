package main

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// The sounds of the shell are synthesised: a style describes the tones, the
// shell writes a small wave file and hands it to the sound system. No sound
// files have to be shipped with the shell.
var (
	soundMu   sync.Mutex
	soundPath string
)

// synthWAV builds a wave file with one tone. The tone fades in and out, so it
// does not click.
func synthWAV(freq int, ms int, wave string, volume float64) []byte {
	const rate = 44100
	n := rate * ms / 1000
	if n < 1 {
		return nil
	}
	if volume <= 0 {
		return nil
	}
	if volume > 1 {
		volume = 1
	}
	buf := make([]byte, 0, 44+n*2)
	// The header of a wave file.
	buf = append(buf, "RIFF"...)
	buf = appendU32(buf, uint32(36+n*2))
	buf = append(buf, "WAVEfmt "...)
	buf = appendU32(buf, 16)
	buf = appendU16(buf, 1) // the format is PCM
	buf = appendU16(buf, 1) // one channel
	buf = appendU32(buf, rate)
	buf = appendU32(buf, rate*2)
	buf = appendU16(buf, 2)
	buf = appendU16(buf, 16)
	buf = append(buf, "data"...)
	buf = appendU32(buf, uint32(n*2))
	for i := 0; i < n; i++ {
		t := float64(i) / rate
		// A short fade, so the tone does not click at the edges.
		env := 1.0
		fade := rate / 40
		if i < fade {
			env = float64(i) / float64(fade)
		} else if i > n-fade {
			env = float64(n-i) / float64(fade)
		}
		var s float64
		switch wave {
		case "square":
			if math.Mod(t*float64(freq), 1) < 0.5 {
				s = 1
			} else {
				s = -1
			}
		case "triangle":
			ph := math.Mod(t*float64(freq), 1)
			s = 4*ph - 1
			if s < 0 {
				s = -s
			}
			s = 1 - 2*s
		default:
			s = math.Sin(2 * math.Pi * float64(freq) * t)
		}
		v := int16(s * env * volume * 26000)
		buf = appendU16(buf, uint16(v))
	}
	return buf
}

func appendU32(b []byte, v uint32) []byte {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], v)
	return append(b, tmp[:]...)
}

func appendU16(b []byte, v uint16) []byte {
	var tmp [2]byte
	binary.LittleEndian.PutUint16(tmp[:], v)
	return append(b, tmp[:]...)
}

// playSound plays one of the sounds of the active style.
func (v *view) playSound(kind string) {
	if v == nil || v.cfg.Sound.Muted {
		return
	}
	freq := 0
	switch kind {
	case "click":
		freq = styleSound.Click
	case "drop":
		freq = styleSound.Drop
	case "open":
		freq = styleSound.Open
	case "close":
		freq = styleSound.Close
	}
	if freq <= 0 {
		return
	}
	vol := styleSound.Volume * float64(v.cfg.Sound.Volume) / 100
	data := synthWAV(freq, 90, styleSound.Wave, vol)
	if len(data) == 0 {
		return
	}
	go func() {
		path, err := v.writeSound(data)
		if err != nil {
			return
		}
		playSoundFile(path)
	}()
}

// writeSound stores the wave file of a tone and returns its path.
func (v *view) writeSound(data []byte) (string, error) {
	soundMu.Lock()
	defer soundMu.Unlock()
	if soundPath == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			dir = os.TempDir()
		}
		dir = filepath.Join(dir, "canvas")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		soundPath = filepath.Join(dir, "tone.wav")
	}
	if err := os.WriteFile(soundPath, data, 0o644); err != nil {
		return "", err
	}
	return soundPath, nil
}

// playSoundFile hands a wave file to the sound system.
func playSoundFile(path string) {
	for _, c := range [][]string{
		{"paplay", path},
		{"aplay", "-q", path},
		{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", path},
	} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		if err := exec.Command(c[0], c[1:]...).Run(); err == nil {
			return
		}
	}
}

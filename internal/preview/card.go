package preview

import (
	"bytes"
	"errors"
	"image"
	"image/gif"
	_ "image/jpeg" // регистрация декодеров для image.Decode
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"lythrum/internal/core"
)

var errUnknownMedia = errors.New("preview: неподдерживаемый тип медиа")

// MediaKind — тип медиа карточки.
type MediaKind string

const (
	KindImage   MediaKind = "image"
	KindGIF     MediaKind = "gif"
	KindVideo   MediaKind = "video"
	KindAudio   MediaKind = "audio"
	KindUnknown MediaKind = "unknown"
)

// Card — карточка файла на холсте.
type Card struct {
	ID    string    `json:"id"`
	Path  string    `json:"path"`
	Kind  MediaKind `json:"kind"`
	Title string    `json:"title"`
	Rect  core.Rect `json:"rect"` // глобальные координаты холста

	// Poster — статичный кадр предпросмотра (первый кадр GIF, кадр видео,
	// сама картинка). Для audio — Album Art или nil (рисуется заглушка).
	Poster image.Image `json:"-"`
}

// KindOf — классификация по расширению (тест 3.4).
func KindOf(path string) MediaKind {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".gif":
		return KindGIF
	case ".png", ".jpg", ".jpeg", ".webp", ".bmp":
		return KindImage
	case ".mp4", ".mkv", ".webm", ".mov", ".avi":
		return KindVideo
	case ".mp3", ".ogg", ".oga", ".wav", ".flac", ".opus":
		return KindAudio
	default:
		return KindUnknown
	}
}

// OpenCard строит карточку файла с постером. Для видео постер извлекается
// ffmpeg'ом (первый кадр); при отсутствии ffmpeg постер остаётся nil —
// карточка рисуется обложкой-заглушкой, это не ошибка.
func OpenCard(path string, rect core.Rect) (*Card, error) {
	kind := KindOf(path)
	if kind == KindUnknown {
		return nil, errUnknownMedia
	}

	card := &Card{
		ID:    core.NewID(),
		Path:  path,
		Kind:  kind,
		Title: filepath.Base(path),
		Rect:  rect,
	}

	var err error
	switch kind {
	case KindImage:
		card.Poster, err = loadImage(path)
	case KindGIF:
		card.Poster, err = firstGIFFrame(path)
	case KindVideo:
		card.Poster, err = firstVideoFrame(path)
	case KindAudio:
		card.Poster, err = nil, nil // Album Art — Спринт 4 (теги файлов)
	}
	if err != nil {
		return nil, err
	}
	return card, nil
}

// PosterSize — размер постера, вписанный в карточку с сохранением пропорций.
func (c *Card) PosterSize() (w, h float64) {
	if c.Poster == nil || c.Poster.Bounds().Dx() == 0 {
		return c.Rect.W, c.Rect.H
	}
	b := c.Poster.Bounds()
	scale := min(c.Rect.W/float64(b.Dx()), c.Rect.H/float64(b.Dy()))
	return float64(b.Dx()) * scale, float64(b.Dy()) * scale
}

func loadImage(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func firstGIFFrame(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return gif.Decode(f) // первый кадр
}

// firstVideoFrame извлекает первый кадр видео (PNG-потом из ffmpeg).
// Отсутствие ffmpeg — постера нет, карточка рисуется заглушкой.
func firstVideoFrame(path string) (image.Image, error) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, nil
	}
	cmd := exec.Command(bin, "-v", "error", "-i", path,
		"-frames:v", "1", "-f", "image2", "-vcodec", "png", "-")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, nil // декодировать превью не вышло — это не фатально
	}
	img, _, err := image.Decode(&out)
	if err != nil {
		return nil, nil
	}
	return img, nil
}

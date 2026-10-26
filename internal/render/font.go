// Текстовая подсистема Lythrum: загрузка TTF/OTF (Geologica, Material Symbols)
// через golang.org/x/image/font/opentype — без C-freetype.
//
// Системный шрифт интерфейсов (меню, поиск, оверлеи) — Geologica;
// иконки — Material Symbols (см. icons.go и IconGlyph).
package render

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// AssetsDir — каталог ресурсов; переопределяется переменной LYTHRUM_ASSETS
// (для интеграционных тестов и нестандартных установок).
func AssetsDir() string {
	if p := os.Getenv("LYTHRUM_ASSETS"); p != "" {
		return p
	}
	return "assets"
}

// DefaultFontPath — файл системного шрифта Geologica.
func DefaultFontPath() string {
	return filepath.Join(AssetsDir(), "fonts", "geologica.ttf")
}

// IconFontPath — файл иконочного шрифта Material Symbols.
func IconFontPath() string {
	return filepath.Join(AssetsDir(), "fonts", "materialsymbols.ttf")
}

// Face — загруженный шрифт на конкретном размере (в пикселях экрана).
//
// Вариативные шрифты (Geologica, Material Symbols — оба variable) рендерятся
// в инстансе по умолчанию (Regular 400); осями wght/FILL управим в Спринте 3,
// когда будет растеризатор-атлас оверлей-слоя.
//
// Потокобезопасность: face не потокобезопасен — создаются отдельные Face
// на goroutine рендера либо замер делается до запуска main loop.
type Face struct {
	ft   *opentype.Font
	size float64
	face font.Face
}

// LoadFontFile загружает шрифт из файла и создаёт face размера size.
func LoadFontFile(path string, size float64) (*Face, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("font: read %s: %w", path, err)
	}
	return LoadFontBytes(data, size)
}

// LoadFontBytes разбирает TTF/OTF из памяти.
func LoadFontBytes(data []byte, size float64) (*Face, error) {
	ft, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("font: parse: %w", err)
	}
	face, err := opentype.NewFace(ft, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("font: face: %w", err)
	}
	return &Face{ft: ft, size: size, face: face}, nil
}

// Measure возвращает ширину строки и высоту (Ascent+Descent) в пикселях.
func (f *Face) Measure(s string) (w, h float64) {
	adv := font.MeasureString(f.face, s)
	m := f.face.Metrics()
	return fixedToPx(adv), fixedToPx(m.Ascent + m.Descent)
}

// HasGlyph проверяет наличие глифа в шрифте (валидация иконок и текста).
func (f *Face) HasGlyph(r rune) bool {
	_, _, _, _, ok := f.face.Glyph(fixed.Point26_6{}, r)
	return ok
}

// Size — размер face в пикселях (для отладки и атласов).
func (f *Face) Size() float64 { return f.size }

func fixedToPx(v fixed.Int26_6) float64 { return float64(v) / 64 }

// fontHealthWatchdog — план Спринта 5: деградация при недоступных шрифтах
// (fallback на встроенный bitmap-шрифт) остаётся за оверлей-слоём; ядро
// обязано подниматься даже без assets, поэтому ошибки загрузки шрифтов
// логируются, но не останавливают композитор. Загрузку выполняем лениво
// с таймаутом, чтобы битый файл не подвесил старт.
var fontLoadTimeout = 5 * time.Second

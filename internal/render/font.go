// Текстовая подсистема Lythrum: загрузка TTF/OTF (Geologica, Material Symbols)
// через golang.org/x/image/font/opentype — без C-freetype.
//
// Системный шрифт интерфейсов (меню, поиск, оверлеи) — Geologica;
// иконки — Material Symbols (см. icons.go и IconGlyph).
package render

import (
	"fmt"
	"image"
	"image/color"
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

// DrawText рисует строку s в dst с левым верхним углом в (x, y) цветом c
// (обычная over-композиция, не-premultiplied источник). Возвращает
// продвинутую ширину строки в пикселях. Face НЕ потокобезопасен —
// вызывается из main loop (владельца кадра).
func (f *Face) DrawText(dst *image.RGBA, x, y float64, s string, c color.RGBA) float64 {
	if s == "" {
		return 0
	}
	ascent := f.face.Metrics().Ascent
	start := fixed.Int26_6(x * 64)
	dot := fixed.Point26_6{X: start, Y: fixed.Int26_6(y*64) + ascent}
	var prev rune = -1
	for _, r := range s {
		if prev >= 0 {
			dot.X += f.face.Kern(prev, r)
		}
		dr, mask, maskp, adv, ok := f.face.Glyph(dot, r)
		if ok {
			blitGlyph(dst, dr, mask, maskp, c)
		}
		dot.X += adv
		prev = r
	}
	return fixedToPx(dot.X - start)
}

// DrawTextClip — DrawText с обрезкой по правому краю: глифы, выходящие
// за maxRight, не рисуются (заголовки окон и меню). Возвращает ширину
// нарисованной части.
func (f *Face) DrawTextClip(dst *image.RGBA, x, y float64, s string, c color.RGBA, maxRight float64) float64 {
	ascent := f.face.Metrics().Ascent
	start := fixed.Int26_6(x * 64)
	dot := fixed.Point26_6{X: start, Y: fixed.Int26_6(y*64) + ascent}
	var prev rune = -1
	for _, r := range s {
		if prev >= 0 {
			dot.X += f.face.Kern(prev, r)
		}
		dr, mask, maskp, adv, ok := f.face.Glyph(dot, r)
		if ok && float64(dr.Max.X) > maxRight {
			break // следующий глиф не влезает — обрезка
		}
		if ok {
			blitGlyph(dst, dr, mask, maskp, c)
		}
		dot.X += adv
		prev = r
	}
	return fixedToPx(dot.X - start)
}

// blitGlyph — закраска маски глифа цветом c в dst (over-композиция).
func blitGlyph(dst *image.RGBA, dr image.Rectangle, mask image.Image, maskp image.Point, c color.RGBA) {
	for py := dr.Min.Y; py < dr.Max.Y; py++ {
		for px := dr.Min.X; px < dr.Max.X; px++ {
			ma := alphaAt(mask, px-dr.Min.X+maskp.X, py-dr.Min.Y+maskp.Y)
			if ma == 0 {
				continue
			}
			i := dst.PixOffset(px, py)
			sa := uint32(ma) // 0..255
			for ch := 0; ch < 4; ch++ {
				var src uint8
				switch ch {
				case 0:
					src = c.R
				case 1:
					src = c.G
				case 2:
					src = c.B
				case 3:
					src = c.A
				}
				d := uint32(dst.Pix[i+ch])
				dst.Pix[i+ch] = uint8((uint32(src)*sa + d*(255-sa)) / 255)
			}
		}
	}
}

// alphaAt читает альфу из маски глифа (быстрый путь image.Alpha).
func alphaAt(mask image.Image, x, y int) uint8 {
	if a, ok := mask.(*image.Alpha); ok {
		return a.AlphaAt(x, y).A
	}
	r, g, b, _ := mask.At(x, y).RGBA()
	return uint8((r + g + b) >> 16) // светимость как альфа (запасной путь)
}

// fontHealthWatchdog — план Спринта 5: деградация при недоступных шрифтах
// (fallback на встроенный bitmap-шрифт) остаётся за оверлей-слоём; ядро
// обязано подниматься даже без assets, поэтому ошибки загрузки шрифтов
// логируются, но не останавливают композитор. Загрузку выполняем лениво
// с таймаутом, чтобы битый файл не подвесил старт.
var fontLoadTimeout = 5 * time.Second

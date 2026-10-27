package render

import (
	"image"
	"testing"

	"lythrum/internal/core"
	"lythrum/internal/palette"
	"lythrum/internal/ui"
)

// paintFixture — кадр с окном и виджетом, опционально со шрифтом.
func paintFixture(t *testing.T, withFont bool) (FrameInput, *Face) {
	t.Helper()
	var face *Face
	if withFont {
		f, err := LoadFontFile(assetPath(t, DefaultFontPath()), 13)
		if err != nil {
			t.Fatalf("шрифт: %v", err)
		}
		face = f
	}
	in := FrameInput{Cam: core.NewCamera(), ViewportW: 800, ViewportH: 600}
	in.Windows = []WindowFrame{
		{Title: "Терминал", Rect: core.Rect{X: 100, Y: 100, W: 300, H: 200}, Focused: true},
		{Title: "Музыка", Rect: core.Rect{X: 500, Y: 100, W: 300, H: 200}},
		{Title: "Свёрнуто", Rect: core.Rect{X: 100, Y: 400, W: 300, H: 100}, Minimized: true},
	}
	in.Widgets = []WindowFrame{
		{Title: "виджет · часы.html", Rect: core.Rect{X: 100, Y: 350, W: 160, H: 120}},
	}
	in.Cards = []CardFrame{
		{Title: "clip.mp4", Rect: core.Rect{X: 400, Y: 350, W: 160, H: 120}},
	}
	return in, face
}

// Фон кадра = палитра; окна рисуются поверх (пиксель внутри окна — не фон).
func TestPaintBackgroundAndWindows(t *testing.T) {
	in, face := paintFixture(t, false)
	img := Paint(in, face)

	bg := paletteToRGBA(palette.CanvasBackground)
	if got := img.RGBAAt(5, 5); got != bg {
		t.Fatalf("фон: %v, want %v", got, bg)
	}
	// Центр первого окна — подложка MenuBackground.
	if got := img.RGBAAt(250, 200); got == bg {
		t.Fatal("окно не нарисовано (центр окна = фон)")
	}
	// Рамка активного окна (левая кромка) — цвет активной рамки.
	border := paletteToRGBA(palette.WindowBorderActive)
	if got := img.RGBAAt(100, 200); got != border {
		t.Fatalf("рамка активного окна: %v, want %v", got, border)
	}
}

// Свёрнутые окна не рисуются.
func TestPaintSkipsMinimized(t *testing.T) {
	in, _ := paintFixture(t, false)
	img := Paint(in, nil)
	bg := paletteToRGBA(palette.CanvasBackground)
	// (350, 450) — внутри свёрнутого окна (100..400 × 400..500), но вне
	// виджета (100..260 × 350..470).
	if got := img.RGBAAt(350, 450); got != bg {
		t.Fatalf("свёрнутое окно нарисовано: %v", got)
	}
}

// Заголовок рисуется шрифтом: в строке заголовка появляются светлые пиксели.
func TestPaintTitleText(t *testing.T) {
	in, face := paintFixture(t, true)
	img := Paint(in, face)

	// Полоса заголовка (OverlayBackground — тёмная): ищем пиксели,
	// заметно светлее её (текст WindowTitleText #F5F6FA).
	found := 0
	for y := 100; y < 124; y++ {
		for x := 104; x < 396; x++ {
			c := img.RGBAAt(x, y)
			if c.R > 0x80 && c.G > 0x80 && c.B > 0x80 {
				found++
			}
		}
	}
	if found == 0 {
		t.Fatal("текст заголовка не нарисован")
	}
}

// Снап-направляющие рисуются цветом SnapGuideLine.
func TestPaintGuides(t *testing.T) {
	in, _ := paintFixture(t, false)
	in.Guides = []core.Guide{{Vertical: true, At: 250}, {Vertical: false, At: 300}}
	img := Paint(in, nil)

	line := paletteToRGBA(palette.SnapGuideLine)
	if got := img.RGBAAt(250, 50); got != line {
		t.Fatalf("вертикальная направляющая: %v, want %v", got, line)
	}
	if got := img.RGBAAt(50, 300); got != line {
		t.Fatalf("горизонтальная направляющая: %v, want %v", got, line)
	}
}

// Постер карточки масштабируется в её прямоугольник.
func TestPaintCardPoster(t *testing.T) {
	in, _ := paintFixture(t, false)
	poster := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range poster.Pix {
		poster.Pix[i] = 0xFF // непрозрачный белый
	}
	in.Cards = []CardFrame{{Title: "clip.mp4", Rect: core.Rect{X: 400, Y: 350, W: 80, H: 60}, Poster: poster}}
	img := Paint(in, nil)

	// Центр карточки — белый (масштабированный постер).
	c := img.RGBAAt(440, 380)
	if c.R != 0xFF || c.G != 0xFF || c.B != 0xFF {
		t.Fatalf("постер не нарисован: %v", c)
	}
}

// Кадр без окон — чистый фон с сеткой (не паникуем на пустом холсте).
func TestPaintEmpty(t *testing.T) {
	in := FrameInput{Cam: core.NewCamera(), ViewportW: 320, ViewportH: 200}
	img := Paint(in, nil)
	if img.Bounds() != image.Rect(0, 0, 320, 200) {
		t.Fatalf("размер кадра: %v", img.Bounds())
	}
}

// Оверлей рисуется поверх кадра в экранных пикселях; hover подсвечен.
func TestPaintOverlay(t *testing.T) {
	m := ui.NewMenu([]ui.MenuItem{
		{Label: "Пункт раз"},
		{Label: "Пункт два"},
	})
	m.Open(100, 100, 800, 600)
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	PaintOverlay(img, m, nil) // без шрифта — не паникуем

	// Фон меню в области ВТОРОГО пункта (первый — hover, подсвечен).
	if got := img.RGBAAt(110, 100+ui.MenuPadding+ui.ItemHeight+16); got != paletteToRGBA(palette.MenuBackground) {
		t.Fatalf("фон меню: %v", got)
	}
	// Hover-строка подсвечена.
	if got := img.RGBAAt(110, 100+ui.MenuPadding+16); got != paletteToRGBA(palette.MenuHoverItem) {
		t.Fatalf("hover не подсвечен: %v", got)
	}
}

// Зум камеры: окно в 2x занимает вдвое больше экрана.
func TestPaintZoomScales(t *testing.T) {
	in, _ := paintFixture(t, false)
	in.Cam = core.Camera{X: 0, Y: 0, Zoom: 2}
	img := Paint(in, nil)

	bg := paletteToRGBA(palette.CanvasBackground)
	// Окно 100..400 холста → 200..800 экрана при зуме 2: точка (300, 300)
	// внутри; точка (150, 300) — фон (левее окна).
	if got := img.RGBAAt(150, 300); got != bg {
		t.Fatalf("ожидается фон левее окна: %v", got)
	}
	if got := img.RGBAAt(300, 300); got == bg {
		t.Fatal("окно не масштабировано зумом")
	}
}

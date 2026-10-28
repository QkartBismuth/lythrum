// Программная растеризация кадра холста (Спринт 6).
//
// Paint — чистая функция снимка состояния: рисует видимую часть холста в
// RGBA-буфер. Используется для скриншотов (EvScreenshot) и как эталонный
// вывод headless-режима; wayland-вывод поверх тех же данных состояния
// подключается отдельно. Пакет НЕ импортирует canvas (он сам — зависимость
// холста): ядро передаёт плоские снимки FrameInput.
package render

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"

	"lythrum/internal/core"
	"lythrum/internal/palette"
	"lythrum/internal/ui"
)

// GridStep — шаг сетки холста в единицах холста (логических пикселях).
const GridStep = 64.0

// TitleBar — высота заголовка окна в единицах холста.
const TitleBar = 24.0

// WindowFrame — снимок окна/виджета для растеризации.
type WindowFrame struct {
	Title     string
	Rect      core.Rect // координаты холста
	Focused   bool
	Minimized bool
}

// CardFrame — снимок карточки Live Preview.
type CardFrame struct {
	Title  string
	Rect   core.Rect
	Poster image.Image
}

// FrameInput — плоский снимок состояния кадра (без ссылок на холст).
type FrameInput struct {
	Cam                  core.Camera
	ViewportW, ViewportH float64

	Windows []WindowFrame // окна
	Widgets []WindowFrame // виджеты (рисуются как рамки-плейсхолдеры)
	Cards   []CardFrame
	Guides  []core.Guide // активные магнитные направляющие
}

// Paint растеризует кадр: фон → сетка → окна → виджеты → карточки →
// направляющие снапа. Вне фрустума не рисуется (куллинг честный).
func Paint(in FrameInput, face *Face) *image.RGBA {
	w, h := int(in.ViewportW), int(in.ViewportH)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRect(img, img.Bounds(), paletteToRGBA(palette.CanvasBackground))
	drawGrid(img, in.Cam)
	fr := in.Cam.Frustum(in.ViewportW, in.ViewportH)

	for _, win := range in.Windows {
		if win.Minimized || !win.Rect.Intersects(fr) {
			continue
		}
		border := palette.WindowBorderInactive
		if win.Focused {
			border = palette.WindowBorderActive
		}
		drawWindow(img, in.Cam, win.Rect, win.Title, border, face)
	}
	for _, wg := range in.Widgets {
		if !wg.Rect.Intersects(fr) {
			continue
		}
		drawWindow(img, in.Cam, wg.Rect, wg.Title,
			palette.WindowBorderInactive, face)
	}
	for _, card := range in.Cards {
		if !card.Rect.Intersects(fr) {
			continue
		}
		drawCard(img, in.Cam, card, face)
	}
	drawGuides(img, in.Cam, in.Guides)
	return img
}

// ── Преобразования координат ─────────────────────────────────────────────

// toScreen — точка холста → экран: screen = (canvas − cam) · zoom.
func toScreen(cam core.Camera, x, y float64) (float64, float64) {
	return (x - cam.X) * cam.Zoom, (y - cam.Y) * cam.Zoom
}

// toScreenRect — прямоугольник холста → экранный (с округлением).
func toScreenRect(cam core.Camera, r core.Rect) image.Rectangle {
	x0, y0 := toScreen(cam, r.X, r.Y)
	x1, y1 := toScreen(cam, r.X+r.W, r.Y+r.H)
	return image.Rect(int(x0), int(y0), int(x1), int(y1))
}

// ── Примитивы ────────────────────────────────────────────────────────────

// fillRect заливает прямоугольник (с обрезкой по границам изображения).
func fillRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			overPixel(img, x, y, c)
		}
	}
}

// overPixel — смешивание пикселя (источник не-premultiplied).
func overPixel(img *image.RGBA, x, y int, c color.RGBA) {
	i := img.PixOffset(x, y)
	a := uint32(c.A)
	for ch := 0; ch < 4; ch++ {
		var src uint8
		switch ch {
		case 0:
			src = c.R
		case 1:
			src = c.G
		case 2:
			src = c.B
		default:
			src = 0xFF
		}
		d := uint32(img.Pix[i+ch])
		img.Pix[i+ch] = uint8((uint32(src)*a + d*(255-a)) / 255)
	}
}

// hline / vline — линии толщиной thick пикселей (направляющие, сетка).
func hline(img *image.RGBA, y, x0, x1 int, c color.RGBA, thick int) {
	b := img.Bounds()
	for d := 0; d < thick; d++ {
		yy := y + d
		if yy < b.Min.Y || yy >= b.Max.Y {
			continue
		}
		for x := max(x0, b.Min.X); x < min(x1, b.Max.X); x++ {
			overPixel(img, x, yy, c)
		}
	}
}

func vline(img *image.RGBA, x, y0, y1 int, c color.RGBA, thick int) {
	b := img.Bounds()
	for d := 0; d < thick; d++ {
		xx := x + d
		if xx < b.Min.X || xx >= b.Max.X {
			continue
		}
		for y := max(y0, b.Min.Y); y < min(y1, b.Max.Y); y++ {
			overPixel(img, xx, y, c)
		}
	}
}

// borderRect — рамка толщиной 2 экранных пикселя.
func borderRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	const t = 2
	if r.Dy() < 2*t || r.Dx() < 2*t {
		fillRect(img, r, c)
		return
	}
	hline(img, r.Min.Y, r.Min.X, r.Max.X, c, t)
	hline(img, r.Max.Y-t, r.Min.X, r.Max.X, c, t)
	vline(img, r.Min.X, r.Min.Y, r.Max.Y, c, t)
	vline(img, r.Max.X-t, r.Min.Y, r.Max.Y, c, t)
}

// ── Элементы кадра ───────────────────────────────────────────────────────

// drawGrid — сетка холста: вертикали/горизонтали каждые GridStep единиц.
func drawGrid(img *image.RGBA, cam core.Camera) {
	c := paletteToRGBA(palette.CanvasGrid)
	w, h := img.Bounds().Max.X, img.Bounds().Max.Y

	// Первая линия правее/ниже левого края фрустума.
	gx := math.Ceil(cam.X/GridStep) * GridStep
	for ; (gx-cam.X)*cam.Zoom < float64(w); gx += GridStep {
		vline(img, int((gx-cam.X)*cam.Zoom), 0, h, c, 1)
	}
	gy := math.Ceil(cam.Y/GridStep) * GridStep
	for ; (gy-cam.Y)*cam.Zoom < float64(h); gy += GridStep {
		hline(img, int((gy-cam.Y)*cam.Zoom), 0, w, c, 1)
	}
}

// drawWindow — окно/виджет: подложка, полоса заголовка, рамка, текст.
func drawWindow(img *image.RGBA, cam core.Camera, r core.Rect, title string, border palette.Color, face *Face) {
	sr := toScreenRect(cam, r)
	fillRect(img, sr, paletteToRGBA(palette.MenuBackground))
	borderRect(img, sr, paletteToRGBA(border))

	// Полоса заголовка в ЭКРАННЫХ пикселях растёт со зумом: окно
	// масштабируется как картинка холста.
	th := int(TitleBar * cam.Zoom)
	if th < 2 {
		th = 2
	}
	fillRect(img, image.Rect(sr.Min.X, sr.Min.Y, sr.Max.X, sr.Min.Y+th),
		paletteToRGBA(palette.OverlayBackground))

	if face == nil {
		return
	}
	pad := 4.0
	ty := float64(sr.Min.Y) + (float64(th)-face.Size()-pad)/2
	face.DrawTextClip(img, float64(sr.Min.X)+pad, ty, title,
		paletteToRGBA(palette.WindowTitleText), float64(sr.Max.X)-pad)
}

// drawCard — карточка Live Preview: постер (масштабирование) или плашка,
// рамка цвета снап-линии, заголовок.
func drawCard(img *image.RGBA, cam core.Camera, card CardFrame, face *Face) {
	sr := toScreenRect(cam, card.Rect)
	if card.Poster != nil {
		draw.ApproxBiLinear.Scale(img, sr, card.Poster, card.Poster.Bounds(), draw.Over, nil)
	} else {
		fillRect(img, sr, paletteToRGBA(palette.OverlayBackground))
	}
	borderRect(img, sr, paletteToRGBA(palette.SnapGuideLine))
	if face != nil {
		face.DrawTextClip(img, float64(sr.Min.X)+4, float64(sr.Min.Y)+4,
			card.Title, paletteToRGBA(palette.WindowTitleText), float64(sr.Max.X)-4)
	}
}

// drawGuides — магнитные направляющие снапа (линии на весь вьюпорт).
func drawGuides(img *image.RGBA, cam core.Camera, guides []core.Guide) {
	c := paletteToRGBA(palette.SnapGuideLine)
	w, h := img.Bounds().Max.X, img.Bounds().Max.Y
	for _, g := range guides {
		if g.Vertical {
			vline(img, int((g.At-cam.X)*cam.Zoom), 0, h, c, 2)
		} else {
			hline(img, int((g.At-cam.Y)*cam.Zoom), 0, w, c, 2)
		}
	}
}

// PaintDialog — панель текстового диалога (поиск/запуск): заголовок и
// строка ввода с кареткой-подчёркиванием. Экранные пиксели 1:1, панель
// в верхней трети по центру (стиль лаунчера).
func PaintDialog(img *image.RGBA, title, value string, face *Face) {
	b := img.Bounds()
	const panelW, panelH, pad = 560.0, 72.0, 14.0
	x := float64(b.Max.X)/2 - panelW/2
	y := float64(b.Max.Y)/3 - panelH/2
	r := image.Rect(int(x), int(y), int(x+panelW), int(y+panelH))
	fillRect(img, r, paletteToRGBA(palette.MenuBackground))
	borderRect(img, r, paletteToRGBA(palette.OverlayAccent))

	if face == nil {
		return
	}
	face.DrawTextClip(img, x+pad, y+pad, title,
		paletteToRGBA(palette.MenuShortcut), float64(r.Max.X)-pad)
	// Каретка как подчёркивание на конце значения.
	face.DrawTextClip(img, x+pad, y+pad+face.Size()+6, value+"_",
		paletteToRGBA(palette.WindowTitleText), float64(r.Max.X)-pad)
}

// ── Оверлей-слой ─────────────────────────────────────────────────────────

// PaintOverlay рисует меню поверх кадра в ЭКРАННЫХ пикселях 1:1
// (зум холста не участвует — требование фиксированного слоя). Иконки
// не рисуются: для них нужна отдельная face Material Symbols.
func PaintOverlay(img *image.RGBA, m *ui.Menu, face *Face) {
	r := image.Rect(int(m.X), int(m.Y), int(m.X+m.W), int(m.Y+m.Height()))
	fillRect(img, r, paletteToRGBA(palette.MenuBackground))
	borderRect(img, r, paletteToRGBA(palette.MenuSeparator))

	pad := 6.0
	y := m.Y + ui.MenuPadding
	for i, it := range m.Items {
		rowH := ui.ItemHeight
		if it.Separator {
			rowH = ui.SepHeight
			hline(img, int(y+rowH/2), r.Min.X+4, r.Max.X-4,
				paletteToRGBA(palette.MenuSeparator), 1)
			y += rowH
			continue
		}
		if i == m.Hover() {
			fillRect(img, image.Rect(int(m.X), int(y), int(m.X+m.W), int(y+rowH)),
				paletteToRGBA(palette.MenuHoverItem))
		}
		if face != nil {
			col := palette.MenuText
			if it.Disabled {
				col = palette.MenuSeparator
			}
			face.DrawTextClip(img, m.X+pad, y+(rowH-face.Size())/2, it.Label,
				paletteToRGBA(col), float64(r.Max.X)-pad)

			// Хоткей — приглушённым цветом у правого края.
			if it.Shortcut != "" && !it.Disabled {
				if w, _ := face.Measure(it.Shortcut); w < m.W-2*pad {
					face.DrawTextClip(img, float64(r.Max.X)-pad-w,
						y+(rowH-face.Size())/2, it.Shortcut,
						paletteToRGBA(palette.MenuShortcut), float64(r.Max.X)-pad)
				}
			}
		}
		y += rowH
	}
}

// ── Утилиты ──────────────────────────────────────────────────────────────

func paletteToRGBA(c palette.Color) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}

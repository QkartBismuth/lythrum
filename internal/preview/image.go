package preview

import (
	"bytes"
	"image"
	"image/png"
)

// decodePNG reads a picture produced by ffmpeg or written to the cache.
func decodePNG(raw []byte) (*image.RGBA, error) {
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	bounds := decoded.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			r, g, b, a := decoded.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			i := y*out.Stride + x*4
			out.Pix[i+0] = uint8(r >> 8)
			out.Pix[i+1] = uint8(g >> 8)
			out.Pix[i+2] = uint8(b >> 8)
			out.Pix[i+3] = uint8(a >> 8)
		}
	}
	return out, nil
}

// encodePNG writes a preview to the cache.
func encodePNG(img *image.RGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

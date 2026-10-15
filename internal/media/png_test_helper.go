package media

import (
	"image"
	"image/png"
	"os"
)

// writePNG is a small test helper kept next to the tests that use it.
func writePNG(path string, img image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}

//go:build !windows

package screen

import (
	"image"
	"image/color"
)

// GetVirtualScreenBounds returns the virtual screen bounds on non-Windows platforms.
func GetVirtualScreenBounds() (image.Rectangle, error) {
	return image.Rect(0, 0, 1920, 1080), nil
}

// CaptureScreen generates a mock gradient/checkerboard test pattern on non-Windows platforms.
func CaptureScreen() (image.Image, error) {
	return CaptureScreenBounds(image.Rect(0, 0, 1920, 1080))
}

// CaptureVirtualScreen generates a synthetic test pattern on non-Windows platforms.
func CaptureVirtualScreen() (image.Image, error) {
	return CaptureScreen()
}

// CaptureScreenBounds generates a mock gradient/checkerboard test canvas of specified bounds.
func CaptureScreenBounds(rect image.Rectangle) (image.Image, error) {
	if rect == (image.Rectangle{}) {
		rect = image.Rect(0, 0, 1920, 1080)
	}
	w := rect.Dx()
	h := rect.Dy()
	if w <= 0 || h <= 0 {
		return nil, ErrInvalidDimensions
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			isCheck := ((x/32)+(y/32))%2 == 0
			r := uint8((x * 255) / w)
			g := uint8((y * 255) / h)
			b := uint8(180)
			if isCheck {
				r = 255 - r
				g = 255 - g
			}
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img, nil
}

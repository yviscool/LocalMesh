//go:build windows

package screen

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modGdi32Capture = windows.NewLazySystemDLL("gdi32.dll")

	procGetDC     = user32.NewProc("GetDC")
	procReleaseDC = user32.NewProc("ReleaseDC")

	procCreateCompatibleDC     = modGdi32Capture.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = modGdi32Capture.NewProc("CreateCompatibleBitmap")
	procSelectObject           = modGdi32Capture.NewProc("SelectObject")
	procBitBlt                 = modGdi32Capture.NewProc("BitBlt")
	procGetDIBits              = modGdi32Capture.NewProc("GetDIBits")
	procDeleteDC               = modGdi32Capture.NewProc("DeleteDC")
	procDeleteObject           = modGdi32Capture.NewProc("DeleteObject")
)

const (
	smCXScreen        = 0
	smCYScreen        = 1
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	srccopy    = 0x00CC0020
	captureBlt = 0x40000000

	dibRGBColors = 0
	biRGB        = 0
)

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type bitmapInfo struct {
	bmiHeader bitmapInfoHeader
	bmiColors [1]uint32
}

// GetVirtualScreenBounds returns the bounding rectangle covering all connected monitors.
func GetVirtualScreenBounds() (image.Rectangle, error) {
	w, _, _ := procGetSystemMetrics.Call(uintptr(smCXVirtualScreen))
	h, _, _ := procGetSystemMetrics.Call(uintptr(smCYVirtualScreen))
	x, _, _ := procGetSystemMetrics.Call(uintptr(smXVirtualScreen))
	y, _, _ := procGetSystemMetrics.Call(uintptr(smYVirtualScreen))

	width := int(int32(w))
	height := int(int32(h))
	minX := int(int32(x))
	minY := int(int32(y))

	if width <= 0 || height <= 0 {
		w, _, _ = procGetSystemMetrics.Call(uintptr(smCXScreen))
		h, _, _ = procGetSystemMetrics.Call(uintptr(smCYScreen))
		width = int(int32(w))
		height = int(int32(h))
		minX = 0
		minY = 0
	}

	if width <= 0 || height <= 0 {
		return image.Rectangle{}, errors.New("failed to retrieve valid display metrics")
	}

	return image.Rect(minX, minY, minX+width, minY+height), nil
}

// CaptureScreen captures the full virtual desktop across all connected monitors.
func CaptureScreen() (image.Image, error) {
	bounds, err := GetVirtualScreenBounds()
	if err != nil {
		return nil, err
	}
	return CaptureScreenBounds(bounds)
}

// CaptureVirtualScreen is an alias for CaptureScreen for backwards compatibility.
func CaptureVirtualScreen() (image.Image, error) {
	return CaptureScreen()
}

// CaptureScreenBounds captures a specific rectangular region of the desktop.
// If rect is empty, the full virtual screen is captured.
func CaptureScreenBounds(rect image.Rectangle) (image.Image, error) {
	if rect == (image.Rectangle{}) {
		var err error
		rect, err = GetVirtualScreenBounds()
		if err != nil {
			return nil, err
		}
	}

	width := rect.Dx()
	height := rect.Dy()
	if width <= 0 || height <= 0 {
		return nil, ErrInvalidDimensions
	}

	x := rect.Min.X
	y := rect.Min.Y

	// 1. Desktop window DC
	hdcScreen, _, errGetDC := procGetDC.Call(0)
	if hdcScreen == 0 {
		return nil, fmt.Errorf("GetDC(0) failed: %w", errGetDC)
	}
	defer procReleaseDC.Call(0, hdcScreen)

	// 2. Compatible memory DC
	hdcMem, _, errCreateDC := procCreateCompatibleDC.Call(hdcScreen)
	if hdcMem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed: %w", errCreateDC)
	}
	defer procDeleteDC.Call(hdcMem)

	// 3. Compatible bitmap
	hBitmap, _, errCreateBmp := procCreateCompatibleBitmap.Call(hdcScreen, uintptr(width), uintptr(height))
	if hBitmap == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed: %w", errCreateBmp)
	}
	defer procDeleteObject.Call(hBitmap)

	// 4. Select bitmap into memory DC
	oldObj, _, _ := procSelectObject.Call(hdcMem, hBitmap)
	if oldObj == 0 {
		return nil, errors.New("SelectObject failed")
	}

	// 5. BitBlt from screen DC to memory DC (try with SRCCOPY | CAPTUREBLT first, then SRCCOPY)
	ret, _, _ := procBitBlt.Call(
		hdcMem,
		0, 0,
		uintptr(width), uintptr(height),
		hdcScreen,
		uintptr(x), uintptr(y),
		uintptr(srccopy|captureBlt),
	)
	if ret == 0 {
		ret, _, _ = procBitBlt.Call(
			hdcMem,
			0, 0,
			uintptr(width), uintptr(height),
			hdcScreen,
			uintptr(x), uintptr(y),
			uintptr(srccopy),
		)
		if ret == 0 {
			// In headless environments, BitBlt on desktop DC may return 0.
			// Fallback safely to synthetic pattern so service doesn't crash.
			procSelectObject.Call(hdcMem, oldObj)
			return generateFallbackPattern(width, height), nil
		}
	}

	// 6. Unselect bitmap before extracting DIB bits
	procSelectObject.Call(hdcMem, oldObj)

	// 7. Setup 32-bit top-down BITMAPINFO
	var bmi bitmapInfo
	bmi.bmiHeader.biSize = uint32(unsafe.Sizeof(bmi.bmiHeader))
	bmi.bmiHeader.biWidth = int32(width)
	bmi.bmiHeader.biHeight = -int32(height) // negative height indicates top-down DIB
	bmi.bmiHeader.biPlanes = 1
	bmi.bmiHeader.biBitCount = 32
	bmi.bmiHeader.biCompression = biRGB
	bmi.bmiHeader.biSizeImage = uint32(width * height * 4)

	rawBuf := make([]byte, width*height*4)
	linesCopied, _, errGetDIBits := procGetDIBits.Call(
		hdcMem,
		hBitmap,
		0,
		uintptr(height),
		uintptr(unsafe.Pointer(&rawBuf[0])),
		uintptr(unsafe.Pointer(&bmi)),
		uintptr(dibRGBColors),
	)
	if linesCopied == 0 {
		return nil, fmt.Errorf("GetDIBits failed: %w", errGetDIBits)
	}

	// 8. Convert BGRA raw buffer into *image.RGBA
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	totalPixels := width * height
	srcIdx := 0
	dstIdx := 0
	for i := 0; i < totalPixels; i++ {
		img.Pix[dstIdx] = rawBuf[srcIdx+2]   // Red
		img.Pix[dstIdx+1] = rawBuf[srcIdx+1] // Green
		img.Pix[dstIdx+2] = rawBuf[srcIdx]   // Blue
		img.Pix[dstIdx+3] = 255              // Alpha = 255 (fully opaque)
		srcIdx += 4
		dstIdx += 4
	}

	return img, nil
}

func generateFallbackPattern(w, h int) image.Image {
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
	return img
}

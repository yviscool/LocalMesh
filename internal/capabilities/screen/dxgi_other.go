//go:build !windows

package screen

import (
	"image"
	"time"
)

// DXGICapturer represents a stub DXGI Desktop Duplication API capturer on non-Windows platforms.
type DXGICapturer struct{}

// NewDXGICapturer returns ErrDXGIUnsupported on non-Windows platforms.
func NewDXGICapturer(displayIndex int, timeout time.Duration) (*DXGICapturer, error) {
	return nil, ErrDXGIUnsupported
}

// Width returns 0 on non-Windows platforms.
func (d *DXGICapturer) Width() int {
	return 0
}

// Height returns 0 on non-Windows platforms.
func (d *DXGICapturer) Height() int {
	return 0
}

// Bounds returns an empty rectangle on non-Windows platforms.
func (d *DXGICapturer) Bounds() image.Rectangle {
	return image.Rectangle{}
}

// Capture returns ErrDXGIUnsupported on non-Windows platforms.
func (d *DXGICapturer) Capture() (*CapturedFrame, error) {
	return nil, ErrDXGIUnsupported
}

// CaptureImage returns ErrDXGIUnsupported on non-Windows platforms.
func (d *DXGICapturer) CaptureImage() (image.Image, error) {
	return nil, ErrDXGIUnsupported
}

// Close releases stub resources.
func (d *DXGICapturer) Close() error {
	return nil
}

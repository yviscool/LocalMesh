//go:build windows

package screen

import "time"

// NewHybridCapturer creates a HybridCapturer for Windows.
// It prioritizes DXGI Desktop Duplication API (DDA) for 60FPS zero-copy capture
// and automatically falls back to GDI when DXGI is unsupported or encounters ACCESS_LOST,
// while periodically probing for DXGI recovery in the background.
func NewHybridCapturer(opts ...HybridOption) (HybridCapturer, error) {
	cfg := hybridConfig{
		probeInterval: 1 * time.Second,
		dxgiTimeout:   16 * time.Millisecond,
		displayIndex:  0,
		gdiCapture:    CaptureScreen,
	}

	cfg.dxgiFactory = func() (Capturer, error) {
		return NewDXGICapturer(cfg.displayIndex, cfg.dxgiTimeout)
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return newHybridCapturerCore(cfg)
}

//go:build !windows

package screen

import "time"

// NewHybridCapturer creates a HybridCapturer for non-Windows platforms.
// Since DXGI Desktop Duplication API is Windows-specific, non-Windows platforms
// operate smoothly using the standard CaptureScreen fallback implementation.
func NewHybridCapturer(opts ...HybridOption) (HybridCapturer, error) {
	cfg := hybridConfig{
		probeInterval: 1 * time.Second,
		dxgiTimeout:   16 * time.Millisecond,
		displayIndex:  0,
		dxgiFactory: func() (Capturer, error) {
			return nil, ErrDXGIUnsupported
		},
		gdiCapture: CaptureScreen,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return newHybridCapturerCore(cfg)
}

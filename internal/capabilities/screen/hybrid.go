package screen

import (
	"errors"
	"fmt"
	"image"
	"sync"
	"time"
)

var (
	// ErrDXGIUnsupported indicates DXGI Desktop Duplication is not supported on this platform or display.
	ErrDXGIUnsupported = errors.New("DXGI desktop duplication is unsupported on this system")

	// ErrDXGIAccessLost indicates DXGI Output Duplication access was lost (e.g. UAC prompt, mode switch, session change).
	ErrDXGIAccessLost = errors.New("DXGI desktop duplication access lost")

	// ErrNoNewFrame indicates AcquireNextFrame timed out without new desktop changes.
	ErrNoNewFrame = errors.New("no new frame available (timeout)")

	// ErrCapturerClosed indicates the screen capturer has already been closed.
	ErrCapturerClosed = errors.New("screen capturer is closed")
)

// CaptureBackend identifies the underlying screen capture implementation.
type CaptureBackend string

const (
	BackendDXGI CaptureBackend = "DXGI"
	BackendGDI  CaptureBackend = "GDI"
)

// CapturedFrame contains the captured desktop image and associated metadata.
type CapturedFrame struct {
	Image      image.Image       // The captured image
	DirtyRects []image.Rectangle // Bounding boxes of screen regions changed since last frame
	Width      int               // Frame width in pixels
	Height     int               // Frame height in pixels
	Timestamp  time.Time         // Time when frame was acquired
	Backend    CaptureBackend    // Backend used for capturing ("DXGI" or "GDI")
}

// Clone returns a deep copy of the CapturedFrame and its pixel data.
func (f *CapturedFrame) Clone() *CapturedFrame {
	if f == nil {
		return nil
	}

	clone := &CapturedFrame{
		DirtyRects: make([]image.Rectangle, len(f.DirtyRects)),
		Width:      f.Width,
		Height:     f.Height,
		Timestamp:  f.Timestamp,
		Backend:    f.Backend,
	}
	copy(clone.DirtyRects, f.DirtyRects)

	if rgba, ok := f.Image.(*image.RGBA); ok && rgba != nil {
		dst := image.NewRGBA(rgba.Rect)
		copy(dst.Pix, rgba.Pix)
		clone.Image = dst
	} else if f.Image != nil {
		b := f.Image.Bounds()
		dst := image.NewRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				dst.Set(x, y, f.Image.At(x, y))
			}
		}
		clone.Image = dst
	}

	return clone
}

// Capturer represents a generic screen capturer interface.
type Capturer interface {
	// Capture acquires the next screen frame with change metadata.
	Capture() (*CapturedFrame, error)

	// CaptureImage acquires only the image representation of the current screen.
	CaptureImage() (image.Image, error)

	// Close releases any allocated resources.
	Close() error
}

// HybridCapturer represents a hybrid screen capturer that prioritizes high-speed DXGI
// and automatically falls back to GDI when access is lost or unsupported, probing for
// DXGI recovery in the background.
type HybridCapturer interface {
	Capturer

	// ActiveBackend returns the currently active capture backend ("DXGI" or "GDI").
	ActiveBackend() CaptureBackend

	// IsDXGIActive returns true if DXGI is currently active.
	IsDXGIActive() bool
}

// hybridConfig stores configuration options for HybridCapturer.
type hybridConfig struct {
	probeInterval time.Duration
	dxgiTimeout   time.Duration
	displayIndex  int
	forceGDI      bool
	dxgiFactory   func() (Capturer, error)
	gdiCapture    func() (image.Image, error)
}

// HybridOption configures a HybridCapturer.
type HybridOption func(*hybridConfig)

// WithProbeInterval sets the interval for background DXGI recovery probing.
func WithProbeInterval(d time.Duration) HybridOption {
	return func(c *hybridConfig) {
		if d > 0 {
			c.probeInterval = d
		}
	}
}

// WithDXGITimeout sets the timeout for DXGI AcquireNextFrame calls.
func WithDXGITimeout(d time.Duration) HybridOption {
	return func(c *hybridConfig) {
		if d > 0 {
			c.dxgiTimeout = d
		}
	}
}

// WithDisplayIndex specifies which display output to capture (0 = primary).
func WithDisplayIndex(idx int) HybridOption {
	return func(c *hybridConfig) {
		if idx >= 0 {
			c.displayIndex = idx
		}
	}
}

// WithForceGDI forces HybridCapturer to only use GDI capture.
func WithForceGDI(force bool) HybridOption {
	return func(c *hybridConfig) {
		c.forceGDI = force
	}
}

// WithDXGIFactory injects a custom DXGI capturer factory (useful for testing).
func WithDXGIFactory(factory func() (Capturer, error)) HybridOption {
	return func(c *hybridConfig) {
		c.dxgiFactory = factory
	}
}

// WithGDICapturer injects a custom GDI capture function (useful for testing).
func WithGDICapturer(fn func() (image.Image, error)) HybridOption {
	return func(c *hybridConfig) {
		c.gdiCapture = fn
	}
}

// hybridCapturer is the core implementation of HybridCapturer.
type hybridCapturer struct {
	mu            sync.Mutex
	cfg           hybridConfig
	activeBackend CaptureBackend
	dxgi          Capturer
	closed        bool
	stopCh        chan struct{}
	doneCh        chan struct{}
	probeSignal   chan struct{}
}

func newHybridCapturerCore(cfg hybridConfig) (*hybridCapturer, error) {
	if cfg.gdiCapture == nil {
		cfg.gdiCapture = CaptureScreen
	}
	if cfg.probeInterval <= 0 {
		cfg.probeInterval = 1 * time.Second
	}

	h := &hybridCapturer{
		cfg:           cfg,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		probeSignal:   make(chan struct{}, 1),
		activeBackend: BackendGDI,
	}

	// Try initial DXGI initialization if not forced to GDI
	if !cfg.forceGDI && cfg.dxgiFactory != nil {
		dxgi, err := cfg.dxgiFactory()
		if err == nil && dxgi != nil {
			h.dxgi = dxgi
			h.activeBackend = BackendDXGI
		}
	}

	go h.probeLoop()
	return h, nil
}

// Capture acquires a desktop frame, automatically falling back to GDI if DXGI fails.
func (h *hybridCapturer) Capture() (*CapturedFrame, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil, ErrCapturerClosed
	}

	// Try DXGI if active
	if h.activeBackend == BackendDXGI && h.dxgi != nil {
		frame, err := h.dxgi.Capture()
		if err == nil {
			return frame, nil
		}

		// Check for recoverable / access lost / unsupported errors
		if errors.Is(err, ErrDXGIAccessLost) || errors.Is(err, ErrDXGIUnsupported) || errors.Is(err, ErrCapturerClosed) {
			_ = h.dxgi.Close()
			h.dxgi = nil
			h.activeBackend = BackendGDI

			// Signal background probe to begin checking for recovery
			select {
			case h.probeSignal <- struct{}{}:
			default:
			}
		} else {
			// Other unexpected error: still attempt GDI fallback to avoid dropping the frame
			_ = h.dxgi.Close()
			h.dxgi = nil
			h.activeBackend = BackendGDI
		}
	}

	// Fallback to GDI capture
	img, err := h.cfg.gdiCapture()
	if err != nil {
		return nil, fmt.Errorf("gdi capture failed: %w", err)
	}

	bounds := img.Bounds()
	return &CapturedFrame{
		Image:      img,
		DirtyRects: []image.Rectangle{bounds},
		Width:      bounds.Dx(),
		Height:     bounds.Dy(),
		Timestamp:  time.Now(),
		Backend:    BackendGDI,
	}, nil
}

// CaptureImage returns only the image representation of the desktop.
func (h *hybridCapturer) CaptureImage() (image.Image, error) {
	frame, err := h.Capture()
	if err != nil {
		return nil, err
	}
	return frame.Image, nil
}

// ActiveBackend returns the currently active backend.
func (h *hybridCapturer) ActiveBackend() CaptureBackend {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activeBackend
}

// IsDXGIActive returns true if DXGI is currently active.
func (h *hybridCapturer) IsDXGIActive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activeBackend == BackendDXGI
}

// Close gracefully stops the background probe and releases DXGI resources.
func (h *hybridCapturer) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	close(h.stopCh)

	if h.dxgi != nil {
		_ = h.dxgi.Close()
		h.dxgi = nil
	}
	h.mu.Unlock()

	<-h.doneCh
	return nil
}

// probeLoop periodically attempts to reinitialize DXGI when degraded to GDI.
func (h *hybridCapturer) probeLoop() {
	defer close(h.doneCh)

	ticker := time.NewTicker(h.cfg.probeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
		case <-h.probeSignal:
		}

		h.mu.Lock()
		isClosed := h.closed
		active := h.activeBackend
		forceGDI := h.cfg.forceGDI
		factory := h.cfg.dxgiFactory
		h.mu.Unlock()

		if isClosed {
			return
		}

		// Only probe if currently in GDI mode and DXGI is allowed
		if active == BackendGDI && !forceGDI && factory != nil {
			newDXGI, err := factory()
			if err == nil && newDXGI != nil {
				h.mu.Lock()
				if !h.closed && h.activeBackend == BackendGDI {
					h.dxgi = newDXGI
					h.activeBackend = BackendDXGI
				} else {
					_ = newDXGI.Close()
				}
				h.mu.Unlock()
			}
		}
	}
}

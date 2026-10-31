package screen

import (
	"errors"
	"image"
	"image/color"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockCapturer implements Capturer for testing switching logic and concurrency.
type mockCapturer struct {
	mu         sync.Mutex
	failErr    error
	callCount  int
	width      int
	height     int
	closed     bool
	dirtyRects []image.Rectangle
}

func newMockCapturer(width, height int, failErr error) *mockCapturer {
	return &mockCapturer{
		width:      width,
		height:     height,
		failErr:    failErr,
		dirtyRects: []image.Rectangle{image.Rect(0, 0, width, height)},
	}
}

func (m *mockCapturer) Capture() (*CapturedFrame, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, ErrCapturerClosed
	}
	m.callCount++
	if m.failErr != nil {
		return nil, m.failErr
	}

	img := image.NewRGBA(image.Rect(0, 0, m.width, m.height))
	// Fill with a test color
	c := color.RGBA{R: 100, G: 150, B: 200, A: 255}
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			img.SetRGBA(x, y, c)
		}
	}

	return &CapturedFrame{
		Image:      img,
		DirtyRects: m.dirtyRects,
		Width:      m.width,
		Height:     m.height,
		Timestamp:  time.Now(),
		Backend:    BackendDXGI,
	}, nil
}

func (m *mockCapturer) CaptureImage() (image.Image, error) {
	frame, err := m.Capture()
	if err != nil {
		return nil, err
	}
	return frame.Image, nil
}

func (m *mockCapturer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockCapturer) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failErr = err
}

func mockGDICapture(w, h int) func() (image.Image, error) {
	return func() (image.Image, error) {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
			}
		}
		return img, nil
	}
}

// 1. DXGI/GDI 切换逻辑单元测试 (Switching and Fallback Logic)

func TestHybridCapturer_InitialDXGI(t *testing.T) {
	mockDXGI := newMockCapturer(1920, 1080, nil)
	h, err := NewHybridCapturer(
		WithDXGIFactory(func() (Capturer, error) {
			return mockDXGI, nil
		}),
		WithGDICapturer(mockGDICapture(1920, 1080)),
	)
	if err != nil {
		t.Fatalf("NewHybridCapturer failed: %v", err)
	}
	defer h.Close()

	if !h.IsDXGIActive() {
		t.Errorf("expected DXGI to be active, got backend: %s", h.ActiveBackend())
	}
	if h.ActiveBackend() != BackendDXGI {
		t.Errorf("expected BackendDXGI, got %s", h.ActiveBackend())
	}

	frame, err := h.Capture()
	if err != nil {
		t.Fatalf("h.Capture() failed: %v", err)
	}
	if frame.Backend != BackendDXGI {
		t.Errorf("expected frame backend DXGI, got %s", frame.Backend)
	}
	if frame.Width != 1920 || frame.Height != 1080 {
		t.Errorf("unexpected frame dimensions: %dx%d", frame.Width, frame.Height)
	}
}

func TestHybridCapturer_InitialGDI_Fallback(t *testing.T) {
	h, err := NewHybridCapturer(
		WithDXGIFactory(func() (Capturer, error) {
			return nil, ErrDXGIUnsupported
		}),
		WithGDICapturer(mockGDICapture(1280, 720)),
	)
	if err != nil {
		t.Fatalf("NewHybridCapturer failed: %v", err)
	}
	defer h.Close()

	if h.IsDXGIActive() {
		t.Errorf("expected DXGI to be inactive")
	}
	if h.ActiveBackend() != BackendGDI {
		t.Errorf("expected BackendGDI, got %s", h.ActiveBackend())
	}

	frame, err := h.Capture()
	if err != nil {
		t.Fatalf("h.Capture() failed: %v", err)
	}
	if frame.Backend != BackendGDI {
		t.Errorf("expected frame backend GDI, got %s", frame.Backend)
	}
	if frame.Width != 1280 || frame.Height != 720 {
		t.Errorf("unexpected frame dimensions: %dx%d", frame.Width, frame.Height)
	}
}

func TestHybridCapturer_AccessLost_FallbackToGDI(t *testing.T) {
	mockDXGI := newMockCapturer(1920, 1080, nil)
	h, err := NewHybridCapturer(
		WithDXGIFactory(func() (Capturer, error) {
			mockDXGI.mu.Lock()
			err := mockDXGI.failErr
			mockDXGI.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return mockDXGI, nil
		}),
		WithGDICapturer(mockGDICapture(1920, 1080)),
		WithProbeInterval(500*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewHybridCapturer failed: %v", err)
	}
	defer h.Close()

	// 1st capture: normal DXGI
	frame1, err := h.Capture()
	if err != nil || frame1.Backend != BackendDXGI {
		t.Fatalf("1st capture expected DXGI, got %v, err: %v", frame1, err)
	}

	// Simulate UAC / display mode change / ACCESS_LOST
	mockDXGI.SetError(ErrDXGIAccessLost)

	// 2nd capture: MUST seamlessly fallback to GDI without returning an error to caller
	frame2, err := h.Capture()
	if err != nil {
		t.Fatalf("2nd capture failed during ACCESS_LOST fallback: %v", err)
	}
	if frame2.Backend != BackendGDI {
		t.Errorf("expected fallback to GDI, got %s", frame2.Backend)
	}
	if h.IsDXGIActive() {
		t.Errorf("expected DXGI to be inactive after ACCESS_LOST")
	}
	if h.ActiveBackend() != BackendGDI {
		t.Errorf("expected ActiveBackend() to be GDI, got %s", h.ActiveBackend())
	}
}

func TestHybridCapturer_ProbeRecoveryToDXGI(t *testing.T) {
	var isHealthy atomic.Bool
	isHealthy.Store(false)

	h, err := NewHybridCapturer(
		WithDXGIFactory(func() (Capturer, error) {
			if !isHealthy.Load() {
				return nil, ErrDXGIAccessLost
			}
			return newMockCapturer(1920, 1080, nil), nil
		}),
		WithGDICapturer(mockGDICapture(1920, 1080)),
		WithProbeInterval(20*time.Millisecond), // fast probe for test
	)
	if err != nil {
		t.Fatalf("NewHybridCapturer failed: %v", err)
	}
	defer h.Close()

	// Initially in GDI mode
	if h.IsDXGIActive() {
		t.Errorf("expected initially GDI")
	}

	// Simulate DXGI becoming healthy again
	isHealthy.Store(true)

	// Wait for background probe to detect recovery
	recovered := false
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if h.IsDXGIActive() {
			recovered = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !recovered {
		t.Fatalf("HybridCapturer failed to automatically probe and recover DXGI mode")
	}

	frame, err := h.Capture()
	if err != nil {
		t.Fatalf("capture after recovery failed: %v", err)
	}
	if frame.Backend != BackendDXGI {
		t.Errorf("expected DXGI backend after recovery, got %s", frame.Backend)
	}
}

func TestHybridCapturer_ForceGDI(t *testing.T) {
	mockDXGI := newMockCapturer(1920, 1080, nil)
	h, err := NewHybridCapturer(
		WithForceGDI(true),
		WithDXGIFactory(func() (Capturer, error) {
			return mockDXGI, nil
		}),
		WithGDICapturer(mockGDICapture(1024, 768)),
	)
	if err != nil {
		t.Fatalf("NewHybridCapturer failed: %v", err)
	}
	defer h.Close()

	if h.IsDXGIActive() {
		t.Errorf("expected DXGI to be disabled under WithForceGDI")
	}
	if h.ActiveBackend() != BackendGDI {
		t.Errorf("expected BackendGDI, got %s", h.ActiveBackend())
	}
}

// 2. 图像尺寸边界测试 (Image Dimension Boundaries)

func TestImageDimensionBoundaries(t *testing.T) {
	testSizes := []struct {
		name string
		w    int
		h    int
	}{
		{"1x1 minimal", 1, 1},
		{"Odd width (stride padding)", 1366, 768},
		{"Standard 1080p", 1920, 1080},
		{"4K UHD", 3840, 2160},
	}

	for _, tc := range testSizes {
		t.Run(tc.name, func(t *testing.T) {
			mock := newMockCapturer(tc.w, tc.h, nil)
			h, err := NewHybridCapturer(
				WithDXGIFactory(func() (Capturer, error) { return mock, nil }),
				WithGDICapturer(mockGDICapture(tc.w, tc.h)),
			)
			if err != nil {
				t.Fatalf("NewHybridCapturer failed: %v", err)
			}
			defer h.Close()

			frame, err := h.Capture()
			if err != nil {
				t.Fatalf("Capture() failed: %v", err)
			}
			if frame.Width != tc.w || frame.Height != tc.h {
				t.Fatalf("expected %dx%d, got %dx%d", tc.w, tc.h, frame.Width, frame.Height)
			}
			if frame.Image.Bounds().Dx() != tc.w || frame.Image.Bounds().Dy() != tc.h {
				t.Fatalf("expected image bounds %dx%d, got %v", tc.w, tc.h, frame.Image.Bounds())
			}

			img, err := h.CaptureImage()
			if err != nil {
				t.Fatalf("CaptureImage() failed: %v", err)
			}
			if img.Bounds().Dx() != tc.w || img.Bounds().Dy() != tc.h {
				t.Fatalf("expected image bounds %dx%d, got %v", tc.w, tc.h, img.Bounds())
			}
		})
	}
}

func TestCapturedFrame_Clone(t *testing.T) {
	orig := &CapturedFrame{
		Image:      image.NewRGBA(image.Rect(0, 0, 100, 100)),
		DirtyRects: []image.Rectangle{image.Rect(10, 10, 20, 20)},
		Width:      100,
		Height:     100,
		Timestamp:  time.Now(),
		Backend:    BackendDXGI,
	}

	clone := orig.Clone()
	if clone == nil {
		t.Fatal("expected non-nil clone")
	}

	// Verify deep copy of DirtyRects
	clone.DirtyRects[0] = image.Rect(0, 0, 50, 50)
	if orig.DirtyRects[0] == clone.DirtyRects[0] {
		t.Error("mutating clone.DirtyRects affected original frame")
	}

	// Verify deep copy of pixel data
	origRGBA := orig.Image.(*image.RGBA)
	cloneRGBA := clone.Image.(*image.RGBA)
	origRGBA.SetRGBA(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	cloneRGBA.SetRGBA(0, 0, color.RGBA{R: 0, G: 255, B: 0, A: 255})

	if origRGBA.RGBAAt(0, 0) == cloneRGBA.RGBAAt(0, 0) {
		t.Error("mutating clone.Image affected original frame")
	}

	// Nil clone
	var nilFrame *CapturedFrame
	if nilFrame.Clone() != nil {
		t.Error("expected nil clone for nil frame")
	}
}

func TestDirtyRects_Clipping(t *testing.T) {
	bounds := image.Rect(0, 0, 1920, 1080)

	// Test rects extending beyond screen edges
	outRect1 := image.Rect(-50, -50, 100, 100)
	outRect2 := image.Rect(1900, 1000, 2000, 1200)

	clipped1 := outRect1.Intersect(bounds)
	clipped2 := outRect2.Intersect(bounds)

	if clipped1 != image.Rect(0, 0, 100, 100) {
		t.Errorf("unexpected clipped rect 1: %v", clipped1)
	}
	if clipped2 != image.Rect(1900, 1000, 1920, 1080) {
		t.Errorf("unexpected clipped rect 2: %v", clipped2)
	}

	// Completely out-of-bounds rect
	disjoint := image.Rect(2000, 2000, 2100, 2100).Intersect(bounds)
	if !disjoint.Empty() {
		t.Errorf("expected empty intersection, got: %v", disjoint)
	}
}

// 3. 并发截屏安全测试 (Concurrent Capture Safety)

func TestHybridCapturer_ConcurrentCapture(t *testing.T) {
	mockDXGI := newMockCapturer(640, 480, nil)
	h, err := NewHybridCapturer(
		WithDXGIFactory(func() (Capturer, error) {
			return mockDXGI, nil
		}),
		WithGDICapturer(mockGDICapture(640, 480)),
		WithProbeInterval(10*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewHybridCapturer failed: %v", err)
	}
	defer h.Close()

	const numGoroutines = 16
	const capturesPerGoroutine = 40

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Goroutine that flips ACCESS_LOST back and forth during concurrent capturing
	stopToggle := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		fail := false
		for {
			select {
			case <-stopToggle:
				return
			case <-ticker.C:
				fail = !fail
				if fail {
					mockDXGI.SetError(ErrDXGIAccessLost)
				} else {
					mockDXGI.SetError(nil)
				}
			}
		}
	}()

	var captureCount atomic.Int64
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < capturesPerGoroutine; j++ {
				if j%2 == 0 {
					frame, err := h.Capture()
					if err != nil {
						t.Errorf("goroutine %d: Capture() failed: %v", id, err)
						return
					}
					if frame == nil || frame.Image == nil || frame.Width <= 0 || frame.Height <= 0 {
						t.Errorf("goroutine %d: invalid frame captured", id)
						return
					}
				} else {
					img, err := h.CaptureImage()
					if err != nil {
						t.Errorf("goroutine %d: CaptureImage() failed: %v", id, err)
						return
					}
					if img == nil || img.Bounds().Empty() {
						t.Errorf("goroutine %d: invalid image captured", id)
						return
					}
				}
				captureCount.Add(1)
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	close(stopToggle)

	expectedTotal := int64(numGoroutines * capturesPerGoroutine)
	if captureCount.Load() != expectedTotal {
		t.Errorf("expected %d total captures, got %d", expectedTotal, captureCount.Load())
	}
}

func TestHybridCapturer_ConcurrentClose(t *testing.T) {
	mockDXGI := newMockCapturer(640, 480, nil)
	h, err := NewHybridCapturer(
		WithDXGIFactory(func() (Capturer, error) {
			return mockDXGI, nil
		}),
		WithGDICapturer(mockGDICapture(640, 480)),
	)
	if err != nil {
		t.Fatalf("NewHybridCapturer failed: %v", err)
	}

	const numWorkers = 8
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, err := h.Capture()
				if errors.Is(err, ErrCapturerClosed) {
					return
				}
				time.Sleep(500 * time.Microsecond)
			}
		}()
	}

	time.Sleep(5 * time.Millisecond)
	_ = h.Close()

	wg.Wait()

	// Post-close capture must return ErrCapturerClosed
	_, err = h.Capture()
	if !errors.Is(err, ErrCapturerClosed) {
		t.Errorf("expected ErrCapturerClosed after Close(), got: %v", err)
	}
}

// 4. Windows 真实平台硬件测试 (Real Windows Environment Integration)

func TestRealWindowsScreenCapture(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("skipping Windows native test on non-windows platform")
	}

	// Test real HybridCapturer initialization
	dxgiDirect, errDXGI := NewDXGICapturer(0, 50*time.Millisecond)
	if errDXGI != nil {
		t.Logf("Direct NewDXGICapturer returned: %v", errDXGI)
	} else {
		t.Logf("Direct NewDXGICapturer succeeded: %dx%d", dxgiDirect.Width(), dxgiDirect.Height())
		_ = dxgiDirect.Close()
	}

	h, err := NewHybridCapturer(
		WithDXGITimeout(50 * time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewHybridCapturer() on Windows failed: %v", err)
	}
	defer h.Close()

	t.Logf("Active screen capture backend: %s (DXGI Active: %v)", h.ActiveBackend(), h.IsDXGIActive())

	frame, err := h.Capture()
	if err != nil {
		t.Fatalf("Real capture failed: %v", err)
	}
	if frame == nil || frame.Image == nil {
		t.Fatal("expected valid captured frame from real hardware")
	}

	t.Logf("Captured frame: %dx%d, Backend: %s, DirtyRects: %d, Timestamp: %s",
		frame.Width, frame.Height, frame.Backend, len(frame.DirtyRects), frame.Timestamp.Format(time.RFC3339Nano))

	if frame.Width <= 0 || frame.Height <= 0 {
		t.Errorf("invalid real frame dimensions: %dx%d", frame.Width, frame.Height)
	}

	// Capture image test
	img, err := h.CaptureImage()
	if err != nil {
		t.Fatalf("CaptureImage() failed: %v", err)
	}
	if img.Bounds().Empty() {
		t.Error("expected non-empty image bounds")
	}
}

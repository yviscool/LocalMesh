package screen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"
)

func TestFrameHeaderSerialization(t *testing.T) {
	t.Run("valid roundtrip", func(t *testing.T) {
		h, err := NewFrameHeader(FormatJPEG, 1920, 1080, 54321, 42)
		if err != nil {
			t.Fatalf("NewFrameHeader failed: %v", err)
		}
		h.Flags = FlagKeyFrame

		data, err := h.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary failed: %v", err)
		}
		if len(data) != HeaderSize {
			t.Fatalf("expected header length %d, got %d", HeaderSize, len(data))
		}

		var decoded FrameHeader
		if err := decoded.UnmarshalBinary(data); err != nil {
			t.Fatalf("UnmarshalBinary failed: %v", err)
		}

		if decoded.Magic != FrameMagic {
			t.Fatalf("magic mismatch: %v", decoded.Magic)
		}
		if decoded.Version != CurrentProtocolVersion {
			t.Fatalf("version mismatch: %d", decoded.Version)
		}
		if decoded.Format != FormatJPEG {
			t.Fatalf("format mismatch: %v", decoded.Format)
		}
		if decoded.Flags != FlagKeyFrame {
			t.Fatalf("flags mismatch: %d", decoded.Flags)
		}
		if decoded.Sequence != 42 {
			t.Fatalf("sequence mismatch: %d", decoded.Sequence)
		}
		if decoded.Width != 1920 || decoded.Height != 1080 {
			t.Fatalf("dimensions mismatch: %dx%d", decoded.Width, decoded.Height)
		}
		if decoded.PayloadSize != 54321 {
			t.Fatalf("payload size mismatch: %d", decoded.PayloadSize)
		}
	})

	t.Run("invalid magic", func(t *testing.T) {
		badData := make([]byte, HeaderSize)
		copy(badData[:4], []byte("BAD!"))

		var decoded FrameHeader
		err := decoded.UnmarshalBinary(badData)
		if err != ErrInvalidMagic {
			t.Fatalf("expected ErrInvalidMagic, got %v", err)
		}
	})

	t.Run("truncated header data", func(t *testing.T) {
		var decoded FrameHeader
		err := decoded.UnmarshalBinary([]byte("short"))
		if err != ErrInvalidHeaderSize {
			t.Fatalf("expected ErrInvalidHeaderSize, got %v", err)
		}
	})

	t.Run("invalid dimensions validation", func(t *testing.T) {
		_, err := NewFrameHeader(FormatRawRGBA, 0, 1080, 100, 1)
		if err != ErrInvalidDimensions {
			t.Fatalf("expected ErrInvalidDimensions for width 0, got %v", err)
		}

		_, err = NewFrameHeader(FormatRawRGBA, MaxDimension+1, 1080, 100, 1)
		if err != ErrInvalidDimensions {
			t.Fatalf("expected ErrInvalidDimensions for oversized width, got %v", err)
		}
	})
}

func TestFrameEncodeDecode(t *testing.T) {
	header, err := NewFrameHeader(FormatRawRGBA, 800, 600, 16, 10)
	if err != nil {
		t.Fatalf("NewFrameHeader failed: %v", err)
	}

	payload := []byte("0123456789abcdef") // exactly 16 bytes

	encoded, err := EncodeFrame(header, payload)
	if err != nil {
		t.Fatalf("EncodeFrame failed: %v", err)
	}

	decodedHeader, decodedPayload, err := DecodeFrame(encoded)
	if err != nil {
		t.Fatalf("DecodeFrame failed: %v", err)
	}

	if decodedHeader.Sequence != 10 {
		t.Fatalf("decoded sequence mismatch: %d", decodedHeader.Sequence)
	}
	if !bytes.Equal(decodedPayload, payload) {
		t.Fatalf("decoded payload mismatch")
	}

	// Payload mismatch check
	wrongPayload := []byte("short")
	_, err = EncodeFrame(header, wrongPayload)
	if err != ErrPayloadMismatch {
		t.Fatalf("expected ErrPayloadMismatch, got %v", err)
	}
}

func TestInputEventValidationAndSerialization(t *testing.T) {
	tests := []struct {
		name    string
		event   InputEvent
		wantErr error
	}{
		{
			name: "valid normalized mouse move",
			event: InputEvent{
				Type:       EventMouseMove,
				X:          0.5,
				Y:          0.75,
				Normalized: true,
			},
			wantErr: nil,
		},
		{
			name: "valid pixel mouse click",
			event: InputEvent{
				Type:       EventMouseDown,
				X:          500,
				Y:          300,
				Button:     ButtonLeft,
				Normalized: false,
			},
			wantErr: nil,
		},
		{
			name: "invalid mouse button for click",
			event: InputEvent{
				Type:       EventMouseDown,
				X:          0.1,
				Y:          0.2,
				Button:     ButtonNone,
				Normalized: true,
			},
			wantErr: ErrInvalidMouseButton,
		},
		{
			name: "out of range normalized coordinates",
			event: InputEvent{
				Type:       EventMouseMove,
				X:          1.5,
				Y:          0.5,
				Normalized: true,
			},
			wantErr: ErrCoordinatesOutOfRange,
		},
		{
			name: "valid keydown event",
			event: InputEvent{
				Type:      EventKeyDown,
				KeyCode:   0x41, // 'A'
				Modifiers: ModCtrl,
			},
			wantErr: nil,
		},
		{
			name: "valid unicode text event",
			event: InputEvent{
				Type: EventKeyDown,
				Text: "你好",
			},
			wantErr: nil,
		},
		{
			name: "key event with neither keycode nor text",
			event: InputEvent{
				Type: EventKeyDown,
			},
			wantErr: ErrKeyNotSpecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if err != tt.wantErr {
				t.Fatalf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}

	t.Run("binary roundtrip", func(t *testing.T) {
		orig := InputEvent{
			Type:       EventMouseDown,
			X:          0.35,
			Y:          0.82,
			Button:     ButtonRight,
			WheelDelta: -120,
			KeyCode:    0x1B,
			Modifiers:  ModShift | ModAlt,
			Text:       "test-key",
			Normalized: true,
		}

		data, err := orig.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary failed: %v", err)
		}

		var decoded InputEvent
		if err := decoded.UnmarshalBinary(data); err != nil {
			t.Fatalf("UnmarshalBinary failed: %v", err)
		}

		if decoded.Type != orig.Type ||
			decoded.Normalized != orig.Normalized ||
			decoded.Button != orig.Button ||
			decoded.WheelDelta != orig.WheelDelta ||
			decoded.KeyCode != orig.KeyCode ||
			decoded.Modifiers != orig.Modifiers ||
			decoded.Text != orig.Text {
			t.Fatalf("decoded event mismatch: %+v vs %+v", decoded, orig)
		}
	})
}

func TestThumbnailDownscaling(t *testing.T) {
	t.Run("dimensions calculation", func(t *testing.T) {
		// 1920x1080 with 320x180 max -> exact 320x180
		w, h := CalculateThumbnailDimensions(1920, 1080, 320, 180)
		if w != 320 || h != 180 {
			t.Fatalf("expected 320x180, got %dx%d", w, h)
		}

		// 1000x2000 (portrait) with 320x180 max -> bounded by height 180 (90x180)
		w, h = CalculateThumbnailDimensions(1000, 2000, 320, 180)
		if w != 90 || h != 180 {
			t.Fatalf("expected 90x180, got %dx%d", w, h)
		}
	})

	t.Run("downscale raw RGBA bytes", func(t *testing.T) {
		srcW, srcH := 4, 4
		src := make([]byte, srcW*srcH*4)
		for i := 0; i < len(src); i += 4 {
			src[i] = 255   // R
			src[i+1] = 128 // G
			src[i+2] = 64  // B
			src[i+3] = 255 // A
		}

		dstW, dstH := 2, 2
		dst, err := DownscaleRGBA(src, srcW, srcH, dstW, dstH)
		if err != nil {
			t.Fatalf("DownscaleRGBA failed: %v", err)
		}
		if len(dst) != dstW*dstH*4 {
			t.Fatalf("unexpected dst length: %d", len(dst))
		}
		// Check that color averages/interpolations remain within expected color range
		if dst[0] != 255 || dst[1] != 128 || dst[2] != 64 {
			t.Fatalf("unexpected downscaled pixel color: %v", dst[:4])
		}
	})

	t.Run("downscale image.Image", func(t *testing.T) {
		img := image.NewRGBA(image.Rect(0, 0, 100, 100))
		for y := 0; y < 100; y++ {
			for x := 0; x < 100; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
			}
		}

		scaled := DownscaleImage(img, 50, 50)
		if scaled.Rect.Dx() != 50 || scaled.Rect.Dy() != 50 {
			t.Fatalf("expected 50x50, got %dx%d", scaled.Rect.Dx(), scaled.Rect.Dy())
		}
	})
}

func TestThumbnailCache(t *testing.T) {
	cache := NewThumbnailCache(320, 180, 500*time.Millisecond)

	// DeviceID is required
	if err := cache.Set(&Thumbnail{}); err != ErrDeviceIDRequired {
		t.Fatalf("expected ErrDeviceIDRequired, got %v", err)
	}

	// Create test PNG image
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("png encode failed: %v", err)
	}

	thumb, err := cache.UpdateRaw("student-01", pngBuf.Bytes(), FormatPNG, 100, 100, 1)
	if err != nil {
		t.Fatalf("UpdateRaw failed: %v", err)
	}
	if thumb.DeviceID != "student-01" {
		t.Fatalf("expected student-01, got %s", thumb.DeviceID)
	}

	// Retrieve
	cached, ok := cache.Get("student-01")
	if !ok || cached.DeviceID != "student-01" {
		t.Fatalf("cache.Get failed")
	}

	// List
	list := cache.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 thumbnail in list, got %d", len(list))
	}

	// Delete
	cache.Delete("student-01")
	if _, ok := cache.Get("student-01"); ok {
		t.Fatalf("expected student-01 to be deleted")
	}

	// Clean expired
	_ = cache.Set(&Thumbnail{
		DeviceID:  "old-device",
		Timestamp: time.Now().Add(-2 * time.Hour),
	})
	cleared := cache.CleanExpired(1 * time.Hour)
	if cleared != 1 {
		t.Fatalf("expected 1 expired thumbnail cleared, got %d", cleared)
	}
}

func TestMockInjector(t *testing.T) {
	inj := NewMockInjector()

	ev1 := InputEvent{
		Type:       EventMouseMove,
		X:          0.5,
		Y:          0.5,
		Normalized: true,
	}
	ev2 := InputEvent{
		Type:    EventKeyDown,
		KeyCode: 0x41,
	}

	if err := inj.Inject(ev1); err != nil {
		t.Fatalf("Inject ev1 failed: %v", err)
	}
	if err := inj.Inject(ev2); err != nil {
		t.Fatalf("Inject ev2 failed: %v", err)
	}

	events := inj.InjectedEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 injected events, got %d", len(events))
	}

	inj.Clear()
	if len(inj.InjectedEvents()) != 0 {
		t.Fatalf("expected 0 events after Clear")
	}
}

func TestThumbnailCacheConcurrency(t *testing.T) {
	cache := NewThumbnailCache(320, 180, 10*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		id := "student-device"
		go func() {
			defer wg.Done()
			_ = cache.Set(&Thumbnail{
				DeviceID:  id,
				Width:     160,
				Height:    90,
				Timestamp: time.Now(),
			})
		}()
		go func() {
			defer wg.Done()
			_, _ = cache.Get(id)
			_ = cache.List()
		}()
	}
	wg.Wait()
}

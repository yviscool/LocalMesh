package screen

import (
	"image"
	"testing"
)

func TestCaptureScreen(t *testing.T) {
	img, err := CaptureScreen()
	if err != nil {
		t.Fatalf("CaptureScreen failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image from CaptureScreen")
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Fatalf("invalid captured dimensions: %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestCaptureScreenBounds(t *testing.T) {
	rect := image.Rect(0, 0, 320, 240)
	img, err := CaptureScreenBounds(rect)
	if err != nil {
		t.Fatalf("CaptureScreenBounds failed: %v", err)
	}
	if img.Bounds().Dx() != 320 || img.Bounds().Dy() != 240 {
		t.Fatalf("expected 320x240, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}

	// Invalid rect
	_, err = CaptureScreenBounds(image.Rectangle{Min: image.Pt(100, 100), Max: image.Pt(50, 50)})
	if err == nil {
		t.Error("expected error for inverted bounds")
	}
}

func TestGetVirtualScreenBounds(t *testing.T) {
	bounds, err := GetVirtualScreenBounds()
	if err != nil {
		t.Fatalf("GetVirtualScreenBounds failed: %v", err)
	}
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Fatalf("invalid virtual screen bounds: %v", bounds)
	}
}

func TestCaptureThumbnailIntegration(t *testing.T) {
	img, err := CaptureScreenBounds(image.Rect(0, 0, 640, 480))
	if err != nil {
		t.Fatalf("CaptureScreenBounds failed: %v", err)
	}

	cache := NewThumbnailCache(160, 120, 0)
	thumb, err := cache.UpdateImage("TEST-DEVICE-01", img, 1)
	if err != nil {
		t.Fatalf("cache.UpdateImage failed: %v", err)
	}
	if thumb.Width > 160 || thumb.Height > 120 {
		t.Fatalf("expected thumbnail within 160x120, got %dx%d", thumb.Width, thumb.Height)
	}
	if len(thumb.Data) == 0 {
		t.Fatal("expected encoded JPEG thumbnail data")
	}
}

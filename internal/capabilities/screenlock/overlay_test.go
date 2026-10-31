package screenlock

import (
	"testing"
	"time"
)

func TestOverlayLifecycle(t *testing.T) {
	if IsOverlayVisible() {
		t.Fatal("expected overlay initially not visible")
	}

	// 1. Show overlay
	err := ShowOverlay("Attention Students", "The teacher has locked your screen.")
	if err != nil {
		t.Fatalf("ShowOverlay failed: %v", err)
	}

	if !IsOverlayVisible() {
		t.Fatal("expected overlay to be visible after ShowOverlay")
	}

	// 2. Update title and message while visible
	err = UpdateOverlay("Eyes on the Board", "New assignment starting now.")
	if err != nil {
		t.Fatalf("UpdateOverlay failed: %v", err)
	}

	if !IsOverlayVisible() {
		t.Fatal("expected overlay still visible after UpdateOverlay")
	}

	time.Sleep(50 * time.Millisecond)

	// 3. Hide overlay
	err = HideOverlay()
	if err != nil {
		t.Fatalf("HideOverlay failed: %v", err)
	}

	if IsOverlayVisible() {
		t.Fatal("expected overlay to not be visible after HideOverlay")
	}

	// 4. Destroy overlay idempotent
	err = DestroyOverlay()
	if err != nil {
		t.Fatalf("DestroyOverlay failed: %v", err)
	}
}

func TestOverlayRapidToggle(t *testing.T) {
	for i := 0; i < 3; i++ {
		if err := ShowOverlay("Rapid Test", "Toggling..."); err != nil {
			t.Fatalf("ShowOverlay [%d] failed: %v", i, err)
		}
		if !IsOverlayVisible() {
			t.Fatalf("expected visible at iteration %d", i)
		}
		if err := HideOverlay(); err != nil {
			t.Fatalf("HideOverlay [%d] failed: %v", i, err)
		}
		if IsOverlayVisible() {
			t.Fatalf("expected hidden at iteration %d", i)
		}
	}
}

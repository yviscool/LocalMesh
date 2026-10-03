package session

import (
	"context"
	"testing"
	"time"
)

func TestBackoffCapsDelay(t *testing.T) {
	b := Backoff{Initial: time.Second, Maximum: 4 * time.Second}
	if got := b.Delay(1); got != time.Second {
		t.Fatalf("Delay(1) = %s, want 1s", got)
	}
	if got := b.Delay(3); got != 4*time.Second {
		t.Fatalf("Delay(3) = %s, want 4s", got)
	}
	if got := b.Delay(8); got != 4*time.Second {
		t.Fatalf("Delay(8) = %s, want 4s", got)
	}
}

func TestBackoffWaitCanBeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (Backoff{Initial: time.Hour, Maximum: time.Hour}).Wait(ctx, 1); err != context.Canceled {
		t.Fatalf("Wait() error = %v, want context.Canceled", err)
	}
}

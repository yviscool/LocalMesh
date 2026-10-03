package session

import (
	"context"
	"time"
)

type Backoff struct {
	Initial time.Duration
	Maximum time.Duration
}

func (b Backoff) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := b.Initial
	for i := 1; i < attempt && delay < b.Maximum; i++ {
		delay *= 2
	}
	if delay > b.Maximum {
		return b.Maximum
	}
	return delay
}

func (b Backoff) Wait(ctx context.Context, attempt int) error {
	timer := time.NewTimer(b.Delay(attempt))
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

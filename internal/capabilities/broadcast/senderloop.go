package broadcast

import (
	"context"
	"sync"
	"time"
)

// FrameProvider supplies encoded JPEG frames to the capture loop.
// Implementations wrap a screen capturer plus a JPEG encoder; changed=false
// means the screen is static (the loop skips sending).
type FrameProvider interface {
	CaptureJPEG(quality int) (payload []byte, width, height int, changed bool, err error)
}

// RateLimiter is a byte-token bucket used to pace multicast bursts.
type RateLimiter struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64 // bytes per second
	last   time.Time
}

// NewRateLimiter creates a bucket refilled at bytesPerSecond with the given
// burst capacity. Non-positive rate disables limiting (Take returns at once).
func NewRateLimiter(bytesPerSecond, burstBytes int) *RateLimiter {
	if bytesPerSecond <= 0 || burstBytes <= 0 {
		return nil
	}
	return &RateLimiter{
		tokens: float64(burstBytes),
		max:    float64(burstBytes),
		rate:   float64(bytesPerSecond),
		last:   time.Now(),
	}
}

// Take blocks until n bytes of budget are available or ctx is done.
func (r *RateLimiter) Take(ctx context.Context, n int) error {
	if r == nil {
		return nil
	}
	for {
		r.mu.Lock()
		now := time.Now()
		r.tokens += now.Sub(r.last).Seconds() * r.rate
		if r.tokens > r.max {
			r.tokens = r.max
		}
		r.last = now
		if r.tokens >= float64(n) {
			r.tokens -= float64(n)
			r.mu.Unlock()
			return nil
		}
		need := (float64(n) - r.tokens) / r.rate
		r.mu.Unlock()

		wait := time.Duration(need * float64(time.Second))
		if wait > 100*time.Millisecond {
			wait = 100 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// LoopOptions configures the capture loop. FPS/Quality are the tier-0
// starting point and get overridden by loss adaptation.
type LoopOptions struct {
	SourceID   string
	FPS        int
	Quality    int
	MinFPS     int
	MinQuality int
}

// quality tier ladder: fps + jpeg quality, descending under loss.
type qualityTier struct {
	fps     int
	quality int
}

var qualityTiers = []qualityTier{{15, 70}, {12, 55}, {10, 40}, {8, 30}}

// LoopStats is the adaptive feedback snapshot of a running capture loop.
type LoopStats struct {
	FramesSent    uint64  `json:"frames_sent"`
	FramesSkipped uint64  `json:"frames_skipped"`
	BytesSent     uint64  `json:"bytes_sent"`
	FPS           int     `json:"fps"`
	Quality       int     `json:"quality"`
	LossRate      float64 `json:"loss_rate"`
	Tier          int     `json:"tier"`
}

// CaptureSendLoop drives a FrameProvider → MulticastSender pipeline with
// static-frame skipping, token-bucket pacing and loss-adaptive quality.
type CaptureSendLoop struct {
	mu       sync.Mutex
	opts     LoopOptions
	provider FrameProvider
	sender   *MulticastSender
	extra    []*MulticastSender // 分组扇出：同一帧写多个地址
	limiter  *RateLimiter

	tier      int
	goodSince time.Time
	lossRate  float64

	framesSent uint64
	skipped    uint64
	bytesSent  uint64
}

// NewCaptureSendLoop wires the loop; budgetBytesPerSec 0 disables pacing.
func NewCaptureSendLoop(opts LoopOptions, provider FrameProvider, sender *MulticastSender, budgetBytesPerSec int) *CaptureSendLoop {
	if opts.FPS <= 0 {
		opts.FPS = qualityTiers[0].fps
	}
	if opts.Quality <= 0 {
		opts.Quality = qualityTiers[0].quality
	}
	if opts.MinFPS <= 0 {
		opts.MinFPS = qualityTiers[len(qualityTiers)-1].fps
	}
	return &CaptureSendLoop{
		opts:     opts,
		provider: provider,
		sender:   sender,
		limiter:  NewRateLimiter(budgetBytesPerSec, budgetBytesPerSec/2),
	}
}

// Run blocks until ctx is done, producing and multicasting frames.
func (l *CaptureSendLoop) Run(ctx context.Context) {
	var seq uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(l.tickInterval()):
		}

		quality := l.currentQuality()
		payload, w, h, changed, err := l.provider.CaptureJPEG(quality)
		if err != nil {
			continue
		}
		if !changed {
			l.mu.Lock()
			l.skipped++
			l.mu.Unlock()
			continue
		}
		if err := l.limiter.Take(ctx, len(payload)); err != nil {
			return
		}

		seq++
		frame := ScreenFrame{
			Sequence:       seq,
			Timestamp:      time.Now(),
			SourceDeviceID: l.opts.SourceID,
			Width:          w,
			Height:         h,
			Payload:        payload,
		}
		if err := l.sender.SendFrame(ctx, frame); err != nil && ctx.Err() != nil {
			return
		}
		for _, tx := range l.extra {
			if err := tx.SendFrame(ctx, frame); err != nil && ctx.Err() != nil {
				return
			}
		}

		l.mu.Lock()
		l.framesSent++
		l.bytesSent += uint64(len(payload))
		l.mu.Unlock()
	}
}

func (l *CaptureSendLoop) tickInterval() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return time.Second / time.Duration(l.opts.FPS)
}

func (l *CaptureSendLoop) currentQuality() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.opts.Quality
}

// ReportLoss feeds the receiver's measured loss rate back into the loop:
// sustained loss steps quality down, sustained clarity steps it up.
func (l *CaptureSendLoop) ReportLoss(rate float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lossRate = rate

	if rate > 0.10 {
		if l.tier < len(qualityTiers)-1 {
			l.tier++
			l.applyTierLocked()
		}
		l.goodSince = time.Time{}
		return
	}
	if rate < 0.02 {
		if l.goodSince.IsZero() {
			l.goodSince = time.Now()
		}
		if time.Since(l.goodSince) > 20*time.Second && l.tier > 0 {
			l.tier--
			l.applyTierLocked()
			l.goodSince = time.Now()
		}
	} else {
		l.goodSince = time.Time{}
	}
}

func (l *CaptureSendLoop) applyTierLocked() {
	t := qualityTiers[l.tier]
	l.opts.FPS = t.fps
	l.opts.Quality = t.quality
}

// AttachSender adds a fan-out destination (per-group multicast).
func (l *CaptureSendLoop) AttachSender(tx *MulticastSender) {
	l.mu.Lock()
	l.extra = append(l.extra, tx)
	l.mu.Unlock()
}

// DetachSender removes a fan-out destination.
func (l *CaptureSendLoop) DetachSender(tx *MulticastSender) {
	l.mu.Lock()
	for i, s := range l.extra {
		if s == tx {
			l.extra = append(l.extra[:i], l.extra[i+1:]...)
			break
		}
	}
	l.mu.Unlock()
}

// Stats snapshots the loop counters.
func (l *CaptureSendLoop) Stats() LoopStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return LoopStats{
		FramesSent: l.framesSent, FramesSkipped: l.skipped, BytesSent: l.bytesSent,
		FPS: l.opts.FPS, Quality: l.opts.Quality, LossRate: l.lossRate, Tier: l.tier,
	}
}

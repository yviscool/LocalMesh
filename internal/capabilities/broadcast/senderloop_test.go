package broadcast

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
	"testing"
	"time"
)

// loopbackMulticastPair wires a sender and receiver over a real UDP
// multicast socket bound to the loopback interface.
func loopbackMulticastPair(t *testing.T) (*MulticastSender, *MulticastReceiver) {
	t.Helper()
	addr := "127.0.0.1:8878"
	sender, err := NewMulticastSender(MulticastSenderConfig{TargetAddr: addr, FEC: DefaultFECConfig()})
	if err != nil {
		t.Fatalf("sender: %v", err)
	}
	receiver, err := NewMulticastReceiver(MulticastReceiverConfig{ListenAddr: addr, ReassemblerTimeout: time.Second})
	if err != nil {
		t.Fatalf("receiver: %v", err)
	}
	t.Cleanup(func() {
		_ = sender.Close()
		_ = receiver.Close()
	})
	return sender, receiver
}

// fakeProvider yields animated frames (changed=true) or static frames.
type fakeProvider struct {
	mu       sync.Mutex
	n        int
	animate  bool
	everSent bool
}

func (p *fakeProvider) CaptureJPEG(quality int) ([]byte, int, int, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	changed := p.animate
	if !p.everSent {
		changed = true // 首帧必须发出
		p.everSent = true
	}
	if p.animate {
		p.n++
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(p.n, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, 0, 0, false, err
	}
	return buf.Bytes(), 8, 8, changed, nil
}

// TestRateLimiterPaces verifies the token bucket actually delays bursts.
func TestRateLimiterPaces(t *testing.T) {
	// 1000 B/s, 1500 B burst → three 1000-byte takes need ~1.5s total.
	limiter := NewRateLimiter(1000, 1500)
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := limiter.Take(context.Background(), 1000); err != nil {
			t.Fatalf("take: %v", err)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 1200*time.Millisecond {
		t.Fatalf("limiter did not pace: elapsed %v", elapsed)
	}
}

// TestRateLimiterNilAllowsAll documents the disabled-limiter shortcut.
func TestRateLimiterNilAllowsAll(t *testing.T) {
	var limiter *RateLimiter
	start := time.Now()
	for i := 0; i < 1000; i++ {
		_ = limiter.Take(context.Background(), 9999)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("nil limiter must never block")
	}
}

// TestCaptureSendLoopAdaptsToLoss drives the loop with a loopback pair and
// asserts the loss feedback steps the quality tier down.
func TestCaptureSendLoopAdaptsToLoss(t *testing.T) {
	addr := "127.0.0.1:8877"
	sender, err := NewMulticastSender(MulticastSenderConfig{TargetAddr: addr, FEC: DefaultFECConfig()})
	if err != nil {
		t.Fatalf("sender: %v", err)
	}
	defer sender.Close()

	provider := &fakeProvider{animate: true}
	loop := NewCaptureSendLoop(LoopOptions{SourceID: "teacher-test", FPS: 30, Quality: 70}, provider, sender, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go loop.Run(ctx)

	// 立刻注入高丢包率反馈
	loop.ReportLoss(0.5)
	loop.ReportLoss(0.5)

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if loop.Stats().Quality <= 55 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	stats := loop.Stats()
	if stats.Quality > 55 {
		t.Fatalf("quality did not adapt down: %+v", stats)
	}
	if stats.LossRate < 0.5 {
		t.Fatalf("loss rate not recorded: %+v", stats)
	}
}

// TestCaptureSendLoopSkipsStaticFrames verifies changed=false frames are
// counted as skipped and not sent.
func TestCaptureSendLoopSkipsStaticFrames(t *testing.T) {
	sender, receiver := loopbackMulticastPair(t)
	provider := &fakeProvider{animate: false} // 永不变化
	loop := NewCaptureSendLoop(LoopOptions{SourceID: "teacher-test", FPS: 30}, provider, sender, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	go loop.Run(ctx)

	frameCh, errCh := receiver.Start(ctx)
	received := 0
	deadline := time.After(1100 * time.Millisecond)
collect:
	for {
		select {
		case frame := <-frameCh:
			if frame != nil {
				received++
			}
		case err := <-errCh:
			t.Fatalf("receiver error: %v", err)
		case <-deadline:
			break collect
		}
	}
	if received != 1 {
		t.Fatalf("expected exactly the initial frame, got %d", received)
	}
	if loop.Stats().FramesSkipped == 0 {
		t.Fatal("static frames must be counted as skipped")
	}
}

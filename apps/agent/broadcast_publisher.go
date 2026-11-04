package main

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"log"
	"sync"

	"localmesh/internal/capabilities/broadcast"
	"localmesh/internal/capabilities/screen"
)

// broadcastPublisher lets a student machine publish its own screen onto a
// multicast address — used for 学生演示 (direct push, no teacher relay).
type broadcastPublisher struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	sender *broadcast.MulticastSender
	addr   string
}

// start begins publishing the local screen to addr.
func (p *broadcastPublisher) start(addr string) error {
	p.mu.Lock()
	if p.addr == addr && p.cancel != nil {
		p.mu.Unlock()
		return nil
	}
	p.stopLocked()
	sender, err := broadcast.NewMulticastSender(broadcast.MulticastSenderConfig{
		TargetAddr: addr,
		FEC:        broadcast.DefaultFECConfig(),
	})
	if err != nil {
		p.mu.Unlock()
		return err
	}
	provider, err := screen.NewHybridCapturer()
	if err != nil {
		_ = sender.Close()
		p.mu.Unlock()
		return fmt.Errorf("capturer: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	loop := broadcast.NewCaptureSendLoop(broadcast.LoopOptions{
		SourceID: "demo-student", FPS: 15, Quality: 65,
	}, &demoProvider{capturer: provider}, sender, 0)

	p.cancel, p.sender, p.addr = cancel, sender, addr
	p.mu.Unlock()

	go loop.Run(ctx)
	log.Printf("[Demo] publishing screen to %s", addr)
	return nil
}

// stop halts publishing.
func (p *broadcastPublisher) stop() {
	p.mu.Lock()
	p.stopLocked()
	p.mu.Unlock()
}

func (p *broadcastPublisher) stopLocked() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	if p.sender != nil {
		_ = p.sender.Close()
		p.sender = nil
	}
	p.addr = ""
}

// demoProvider adapts the capturer to the shared broadcast loop.
type demoProvider struct{ capturer screen.HybridCapturer }

func (p *demoProvider) CaptureJPEG(quality int) ([]byte, int, int, bool, error) {
	frame, err := p.capturer.Capture()
	if err != nil {
		if err == screen.ErrNoNewFrame {
			return nil, 0, 0, false, nil
		}
		return nil, 0, 0, false, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, frame.Image, &jpeg.Options{Quality: quality}); err != nil {
		return nil, 0, 0, false, err
	}
	return buf.Bytes(), frame.Width, frame.Height, true, nil
}

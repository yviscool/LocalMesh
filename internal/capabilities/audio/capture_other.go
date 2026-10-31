//go:build !windows

package audio

import (
	"context"
	"math"
	"sync"
	"time"
)

// MockCapturer generates synthetic audio on non-Windows platforms.
type MockCapturer struct {
	mu        sync.Mutex
	format    AudioFormat
	running   bool
	stopCh    chan struct{}
	frequency float64 // Audio test tone in Hz, 0 for silence
}

// NewCapturer returns a platform capturer (Mock on non-Windows platforms).
func NewCapturer(fmt AudioFormat) Capturer {
	if err := fmt.Validate(); err != nil {
		fmt = DefaultAudioFormat
	}
	return &MockCapturer{
		format:    fmt,
		frequency: 440.0, // A4 tone
	}
}

// NewSilentCapturer returns a mock capturer that produces continuous silence.
func NewSilentCapturer(fmt AudioFormat) Capturer {
	if err := fmt.Validate(); err != nil {
		fmt = DefaultAudioFormat
	}
	return &MockCapturer{
		format:    fmt,
		frequency: 0,
	}
}

func (c *MockCapturer) Format() AudioFormat {
	return c.format
}

// Start begins generating synthetic audio frames and sending them to the handler.
func (c *MockCapturer) Start(ctx context.Context, handler AudioHandler) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	frameDur := DefaultFrameDuration
	samplesPerFrame := int(float64(c.format.SampleRate) * frameDur.Seconds())
	frameBytes := samplesPerFrame * c.format.FrameSize()

	go func() {
		ticker := time.NewTicker(frameDur)
		defer ticker.Stop()

		var sampleIndex int64
		buf := make([]byte, frameBytes)

		for {
			select {
			case <-c.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if c.frequency > 0 {
					// Generate 16-bit PCM sine wave
					for i := 0; i < samplesPerFrame; i++ {
						t := float64(sampleIndex) / float64(c.format.SampleRate)
						val := int16(math.Sin(2*math.Pi*c.frequency*t) * 16000)
						sampleIndex++

						for ch := 0; ch < c.format.Channels; ch++ {
							offset := (i*c.format.Channels + ch) * 2
							buf[offset] = byte(val)
							buf[offset+1] = byte(val >> 8)
						}
					}
				} else {
					// Silence
					for j := range buf {
						buf[j] = 0
					}
				}
				handler(buf)
			}
		}
	}()

	return nil
}

// Stop terminates synthetic audio generation.
func (c *MockCapturer) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}
	c.running = false
	close(c.stopCh)
	return nil
}

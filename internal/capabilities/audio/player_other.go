//go:build !windows

package audio

import (
	"context"
	"sync"
	"time"
)

// MockPlayer consumes audio samples on non-Windows platforms.
type MockPlayer struct {
	mu           sync.Mutex
	format       AudioFormat
	running      bool
	bytesWritten uint64
	lastWrite    time.Time
}

// NewPlayer returns a platform player (Mock on non-Windows platforms).
func NewPlayer(fmt AudioFormat) Player {
	if err := fmt.Validate(); err != nil {
		fmt = DefaultAudioFormat
	}
	return &MockPlayer{
		format: fmt,
	}
}

func (p *MockPlayer) Format() AudioFormat {
	return p.format
}

func (p *MockPlayer) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = true
	p.lastWrite = time.Now()
	return nil
}

func (p *MockPlayer) Write(pcm []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.bytesWritten += uint64(len(pcm))
	p.lastWrite = time.Now()
	return len(pcm), nil
}

func (p *MockPlayer) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = false
	return nil
}

func (p *MockPlayer) BufferLatency() time.Duration {
	return 10 * time.Millisecond
}

// BytesWritten returns total bytes written to the mock player.
func (p *MockPlayer) BytesWritten() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bytesWritten
}

//go:build !windows

package screenlock

import (
	"sync"
)

// PlatformTrapper is a stub trapper for non-Windows systems.
type PlatformTrapper struct {
	mu        sync.RWMutex
	trapping  bool
	inputOnly bool
}

// NewPlatformTrapper returns a new platform-specific trapper.
func NewPlatformTrapper() Trapper {
	return &PlatformTrapper{}
}

// Trap sets trapping state.
func (p *PlatformTrapper) Trap(inputOnly bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trapping = true
	p.inputOnly = inputOnly
	return nil
}

// Untrap clears trapping state.
func (p *PlatformTrapper) Untrap() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trapping = false
	p.inputOnly = false
	return nil
}

// IsTrapping returns whether trapping is active.
func (p *PlatformTrapper) IsTrapping() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.trapping
}

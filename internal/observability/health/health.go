package health

import (
	"context"
	"sync"
	"time"
)

type Status string

const (
	StatusHealthy  Status = "healthy"
	StatusDegraded Status = "degraded"
	StatusFailed   Status = "failed"
)

type Check struct {
	Name      string
	Status    Status
	Detail    string
	CheckedAt time.Time
}
type Probe interface{ Check(context.Context) Check }

type Registry struct {
	mu     sync.RWMutex
	probes []Probe
}

func New() *Registry { return &Registry{} }
func (r *Registry) Register(p Probe) {
	if p == nil {
		return
	}
	r.mu.Lock()
	r.probes = append(r.probes, p)
	r.mu.Unlock()
}
func (r *Registry) Check(ctx context.Context) []Check {
	r.mu.RLock()
	probes := append([]Probe(nil), r.probes...)
	r.mu.RUnlock()
	out := make([]Check, 0, len(probes))
	for _, p := range probes {
		value := p.Check(ctx)
		if value.CheckedAt.IsZero() {
			value.CheckedAt = time.Now().UTC()
		}
		out = append(out, value)
	}
	return out
}
func Overall(checks []Check) Status {
	for _, c := range checks {
		if c.Status == StatusFailed {
			return StatusFailed
		}
	}
	for _, c := range checks {
		if c.Status == StatusDegraded {
			return StatusDegraded
		}
	}
	return StatusHealthy
}

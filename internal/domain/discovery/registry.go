package discovery

import (
	"sort"
	"sync"
	"time"
)

// Registry stores short-lived network observations. It does not store membership or capabilities.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]Observation
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]Observation)}
}

func (r *Registry) Observe(observation Observation) {
	if observation.DeviceID == "" {
		return
	}
	r.mu.Lock()
	r.entries[string(observation.DeviceID)] = observation
	r.mu.Unlock()
}

func (r *Registry) Candidates(now time.Time, maxAge time.Duration) []Observation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Observation, 0, len(r.entries))
	for _, observation := range r.entries {
		if observation.Candidate() && maxAge >= 0 && now.Sub(observation.SeenAt) <= maxAge {
			result = append(result, observation)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DeviceID < result[j].DeviceID })
	return result
}

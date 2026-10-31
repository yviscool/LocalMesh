package screen

import (
	"sync"
)

// MockInjector records input injection events for testing or simulation.
type MockInjector struct {
	mu     sync.Mutex
	events []InputEvent
}

// NewMockInjector returns an injector that records events in memory.
func NewMockInjector() *MockInjector {
	return &MockInjector{
		events: make([]InputEvent, 0),
	}
}

// Inject stores the event in memory after validation.
func (m *MockInjector) Inject(event InputEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.events = append(m.events, event)
	return nil
}

// InjectedEvents returns a copy of all recorded events.
func (m *MockInjector) InjectedEvents() []InputEvent {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]InputEvent, len(m.events))
	copy(out, m.events)
	return out
}

// Clear clears recorded events.
func (m *MockInjector) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = m.events[:0]
}

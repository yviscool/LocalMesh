package idempotency

import "sync"

type Status string

const (
	StatusNew      Status = "new"
	StatusReplayed Status = "replayed"
	StatusConflict Status = "conflict"
)

type Result struct {
	Status       Status
	OriginalData []byte
}

type record struct {
	fingerprint string
	data        []byte
}

// Store makes command retries safe without coupling execution to a transport.
type Store struct {
	mu      sync.Mutex
	entries map[string]record
}

func NewStore() *Store {
	return &Store{entries: make(map[string]record)}
}

func (s *Store) Begin(key, fingerprint string) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.entries[key]; ok {
		if existing.fingerprint != fingerprint {
			return Result{Status: StatusConflict}
		}
		return Result{Status: StatusReplayed, OriginalData: append([]byte(nil), existing.data...)}
	}
	s.entries[key] = record{fingerprint: fingerprint}
	return Result{Status: StatusNew}
}

func (s *Store) Complete(key, fingerprint string, data []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.entries[key]
	if !ok || existing.fingerprint != fingerprint {
		return false
	}
	existing.data = append([]byte(nil), data...)
	s.entries[key] = existing
	return true
}

// Abort releases a reservation when the side effect did not happen.
func (s *Store) Abort(key, fingerprint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.entries[key]
	if !ok || existing.fingerprint != fingerprint || len(existing.data) > 0 {
		return false
	}
	delete(s.entries, key)
	return true
}

package workers

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Worker interface {
	Run(context.Context) error
	Stop() error
}
type Factory func(string) (Worker, error)

type Supervisor struct {
	factory Factory
	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func New(factory Factory) *Supervisor {
	return &Supervisor{factory: factory, running: map[string]context.CancelFunc{}}
}
func (s *Supervisor) Start(ctx context.Context, id string) error {
	if s.factory == nil || id == "" {
		return errors.New("worker supervisor not configured")
	}
	worker, err := s.factory(id)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if old := s.running[id]; old != nil {
		old()
	}
	s.running[id] = cancel
	s.mu.Unlock()
	go func() { _ = worker.Run(runCtx); s.mu.Lock(); delete(s.running, id); s.mu.Unlock() }()
	return nil
}
func (s *Supervisor) Stop(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancel := s.running[id]
	if cancel == nil {
		return nil
	}
	cancel()
	delete(s.running, id)
	return nil
}
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.running {
		cancel()
		delete(s.running, id)
	}
}
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := time.Second
	for i := 1; i < attempt && d < time.Minute; i++ {
		d *= 2
	}
	if d > time.Minute {
		return time.Minute
	}
	return d
}

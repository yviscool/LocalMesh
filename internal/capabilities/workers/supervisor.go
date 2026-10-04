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
	factory    Factory
	mu         sync.Mutex
	running    map[string]record
	generation map[string]uint64
}

type record struct {
	cancel context.CancelFunc
	worker Worker
}

func New(factory Factory) *Supervisor {
	return &Supervisor{factory: factory, running: map[string]record{}, generation: map[string]uint64{}}
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
	if old, ok := s.running[id]; ok {
		old.cancel()
		_ = old.worker.Stop()
	}
	s.generation[id]++
	generation := s.generation[id]
	s.running[id] = record{cancel: cancel, worker: worker}
	s.mu.Unlock()
	go func() {
		_ = worker.Run(runCtx)
		s.mu.Lock()
		if s.generation[id] == generation {
			delete(s.running, id)
		}
		s.mu.Unlock()
	}()
	return nil
}

func (s *Supervisor) Running(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[id]
	return ok
}
func (s *Supervisor) Stop(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.running[id]
	if !ok {
		return nil
	}
	value.cancel()
	_ = value.worker.Stop()
	delete(s.running, id)
	return nil
}
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, value := range s.running {
		value.cancel()
		_ = value.worker.Stop()
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

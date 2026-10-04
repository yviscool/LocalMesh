package workers

import (
	"context"
	"testing"
	"time"
)

type fakeWorker struct{ done chan struct{} }

func (w *fakeWorker) Run(ctx context.Context) error { close(w.done); <-ctx.Done(); return ctx.Err() }
func (w *fakeWorker) Stop() error                   { return nil }

func TestSupervisorStartsAndStopsWorkers(t *testing.T) {
	w := &fakeWorker{done: make(chan struct{})}
	s := New(func(string) (Worker, error) { return w, nil })
	if err := s.Start(context.Background(), "screen.capture"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if err := s.Stop("screen.capture"); err != nil {
		t.Fatal(err)
	}
	if s.Running("screen.capture") {
		t.Fatal("worker still marked running after stop")
	}
}
func TestBackoffCaps(t *testing.T) {
	if Backoff(10) != time.Minute {
		t.Fatalf("backoff=%v", Backoff(10))
	}
}

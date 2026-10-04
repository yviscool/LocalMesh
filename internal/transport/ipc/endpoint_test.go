package ipc

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type blockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingListener() *blockingListener { return &blockingListener{closed: make(chan struct{})} }
func (l *blockingListener) Accept(ctx context.Context) (io.ReadWriteCloser, error) {
	select {
	case <-l.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (l *blockingListener) Close() error { l.once.Do(func() { close(l.closed) }); return nil }

func TestServeListenerStopsBlockedAcceptOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	listener := newBlockingListener()
	done := make(chan error, 1)
	go func() { done <- ServeListener(ctx, listener, handler{}, 1024) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ServeListener() = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeListener did not stop")
	}
}

func TestServeConnectionClosesBlockedReadOnCancellation(t *testing.T) {
	left, right := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveConnection(ctx, right, handler{}, 1024) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) && !errors.Is(err, context.Canceled) {
			t.Fatalf("serveConnection() = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serveConnection did not stop")
	}
	_ = left.Close()
}

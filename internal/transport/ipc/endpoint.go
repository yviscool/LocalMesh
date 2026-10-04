package ipc

import (
	"context"
	"errors"
	"io"
)

type Listener interface {
	Accept(context.Context) (io.ReadWriteCloser, error)
	Close() error
}

// ServeListener accepts one connection at a time and applies the same framed
// IPC handler used by every platform endpoint.
func ServeListener(ctx context.Context, listener Listener, handler Handler, max int) error {
	if listener == nil {
		return ErrListenerNotConfigured
	}
	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-closed:
		}
	}()
	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		err = serveConnection(ctx, conn, handler, max)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				continue
			}
			continue
		}
	}
}

func serveConnection(ctx context.Context, conn io.ReadWriteCloser, handler Handler, max int) error {
	if conn == nil {
		return io.ErrUnexpectedEOF
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	err := Serve(ctx, conn, handler, max)
	close(done)
	_ = conn.Close()
	return err
}

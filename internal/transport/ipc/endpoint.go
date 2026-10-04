package ipc

import (
	"context"
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
	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return err
		}
		err = Serve(ctx, conn, handler, max)
		_ = conn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			continue
		}
	}
}

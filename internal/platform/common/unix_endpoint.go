//go:build !windows

package common

import (
	"context"
	"errors"
	"io"
	"net"
	"os"

	"localmesh/internal/transport/ipc"
)

var ErrInvalidSocketPath = errors.New("invalid unix socket path")

type UnixEndpoint struct{}

func (UnixEndpoint) Listen(_ context.Context, address string) (ipc.Listener, error) {
	if address == "" {
		return nil, ErrInvalidSocketPath
	}
	_ = os.Remove(address)
	listener, err := net.Listen("unix", address)
	if err != nil {
		return nil, err
	}
	return unixListener{listener: listener}, nil
}

func (UnixEndpoint) Dial(ctx context.Context, address string) (io.ReadWriteCloser, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", address)
}

type unixListener struct{ listener net.Listener }

func (l unixListener) Accept(ctx context.Context) (io.ReadWriteCloser, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = l.listener.(*net.UnixListener).SetDeadline(deadline)
	}
	return l.listener.Accept()
}
func (l unixListener) Close() error { return l.listener.Close() }

var _ ipc.Listener = unixListener{}

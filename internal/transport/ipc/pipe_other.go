//go:build !windows

package ipc

import (
	"context"
	"errors"
	"io"
)

var ErrNamedPipeUnsupported = errors.New("named pipes are only available on Windows")

func ListenNamedPipe(string) (Listener, error) { return nil, ErrNamedPipeUnsupported }

type memoryListener struct{ conn io.ReadWriteCloser }

func (l *memoryListener) Accept(context.Context) (io.ReadWriteCloser, error) {
	if l.conn == nil {
		return nil, io.EOF
	}
	c := l.conn
	l.conn = nil
	return c, nil
}
func (l *memoryListener) Close() error { return nil }

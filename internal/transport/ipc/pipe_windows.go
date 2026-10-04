//go:build windows

package ipc

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

type namedPipeListener struct {
	name   string
	handle windows.Handle
	closed bool
}

func ListenNamedPipe(name string) (Listener, error) {
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateNamedPipe(path, windows.PIPE_ACCESS_DUPLEX, windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT, windows.PIPE_UNLIMITED_INSTANCES, DefaultMaxMessage, DefaultMaxMessage, 0, nil)
	if err != nil {
		return nil, err
	}
	return &namedPipeListener{name: name, handle: handle}, nil
}

func (l *namedPipeListener) Accept(ctx context.Context) (io.ReadWriteCloser, error) {
	if l.closed {
		return nil, io.EOF
	}
	done := make(chan error, 1)
	go func() { done <- windows.ConnectNamedPipe(l.handle, nil) }()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-done:
		if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
			return nil, err
		}
		return os.NewFile(uintptr(l.handle), l.name), nil
	}
}

func (l *namedPipeListener) Close() error { l.closed = true; return windows.CloseHandle(l.handle) }

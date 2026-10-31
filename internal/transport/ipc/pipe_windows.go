//go:build windows

package ipc

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// DefaultNamedPipeSDDL grants Generic All (GA) access to SYSTEM (SY),
	// Built-in Administrators (BA), and Interactive Users (IU).
	DefaultNamedPipeSDDL = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;IU)"
)

type namedPipeListener struct {
	name      string
	path      *uint16
	sa        *windows.SecurityAttributes
	mu        sync.Mutex
	closed    bool
	closeChan chan struct{}
	curHandle windows.Handle
	curOver   *windows.Overlapped
}

func normalizePipeName(name string) string {
	if !strings.HasPrefix(name, `\\.\pipe\`) && !strings.HasPrefix(name, `\\?\pipe\`) {
		name = strings.TrimPrefix(name, `\\.\`)
		name = strings.TrimPrefix(name, `\`)
		return `\\.\pipe\` + name
	}
	return name
}

// ListenNamedPipe creates a Windows Named Pipe listener with restricted ACLs.
func ListenNamedPipe(name string) (Listener, error) {
	normName := normalizePipeName(name)
	path, err := windows.UTF16PtrFromString(normName)
	if err != nil {
		return nil, fmt.Errorf("invalid pipe name %q: %w", normName, err)
	}

	sd, err := windows.SecurityDescriptorFromString(DefaultNamedPipeSDDL)
	if err != nil {
		return nil, fmt.Errorf("failed to create security descriptor: %w", err)
	}

	var sa windows.SecurityAttributes
	sa.Length = uint32(unsafe.Sizeof(sa))
	sa.SecurityDescriptor = sd

	l := &namedPipeListener{
		name:      normName,
		path:      path,
		sa:        &sa,
		closeChan: make(chan struct{}),
	}

	// Create initial pipe instance so the pipe immediately exists in Windows namespace.
	h, err := l.createInstance()
	if err != nil {
		return nil, fmt.Errorf("failed to create initial named pipe instance: %w", err)
	}
	l.curHandle = h

	return l, nil
}

func (l *namedPipeListener) createInstance() (windows.Handle, error) {
	return windows.CreateNamedPipe(
		l.path,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT,
		windows.PIPE_UNLIMITED_INSTANCES,
		DefaultMaxMessage,
		DefaultMaxMessage,
		0,
		l.sa,
	)
}

func (l *namedPipeListener) Accept(ctx context.Context) (io.ReadWriteCloser, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, io.EOF
	}

	h := l.curHandle
	if h == 0 || h == windows.InvalidHandle {
		var err error
		h, err = l.createInstance()
		if err != nil {
			l.mu.Unlock()
			return nil, err
		}
		l.curHandle = h
	}

	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		l.mu.Unlock()
		return nil, err
	}
	defer windows.CloseHandle(event)

	var o windows.Overlapped
	o.HEvent = event
	l.curOver = &o
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.curOver = nil
		l.mu.Unlock()
	}()

	err = windows.ConnectNamedPipe(h, &o)
	var connected bool
	switch err {
	case nil, windows.ERROR_PIPE_CONNECTED:
		connected = true
	case windows.ERROR_IO_PENDING:
		connected = false
	default:
		l.mu.Lock()
		windows.CloseHandle(h)
		l.curHandle = 0
		l.mu.Unlock()
		return nil, err
	}

	if !connected {
		waitDone := make(chan error, 1)
		go func() {
			res, werr := windows.WaitForSingleObject(event, windows.INFINITE)
			if werr != nil {
				waitDone <- werr
				return
			}
			if res != windows.WAIT_OBJECT_0 {
				waitDone <- fmt.Errorf("unexpected wait result: %d", res)
				return
			}
			waitDone <- nil
		}()

		select {
		case <-ctx.Done():
			_ = windows.CancelIoEx(h, &o)
			<-waitDone
			l.mu.Lock()
			windows.CloseHandle(h)
			l.curHandle = 0
			if !l.closed {
				if nextH, err := l.createInstance(); err == nil {
					l.curHandle = nextH
				}
			}
			l.mu.Unlock()
			return nil, ctx.Err()
		case <-l.closeChan:
			_ = windows.CancelIoEx(h, &o)
			<-waitDone
			l.mu.Lock()
			windows.CloseHandle(h)
			l.curHandle = 0
			l.mu.Unlock()
			return nil, io.EOF
		case werr := <-waitDone:
			if werr != nil {
				l.mu.Lock()
				windows.CloseHandle(h)
				l.curHandle = 0
				l.mu.Unlock()
				return nil, werr
			}
			var bytes uint32
			if err := windows.GetOverlappedResult(h, &o, &bytes, true); err != nil {
				l.mu.Lock()
				windows.CloseHandle(h)
				l.curHandle = 0
				l.mu.Unlock()
				return nil, err
			}
		}
	}

	// Instance connected. Hand off handle to caller.
	l.mu.Lock()
	l.curHandle = 0
	if !l.closed {
		if nextH, err := l.createInstance(); err == nil {
			l.curHandle = nextH
		}
	}
	l.mu.Unlock()

	return os.NewFile(uintptr(h), l.name), nil
}

func (l *namedPipeListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	close(l.closeChan)
	if l.curHandle != 0 && l.curOver != nil {
		_ = windows.CancelIoEx(l.curHandle, l.curOver)
	} else if l.curHandle != 0 {
		windows.CloseHandle(l.curHandle)
		l.curHandle = 0
	}
	return nil
}

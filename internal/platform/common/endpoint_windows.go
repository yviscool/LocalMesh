//go:build windows

package common

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"localmesh/internal/platform"
	"localmesh/internal/transport/ipc"
)

type unsupportedEndpoint struct{}

func (unsupportedEndpoint) Listen(context.Context, string) (ipc.Listener, error) {
	return nil, platform.ErrUnsupportedPlatform
}

func (unsupportedEndpoint) Dial(context.Context, string) (io.ReadWriteCloser, error) {
	return nil, platform.ErrUnsupportedPlatform
}

func defaultEndpoint() platform.LocalEndpoint { return namedPipeEndpoint{} }

type namedPipeEndpoint struct{}

func (namedPipeEndpoint) Listen(_ context.Context, address string) (ipc.Listener, error) {
	return ipc.ListenNamedPipe(address)
}

func (namedPipeEndpoint) Dial(ctx context.Context, address string) (io.ReadWriteCloser, error) {
	if !strings.HasPrefix(address, `\\.\pipe\`) && !strings.HasPrefix(address, `\\?\pipe\`) {
		address = `\\.\pipe\` + strings.TrimPrefix(strings.TrimPrefix(address, `\\.\`), `\`)
	}

	for {
		f, err := os.OpenFile(address, os.O_RDWR, 0)
		if err == nil {
			return f, nil
		}
		if errors.Is(err, windows.ERROR_PIPE_BUSY) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Millisecond):
				continue
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if _, hasDeadline := ctx.Deadline(); hasDeadline && errors.Is(err, os.ErrNotExist) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Millisecond):
				continue
			}
		}
		return nil, err
	}
}

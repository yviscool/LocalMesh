//go:build windows

package common

import (
	"context"
	"io"
	"os"

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
func (namedPipeEndpoint) Dial(_ context.Context, address string) (io.ReadWriteCloser, error) {
	return os.OpenFile(address, os.O_RDWR, 0)
}

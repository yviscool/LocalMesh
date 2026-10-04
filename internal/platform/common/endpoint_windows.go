//go:build windows

package common

import (
	"context"
	"io"

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
func defaultEndpoint() platform.LocalEndpoint { return unsupportedEndpoint{} }

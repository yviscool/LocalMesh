package common

import (
	"context"
	"runtime"

	"localmesh/internal/platform"
)

type FakeSession struct{ User string }

func (f FakeSession) CurrentUser(context.Context) (string, error)       { return f.User, nil }
func (f FakeSession) RunAsUser(context.Context, string, []string) error { return nil }

func RuntimeForCurrentOS() (platform.Runtime, error) {
	var os platform.OS
	switch runtime.GOOS {
	case "windows":
		os = platform.Windows
	case "linux":
		os = platform.Linux
	case "darwin":
		os = platform.Darwin
	default:
		return platform.Runtime{}, platform.ErrUnsupportedPlatform
	}
	service := NewFakeService()
	r := platform.Runtime{Ports: platform.Ports{OS: os, Service: service, Session: FakeSession{}, Endpoint: defaultEndpoint(), Capabilities: FakeCapabilities{SupportedNames: map[string]bool{}}}}
	if err := r.Validate(); err != nil {
		return platform.Runtime{}, err
	}
	return r, nil
}

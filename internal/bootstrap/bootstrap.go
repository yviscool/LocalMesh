package bootstrap

import (
	"errors"

	"localmesh/internal/capabilities/registry"
	"localmesh/internal/platform"
)

var ErrCapabilityStartup = errors.New("capability startup validation failed")

type Runtime struct {
	Platform     platform.Runtime
	Capabilities *registry.Registry
}

func Compose(platformRuntime platform.Runtime, capabilities *registry.Registry) (Runtime, error) {
	if err := platformRuntime.Validate(); err != nil {
		return Runtime{}, err
	}
	if capabilities == nil {
		return Runtime{}, ErrCapabilityStartup
	}
	if err := capabilities.ValidatePlatform(platformRuntime.OS); err != nil {
		return Runtime{}, err
	}
	return Runtime{Platform: platformRuntime, Capabilities: capabilities}, nil
}

package platform

import (
	"context"
	"errors"
)

type OS string

const (
	Windows OS = "windows"
	Linux   OS = "linux"
	Darwin  OS = "darwin"
)

type Service interface {
	Install(context.Context, string) error
	Start(context.Context, string) error
	Stop(context.Context, string) error
}

type Session interface {
	CurrentUser(context.Context) (string, error)
	RunAsUser(context.Context, string, []string) error
}

type LocalEndpoint interface {
	Listen(context.Context, string) error
	Dial(context.Context, string) error
}

type Capabilities interface {
	Supported(context.Context, string) (bool, error)
}

type Ports struct {
	OS           OS
	Service      Service
	Session      Session
	Endpoint     LocalEndpoint
	Capabilities Capabilities
}

var ErrUnsupportedPlatform = errors.New("platform is not supported")

// Runtime is the single composition boundary used by platform-aware startup.
// Business code receives ports from Runtime and never switches on GOOS.
type Runtime struct{ Ports }

func (r Runtime) Validate() error {
	if r.OS == "" || r.Service == nil || r.Session == nil || r.Endpoint == nil || r.Capabilities == nil {
		return errors.New("platform runtime is incomplete")
	}
	return nil
}

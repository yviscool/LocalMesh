package platform

import "context"

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

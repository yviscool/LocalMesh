//go:build !windows

package common

import "localmesh/internal/platform"

func defaultEndpoint() platform.LocalEndpoint { return UnixEndpoint{} }

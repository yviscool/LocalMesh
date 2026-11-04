//go:build !windows

package restrictions

import (
	"context"
	"fmt"
)

// SystemPolicyExecutor is unsupported on non-Windows platforms.
type SystemPolicyExecutor struct{}

func (e *SystemPolicyExecutor) Apply(_ context.Context, _ []RegistryCommand) error {
	return fmt.Errorf("system restrictions not supported on this platform")
}

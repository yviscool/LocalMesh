//go:build windows

package restrictions

import (
	"context"
	"fmt"
	"os/exec"
)

// SystemPolicyExecutor applies compiled registry commands via reg.exe.
type SystemPolicyExecutor struct{}

// Apply executes the compiled commands; USB storage entries require admin.
func (e *SystemPolicyExecutor) Apply(_ context.Context, commands []RegistryCommand) error {
	for _, cmd := range commands {
		if out, err := exec.Command("reg", cmd.Args...).CombinedOutput(); err != nil {
			return fmt.Errorf("reg %v: %v: %s", cmd.Args, err, string(out))
		}
	}
	return nil
}

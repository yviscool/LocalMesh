//go:build !windows

package restrictions

import (
	"context"
	"fmt"
	"os"
)

// ListProcesses is unsupported on non-Windows platforms for now.
func (c *OSProcessController) ListProcesses(_ context.Context) ([]ProcessInfo, error) {
	return nil, fmt.Errorf("process listing not supported on this platform")
}

// KillProcess terminates one process by PID via os.FindProcess.
func (c *OSProcessController) KillProcess(_ context.Context, pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

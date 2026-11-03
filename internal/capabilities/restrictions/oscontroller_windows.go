//go:build windows

package restrictions

import (
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ListProcesses returns the visible-session process list via tasklist,
// giving ProcessMonitor a working enumeration path on Windows.
func (c *OSProcessController) ListProcesses(_ context.Context) ([]ProcessInfo, error) {
	out, err := exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil, fmt.Errorf("tasklist: %w", err)
	}

	records, err := csv.NewReader(strings.NewReader(string(out))).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse tasklist output: %w", err)
	}

	procs := make([]ProcessInfo, 0, len(records))
	for _, rec := range records {
		// Row: "name","pid","session name","session num","mem usage"
		if len(rec) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(rec[1]))
		if err != nil {
			continue
		}
		procs = append(procs, ProcessInfo{PID: pid, Name: strings.TrimSpace(rec[0])})
	}
	return procs, nil
}

// KillProcess terminates one process tree by PID.
func (c *OSProcessController) KillProcess(_ context.Context, pid int) error {
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}

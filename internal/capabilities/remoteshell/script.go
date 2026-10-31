package remoteshell

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

var (
	ErrEmptyContent           = errors.New("script content cannot be empty")
	ErrUnsupportedScriptType  = errors.New("unsupported script type")
	ErrNegativeTimeout        = errors.New("timeout seconds cannot be negative")
	ErrNegativeMaxOutputBytes = errors.New("max output bytes cannot be negative")
	ErrWorkingDirNotFound     = errors.New("working directory does not exist")
)

// ScriptType designates the script interpreter runtime.
type ScriptType string

const (
	ScriptTypeBatch      ScriptType = "batch"
	ScriptTypePowerShell ScriptType = "powershell"
	ScriptTypeShell      ScriptType = "shell"
)

// ScriptExecutionRequest contains instructions and parameters for running a remote script.
type ScriptExecutionRequest struct {
	ScriptType     ScriptType        `json:"script_type"`
	Content        string            `json:"content"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	WorkingDir     string            `json:"working_dir,omitempty"`
	MaxOutputBytes int               `json:"max_output_bytes,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
}

// Validate verifies the script execution request parameters.
func (r ScriptExecutionRequest) Validate() error {
	switch r.ScriptType {
	case ScriptTypeBatch, ScriptTypePowerShell, ScriptTypeShell:
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedScriptType, r.ScriptType)
	}

	if strings.TrimSpace(r.Content) == "" {
		return ErrEmptyContent
	}

	if r.TimeoutSeconds < 0 {
		return ErrNegativeTimeout
	}

	if r.MaxOutputBytes < 0 {
		return ErrNegativeMaxOutputBytes
	}

	if r.WorkingDir != "" {
		info, err := os.Stat(r.WorkingDir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("%w: %q", ErrWorkingDirNotFound, r.WorkingDir)
		}
	}

	return nil
}

// ScriptExecutionResult encapsulates the outcome of a script execution.
type ScriptExecutionResult struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated"`
	Error      string `json:"error,omitempty"`
}

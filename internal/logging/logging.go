// Package logging provides leveled, rotating file+console logging for the
// LocalMesh apps. It wraps the standard library logger with a size-based
// rotation policy (10 MB → .old).
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var (
	minLevel  = LevelInfo
	levelNames = map[Level]string{LevelDebug: "DEBUG", LevelInfo: "INFO", LevelWarn: "WARN", LevelError: "ERROR"}
)

// Level is the minimum severity that gets emitted.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

const maxLogSize = 10 << 20 // 10 MB

// Setup routes the standard logger to (optionally) a rotating file plus
// stderr, and sets the minimum level. Returns a close func.
func Setup(level, file string) func() {
	switch strings.ToLower(level) {
	case "debug":
		minLevel = LevelDebug
	case "warn":
		minLevel = LevelWarn
	case "error":
		minLevel = LevelError
	default:
		minLevel = LevelInfo
	}

	writers := []io.Writer{os.Stderr}
	if file != "" {
		_ = os.MkdirAll(filepath.Dir(file), 0o755)
		f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			// size-based rotation
			if info, err := f.Stat(); err == nil && info.Size() > maxLogSize {
				f.Close()
				_ = os.Rename(file, file+".old")
				f, err = os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
				if err != nil {
					log.SetOutput(os.Stderr)
					return func() {}
				}
			}
			writers = append(writers, f)
		}
	}

	out := io.MultiWriter(writers...)
	log.SetOutput(out)
	log.SetFlags(log.LstdFlags | log.LUTC)
	return func() {}
}

// Emit writes one leveled line; used by app code instead of bare log calls
// when the level matters.
func Emit(level Level, format string, args ...any) {
	if level < minLevel {
		return
	}
	log.Output(2, fmt.Sprintf("[%s] %s", levelNames[level], fmt.Sprintf(format, args...)))
}

// Debug emits a debug line.
func Debug(format string, args ...any) { Emit(LevelDebug, format, args...) }

// Info emits an info line.
func Info(format string, args ...any) { Emit(LevelInfo, format, args...) }

// Warn emits a warning line.
func Warn(format string, args ...any) { Emit(LevelWarn, format, args...) }

// Error emits an error line.
func Error(format string, args ...any) { Emit(LevelError, format, args...) }

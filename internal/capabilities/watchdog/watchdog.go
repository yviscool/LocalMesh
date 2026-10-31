package watchdog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrSupervisorRunning    = errors.New("watchdog supervisor is already running")
	ErrSupervisorNotRunning = errors.New("watchdog supervisor is not running")
	ErrInvalidTarget        = errors.New("invalid watch target")
	ErrProcessNotFound      = errors.New("target process not found")
	ErrRateLimitExceeded    = errors.New("max crash rate limit exceeded; avalanche protection engaged")
	ErrProcessLaunchFailed  = errors.New("failed to restart target process")
)

// SupervisorState denotes the lifecycle status of the watchdog supervisor.
type SupervisorState string

const (
	StateStopped     SupervisorState = "stopped"
	StateMonitoring  SupervisorState = "monitoring"
	StateRestarting  SupervisorState = "restarting"
	StateRateLimited SupervisorState = "rate_limited"
	StateFailed      SupervisorState = "failed"
)

// RestartPolicy governs crash detection thresholds and restart timing.
type RestartPolicy struct {
	// MaxCrashes is the maximum allowed crash count within CrashWindow before rate limiting.
	MaxCrashes int `json:"max_crashes"`
	// CrashWindow is the sliding window duration to monitor crash frequency.
	CrashWindow time.Duration `json:"crash_window"`
	// RestartDelay is the interval before initiating process respawn (<200ms).
	RestartDelay time.Duration `json:"restart_delay"`
	// MinUptime is the duration a process must run smoothly before resetting the crash log.
	MinUptime time.Duration `json:"min_uptime"`
}

// DefaultRestartPolicy provides safe, responsive supervisor restart parameters (<200ms restart).
func DefaultRestartPolicy() RestartPolicy {
	return RestartPolicy{
		MaxCrashes:   5,
		CrashWindow:  30 * time.Second,
		RestartDelay: 50 * time.Millisecond,
		MinUptime:    10 * time.Second,
	}
}

// WatchTarget defines the executable and runtime attributes of the monitored child/peer process.
type WatchTarget struct {
	PID           uint32        `json:"pid"`
	ExecPath      string        `json:"exec_path"`
	Args          []string      `json:"args"`
	RestartPolicy RestartPolicy `json:"restart_policy"`
}

// Validate ensures target executable and parameters are actionable.
func (t WatchTarget) Validate() error {
	if strings.TrimSpace(t.ExecPath) == "" && t.PID == 0 {
		return fmt.Errorf("%w: ExecPath or PID must be specified", ErrInvalidTarget)
	}
	if t.RestartPolicy.MaxCrashes <= 0 {
		t.RestartPolicy.MaxCrashes = 5
	}
	if t.RestartPolicy.CrashWindow <= 0 {
		t.RestartPolicy.CrashWindow = 30 * time.Second
	}
	if t.RestartPolicy.RestartDelay <= 0 {
		t.RestartPolicy.RestartDelay = 50 * time.Millisecond
	}
	return nil
}

// ProcessWatcher observes a process until termination.
type ProcessWatcher interface {
	WaitForExit(ctx context.Context, pid uint32) (int, error)
	IsAlive(pid uint32) bool
}

// ProcessLauncher starts a new instance of the monitored process.
type ProcessLauncher interface {
	StartProcess(execPath string, args []string) (uint32, error)
}

// ProcessHardener applies DACL hardening to protect against unauthorized termination.
type ProcessHardener interface {
	HardenProcessDACL(pid uint32) error
}

// KillerFilter maintains a blacklist of process names representing task-killers or trainers.
type KillerFilter struct {
	mu        sync.RWMutex
	blacklist map[string]struct{}
}

// defaultBlacklist contains common utilities used by students to terminate classroom agents.
var defaultBlacklist = []string{
	"taskmgr.exe",
	"jiyutrainer.exe",
	"pchunter.exe",
	"pchunter32.exe",
	"pchunter64.exe",
	"processhacker.exe",
	"procexp.exe",
	"x64dbg.exe",
	"x32dbg.exe",
	"cheatengine-x86_64.exe",
	"cheatengine-i386.exe",
	"ida.exe",
	"ida64.exe",
}

// normalizeProcessName removes directory paths, .exe suffixes, and converts to lowercase.
func normalizeProcessName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	lower := strings.ToLower(base)
	return strings.TrimSuffix(lower, ".exe")
}

// NewKillerFilter constructs a KillerFilter initialized with default blacklisted utilities
// and optional caller-specified additions.
func NewKillerFilter(customBlacklist ...string) *KillerFilter {
	kf := &KillerFilter{
		blacklist: make(map[string]struct{}),
	}
	for _, name := range defaultBlacklist {
		kf.blacklist[normalizeProcessName(name)] = struct{}{}
	}
	for _, name := range customBlacklist {
		normalized := normalizeProcessName(name)
		if normalized != "" {
			kf.blacklist[normalized] = struct{}{}
		}
	}
	return kf
}

// Matches checks whether the given process name matches any blacklisted killer utility.
func (kf *KillerFilter) Matches(processName string) bool {
	if kf == nil {
		return false
	}
	kf.mu.RLock()
	defer kf.mu.RUnlock()

	normalized := normalizeProcessName(processName)
	_, found := kf.blacklist[normalized]
	return found
}

// Scan filters an input slice of process names, returning all matches against the blacklist.
func (kf *KillerFilter) Scan(processNames []string) []string {
	if kf == nil || len(processNames) == 0 {
		return nil
	}
	kf.mu.RLock()
	defer kf.mu.RUnlock()

	var matches []string
	for _, name := range processNames {
		normalized := normalizeProcessName(name)
		if _, found := kf.blacklist[normalized]; found {
			matches = append(matches, name)
		}
	}
	return matches
}

// defaultGlobalKillerFilter is the shared instance for IsBlacklistedKiller.
var defaultGlobalKillerFilter = NewKillerFilter()

// IsBlacklistedKiller returns true if the process matches known killer tools (taskmgr, jiyutrainer, pchunter, etc.).
func IsBlacklistedKiller(processName string) bool {
	return defaultGlobalKillerFilter.Matches(processName)
}

// SupervisorOption allows customizing supervisor dependencies and event callbacks.
type SupervisorOption func(*Supervisor)

// WithWatcher configures a custom ProcessWatcher implementation.
func WithWatcher(w ProcessWatcher) SupervisorOption {
	return func(s *Supervisor) {
		s.watcher = w
	}
}

// WithLauncher configures a custom ProcessLauncher implementation.
func WithLauncher(l ProcessLauncher) SupervisorOption {
	return func(s *Supervisor) {
		s.launcher = l
	}
}

// WithHardener configures a custom ProcessHardener implementation.
func WithHardener(h ProcessHardener) SupervisorOption {
	return func(s *Supervisor) {
		s.hardener = h
	}
}

// WithClock configures a custom time provider for testing.
func WithClock(clock func() time.Time) SupervisorOption {
	return func(s *Supervisor) {
		s.clock = clock
	}
}

// WithCallbacks wires notification hooks for lifecycle events.
func WithCallbacks(onRestart func(newPID uint32), onCrash func(pid uint32, code int), onRateLimit func(crashes int)) SupervisorOption {
	return func(s *Supervisor) {
		s.onRestart = onRestart
		s.onCrash = onCrash
		s.onRateLimit = onRateLimit
	}
}

// Supervisor continuously monitors a target process, detects abnormal termination,
// and restarts it in <200ms with avalanche rate limiting protection.
type Supervisor struct {
	mu           sync.RWMutex
	target       WatchTarget
	state        SupervisorState
	currentPID   uint32
	restartCount int
	crashes      []time.Time

	watcher  ProcessWatcher
	launcher ProcessLauncher
	hardener ProcessHardener
	clock    func() time.Time

	cancel    context.CancelFunc
	done      chan struct{}
	lastStart time.Time

	onRestart   func(newPID uint32)
	onCrash     func(pid uint32, code int)
	onRateLimit func(crashes int)
}

// NewSupervisor constructs a new Supervisor with safe defaults and platform ports.
func NewSupervisor(target WatchTarget, opts ...SupervisorOption) (*Supervisor, error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}

	s := &Supervisor{
		target:     target,
		state:      StateStopped,
		currentPID: target.PID,
		crashes:    make([]time.Time, 0, 16),
		clock:      time.Now,
		watcher:    NewDefaultWatcher(),
		launcher:   NewDefaultLauncher(),
		hardener:   NewDefaultHardener(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

// State returns the current lifecycle status of the supervisor.
func (s *Supervisor) State() SupervisorState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// CurrentPID returns the actively supervised PID.
func (s *Supervisor) CurrentPID() uint32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentPID
}

// RestartCount returns the cumulative number of successful auto-restarts performed.
func (s *Supervisor) RestartCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.restartCount
}

// Start begins asynchronous process supervision.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.state == StateMonitoring || s.state == StateRestarting {
		s.mu.Unlock()
		return ErrSupervisorRunning
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.state = StateMonitoring
	s.lastStart = s.clock()

	pid := s.currentPID
	hardener := s.hardener
	s.mu.Unlock()

	// Apply DACL hardening to initial process if specified
	if pid > 0 && hardener != nil {
		_ = hardener.HardenProcessDACL(pid)
	}

	go s.supervise(runCtx)
	return nil
}

// Stop terminates the supervision loop without killing the target process.
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	if s.state == StateStopped {
		s.mu.Unlock()
		return nil
	}

	cancel := s.cancel
	done := s.done
	s.state = StateStopped
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

// supervise runs the monitoring and restart state machine.
func (s *Supervisor) supervise(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		if s.state != StateRateLimited && s.state != StateFailed {
			s.state = StateStopped
		}
		done := s.done
		s.mu.Unlock()
		if done != nil {
			close(done)
		}
	}()

	for {
		s.mu.RLock()
		pid := s.currentPID
		execPath := s.target.ExecPath
		args := s.target.Args
		policy := s.target.RestartPolicy
		s.mu.RUnlock()

		// If process is not yet running, spawn it
		if pid == 0 {
			if execPath == "" {
				s.mu.Lock()
				s.state = StateFailed
				s.mu.Unlock()
				return
			}
			newPID, err := s.launcher.StartProcess(execPath, args)
			if err != nil {
				s.mu.Lock()
				s.state = StateFailed
				s.mu.Unlock()
				return
			}
			pid = newPID
			s.mu.Lock()
			s.currentPID = pid
			s.lastStart = s.clock()
			s.state = StateMonitoring
			s.mu.Unlock()

			if s.hardener != nil {
				_ = s.hardener.HardenProcessDACL(pid)
			}
			if s.onRestart != nil {
				s.onRestart(pid)
			}
		}

		// Wait for target process to exit or context cancellation
		exitCode, err := s.watcher.WaitForExit(ctx, pid)
		if ctx.Err() != nil {
			// Supervised loop cancelled normally via Stop or parent context
			return
		}

		now := s.clock()

		// Record crash event
		s.mu.Lock()
		if s.onCrash != nil {
			s.onCrash(pid, exitCode)
		}

		// Avalanche rate limiting check:
		// Discard crashes outside the sliding CrashWindow
		windowStart := now.Add(-policy.CrashWindow)
		validCrashes := make([]time.Time, 0, len(s.crashes)+1)
		for _, t := range s.crashes {
			if t.After(windowStart) {
				validCrashes = append(validCrashes, t)
			}
		}
		validCrashes = append(validCrashes, now)
		s.crashes = validCrashes

		if len(s.crashes) > policy.MaxCrashes {
			s.state = StateRateLimited
			crashCount := len(s.crashes)
			onRateLimit := s.onRateLimit
			s.mu.Unlock()

			if onRateLimit != nil {
				onRateLimit(crashCount)
			}
			return
		}

		s.state = StateRestarting
		s.currentPID = 0
		s.mu.Unlock()

		// Responsive restart delay (<200ms)
		if policy.RestartDelay > 0 {
			select {
			case <-time.After(policy.RestartDelay):
			case <-ctx.Done():
				return
			}
		}

		// Respawn target process
		if execPath == "" {
			s.mu.Lock()
			s.state = StateFailed
			s.mu.Unlock()
			return
		}

		newPID, err := s.launcher.StartProcess(execPath, args)
		if err != nil {
			s.mu.Lock()
			s.state = StateFailed
			s.mu.Unlock()
			return
		}

		s.mu.Lock()
		s.currentPID = newPID
		s.target.PID = newPID
		s.restartCount++
		s.lastStart = s.clock()
		s.state = StateMonitoring
		hardener := s.hardener
		onRestart := s.onRestart
		s.mu.Unlock()

		if hardener != nil {
			_ = hardener.HardenProcessDACL(newPID)
		}
		if onRestart != nil {
			onRestart(newPID)
		}
	}
}

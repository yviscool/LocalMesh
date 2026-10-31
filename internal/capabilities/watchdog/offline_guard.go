package watchdog

import (
	"context"
	"net"
	"sync"
	"time"

	"localmesh/internal/capabilities/screenlock"
)

const (
	DefaultLeaseDuration  = 10 * time.Second
	DefaultCheckInterval  = 1 * time.Second
	DefaultWarningTitle   = "⚠️ 警告：检测到本机网络连接已中断！"
	DefaultWarningMessage = "请插回网线以恢复上课！"
)

// OfflineGuardConfig defines timing and messaging policies for the offline watchdog.
type OfflineGuardConfig struct {
	// LeaseDuration is how long the agent can go without a teacher heartbeat before locking (e.g. 10s).
	LeaseDuration time.Duration `json:"lease_duration"`
	// CheckInterval is how frequently the guard checks lease expiry and network connectivity.
	CheckInterval time.Duration `json:"check_interval"`
	// WarningTitle is the lock overlay banner title.
	WarningTitle string `json:"warning_title"`
	// WarningMessage is the lock overlay banner description.
	WarningMessage string `json:"warning_message"`
	// CheckNICState when true will immediately trigger punishment if all physical NICs are down.
	CheckNICState bool `json:"check_nic_state"`
}

// DefaultOfflineGuardConfig returns recommended default settings.
func DefaultOfflineGuardConfig() OfflineGuardConfig {
	return OfflineGuardConfig{
		LeaseDuration:  DefaultLeaseDuration,
		CheckInterval:  DefaultCheckInterval,
		WarningTitle:   DefaultWarningTitle,
		WarningMessage: DefaultWarningMessage,
		CheckNICState:  true,
	}
}

// OfflineGuard monitors student-to-teacher connectivity and heartbeats during active class sessions.
// If the physical cable is unplugged, network adapter disabled, or heartbeat expires,
// it locks down the screen and input via screenlock.Manager, restoring them once heartbeats resume.
type OfflineGuard struct {
	mu             sync.RWMutex
	lockMgr        *screenlock.Manager
	cfg            OfflineGuardConfig
	inSession      bool
	sessionID      string
	lastHeartbeat  time.Time
	isPunished     bool
	cancelFunc     context.CancelFunc
	doneChan       chan struct{}
	netCheckerFunc func() bool
}

// NewOfflineGuard creates an OfflineGuard with the specified screen lock manager and config.
func NewOfflineGuard(lockMgr *screenlock.Manager, cfg OfflineGuardConfig) *OfflineGuard {
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = DefaultLeaseDuration
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = DefaultCheckInterval
	}
	if cfg.WarningTitle == "" {
		cfg.WarningTitle = DefaultWarningTitle
	}
	if cfg.WarningMessage == "" {
		cfg.WarningMessage = DefaultWarningMessage
	}

	return &OfflineGuard{
		lockMgr:        lockMgr,
		cfg:            cfg,
		netCheckerFunc: HasActivePhysicalNetwork,
	}
}

// SetNetChecker allows substituting network interface detection for testing.
func (g *OfflineGuard) SetNetChecker(fn func() bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.netCheckerFunc = fn
}

// Start begins periodic background monitoring of heartbeats and connectivity.
func (g *OfflineGuard) Start(ctx context.Context) {
	g.mu.Lock()
	if g.cancelFunc != nil {
		g.mu.Unlock()
		return
	}
	monitorCtx, cancel := context.WithCancel(ctx)
	g.cancelFunc = cancel
	g.doneChan = make(chan struct{})
	g.mu.Unlock()

	go g.run(monitorCtx)
}

// Stop terminates the background monitor and releases any active punishment locks.
func (g *OfflineGuard) Stop() {
	g.mu.Lock()
	cancel := g.cancelFunc
	g.cancelFunc = nil
	done := g.doneChan
	g.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.isPunished && g.lockMgr != nil {
		_ = g.lockMgr.Unlock()
		g.isPunished = false
	}
}

// EnterSession marks the agent as actively managed under a classroom session.
// Heartbeat lease monitoring begins immediately.
func (g *OfflineGuard) EnterSession(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.inSession = true
	g.sessionID = sessionID
	g.lastHeartbeat = time.Now()
}

// LeaveSession marks the classroom session as ended, disengaging watchdog punishment.
func (g *OfflineGuard) LeaveSession() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.inSession = false
	g.sessionID = ""
	if g.isPunished && g.lockMgr != nil {
		_ = g.lockMgr.Unlock()
		g.isPunished = false
	}
}

// InSession reports whether the agent is currently in an active classroom session.
func (g *OfflineGuard) InSession() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.inSession
}

// RecordHeartbeat logs a heartbeat from the teacher console.
// If the screen was locked due to offline penalty, it is automatically unlocked.
func (g *OfflineGuard) RecordHeartbeat() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.lastHeartbeat = time.Now()

	// If currently locked due to offline punishment, restore screen and input immediately
	if g.isPunished && g.lockMgr != nil {
		_ = g.lockMgr.Unlock()
		g.isPunished = false
	}
}

// IsPunished reports whether offline punishment lock is currently applied.
func (g *OfflineGuard) IsPunished() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.isPunished
}

// LastHeartbeat returns the timestamp of the most recent recorded heartbeat.
func (g *OfflineGuard) LastHeartbeat() time.Time {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.lastHeartbeat
}

// Check evaluates heartbeat freshness and network link status, applying or clearing penalty locks.
func (g *OfflineGuard) Check() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.inSession {
		return
	}

	now := time.Now()
	expired := now.Sub(g.lastHeartbeat) > g.cfg.LeaseDuration
	carrierLost := false

	if g.cfg.CheckNICState && g.netCheckerFunc != nil {
		if !g.netCheckerFunc() {
			carrierLost = true
		}
	}

	needsPunishment := expired || carrierLost

	if needsPunishment && !g.isPunished {
		// Engage offline punishment
		if g.lockMgr != nil {
			_ = g.lockMgr.Lock(screenlock.LockRequest{
				Title:         g.cfg.WarningTitle,
				Message:       g.cfg.WarningMessage,
				LockInputOnly: false,
				ShowBanner:    true,
			})
		}
		g.isPunished = true
	}
}

func (g *OfflineGuard) run(ctx context.Context) {
	defer func() {
		g.mu.Lock()
		if g.doneChan != nil {
			close(g.doneChan)
		}
		g.mu.Unlock()
	}()

	ticker := time.NewTicker(g.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.Check()
		}
	}
}

// HasActivePhysicalNetwork inspects local network interfaces to detect physical carrier presence.
// Returns true if at least one non-loopback up interface exists with an assigned address.
func HasActivePhysicalNetwork() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return true // If check fails, do not falsely flag carrier loss
	}

	for _, iface := range ifaces {
		// Ignore loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err == nil && len(addrs) > 0 {
			return true
		}
	}

	return false
}

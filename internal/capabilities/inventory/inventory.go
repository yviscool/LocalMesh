package inventory

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	// ErrNilCollector indicates that a nil collector was supplied to the manager.
	ErrNilCollector = errors.New("inventory collector cannot be nil")
	// ErrCollectorFailed indicates that inventory collection failed.
	ErrCollectorFailed = errors.New("failed to collect inventory telemetry")
)

// CPUInfo represents processor hardware specifications and real-time load.
type CPUInfo struct {
	Model        string  `json:"model"`
	Cores        int     `json:"cores"`
	FrequencyMHz uint64  `json:"frequency_mhz"`
	UsagePercent float64 `json:"usage_percent"`
}

// MemoryInfo represents physical RAM telemetry.
type MemoryInfo struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

// DiskPartition represents a storage partition or mounted filesystem.
type DiskPartition struct {
	MountPoint string `json:"mount_point"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	FileSystem string `json:"file_system"`
}

// GPUInfo represents graphics adapter hardware details.
type GPUInfo struct {
	Model     string `json:"model"`
	VRAMBytes uint64 `json:"vram_bytes"`
}

// NetAdapter represents a physical or virtual network interface.
type NetAdapter struct {
	Name string   `json:"name"`
	MAC  string   `json:"mac"`
	IPs  []string `json:"ips"`
}

// HardwareTelemetry aggregates hardware asset information.
type HardwareTelemetry struct {
	CPU     CPUInfo         `json:"cpu"`
	Memory  MemoryInfo      `json:"memory"`
	Disks   []DiskPartition `json:"disks"`
	GPU     GPUInfo         `json:"gpu"`
	Network []NetAdapter    `json:"network"`
}

// OSInfo represents operating system information.
type OSInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	BuildNumber string `json:"build_number"`
	Arch        string `json:"arch"`
}

// SystemTelemetry aggregates operating system and runtime telemetry.
type SystemTelemetry struct {
	OS            OSInfo    `json:"os"`
	Hostname      string    `json:"hostname"`
	UptimeSeconds uint64    `json:"uptime_seconds"`
	CurrentUser   string    `json:"current_user"`
	BootTime      time.Time `json:"boot_time"`
}

// SoftwareItem represents an installed application or package entry.
type SoftwareItem struct {
	DisplayName     string `json:"display_name"`
	DisplayVersion  string `json:"display_version"`
	Publisher       string `json:"publisher"`
	InstallDate     string `json:"install_date"`
	InstallLocation string `json:"install_location"`
}

// InventorySnapshot aggregates full hardware, system, and installed software assets.
type InventorySnapshot struct {
	Hardware          HardwareTelemetry `json:"hardware"`
	System            SystemTelemetry   `json:"system"`
	InstalledSoftware []SoftwareItem    `json:"installed_software"`
	CapturedAt        time.Time         `json:"captured_at"`
}

// Collector defines the interface for collecting system, hardware, and software telemetry.
type Collector interface {
	CollectHardware(ctx context.Context) (HardwareTelemetry, error)
	CollectSystem(ctx context.Context) (SystemTelemetry, error)
	CollectSoftware(ctx context.Context) ([]SoftwareItem, error)
	CollectSnapshot(ctx context.Context) (InventorySnapshot, error)
}

// Manager coordinates inventory collection and thread-safe snapshot caching.
type Manager struct {
	mu           sync.RWMutex
	collector    Collector
	cached       *InventorySnapshot
	cacheTTL     time.Duration
	lastCaptured time.Time
}

// NewManager creates a new Manager with the default 30-second cache TTL.
func NewManager(collector Collector) (*Manager, error) {
	return NewManagerWithTTL(collector, 30*time.Second)
}

// NewManagerWithTTL creates a new Manager with a specified cache TTL.
func NewManagerWithTTL(collector Collector, ttl time.Duration) (*Manager, error) {
	if collector == nil {
		return nil, ErrNilCollector
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Manager{
		collector: collector,
		cacheTTL:  ttl,
	}, nil
}

// GetSnapshot retrieves the inventory snapshot, serving from cache if still within TTL.
func (m *Manager) GetSnapshot(ctx context.Context) (InventorySnapshot, error) {
	m.mu.RLock()
	if m.cached != nil && time.Since(m.lastCaptured) < m.cacheTTL {
		snap := *m.cached
		m.mu.RUnlock()
		return snap, nil
	}
	m.mu.RUnlock()

	return m.Refresh(ctx)
}

// Refresh triggers an immediate telemetry collection, bypassing and updating the cache.
func (m *Manager) Refresh(ctx context.Context) (InventorySnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap, err := m.collector.CollectSnapshot(ctx)
	if err != nil {
		return InventorySnapshot{}, err
	}
	m.cached = &snap
	m.lastCaptured = snap.CapturedAt
	return snap, nil
}

// GetCachedSnapshot returns the cached snapshot if one exists.
func (m *Manager) GetCachedSnapshot() (InventorySnapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cached == nil {
		return InventorySnapshot{}, false
	}
	return *m.cached, true
}

// GetHardware returns fresh hardware telemetry.
func (m *Manager) GetHardware(ctx context.Context) (HardwareTelemetry, error) {
	return m.collector.CollectHardware(ctx)
}

// GetSystem returns fresh system telemetry.
func (m *Manager) GetSystem(ctx context.Context) (SystemTelemetry, error) {
	return m.collector.CollectSystem(ctx)
}

// GetSoftware returns the installed software list.
func (m *Manager) GetSoftware(ctx context.Context) ([]SoftwareItem, error) {
	return m.collector.CollectSoftware(ctx)
}

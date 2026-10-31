package inventory_test

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"testing"
	"time"

	"localmesh/internal/capabilities/inventory"
)

// fakeCollector implements inventory.Collector for deterministic testing.
type fakeCollector struct {
	hw  inventory.HardwareTelemetry
	sys inventory.SystemTelemetry
	sw  []inventory.SoftwareItem
}

func (f *fakeCollector) CollectHardware(ctx context.Context) (inventory.HardwareTelemetry, error) {
	if err := ctx.Err(); err != nil {
		return inventory.HardwareTelemetry{}, err
	}
	return f.hw, nil
}

func (f *fakeCollector) CollectSystem(ctx context.Context) (inventory.SystemTelemetry, error) {
	if err := ctx.Err(); err != nil {
		return inventory.SystemTelemetry{}, err
	}
	return f.sys, nil
}

func (f *fakeCollector) CollectSoftware(ctx context.Context) ([]inventory.SoftwareItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.sw, nil
}

func (f *fakeCollector) CollectSnapshot(ctx context.Context) (inventory.InventorySnapshot, error) {
	hw, err := f.CollectHardware(ctx)
	if err != nil {
		return inventory.InventorySnapshot{}, err
	}
	sys, err := f.CollectSystem(ctx)
	if err != nil {
		return inventory.InventorySnapshot{}, err
	}
	sw, err := f.CollectSoftware(ctx)
	if err != nil {
		return inventory.InventorySnapshot{}, err
	}
	return inventory.InventorySnapshot{
		Hardware:          hw,
		System:            sys,
		InstalledSoftware: sw,
		CapturedAt:        time.Now().UTC(),
	}, nil
}

func sampleSnapshot() inventory.InventorySnapshot {
	return inventory.InventorySnapshot{
		Hardware: inventory.HardwareTelemetry{
			CPU: inventory.CPUInfo{
				Model:        "Intel(R) Core(TM) i7-12700H",
				Cores:        14,
				FrequencyMHz: 2300,
				UsagePercent: 18.5,
			},
			Memory: inventory.MemoryInfo{
				TotalBytes:     17179869184,
				AvailableBytes: 8589934592,
				UsedPercent:    50.0,
			},
			Disks: []inventory.DiskPartition{
				{
					MountPoint: "C:\\",
					TotalBytes: 512000000000,
					FreeBytes:  256000000000,
					FileSystem: "NTFS",
				},
			},
			GPU: inventory.GPUInfo{
				Model:     "NVIDIA GeForce RTX 3060",
				VRAMBytes: 6442450944,
			},
			Network: []inventory.NetAdapter{
				{
					Name: "Ethernet0",
					MAC:  "00:11:22:33:44:55",
					IPs:  []string{"192.168.1.100"},
				},
			},
		},
		System: inventory.SystemTelemetry{
			OS: inventory.OSInfo{
				Name:        "Windows 11 Pro",
				Version:     "23H2",
				BuildNumber: "22631",
				Arch:        "amd64",
			},
			Hostname:      "LAB-PC-01",
			UptimeSeconds: 7200,
			CurrentUser:   "Administrator",
			BootTime:      time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC),
		},
		InstalledSoftware: []inventory.SoftwareItem{
			{
				DisplayName:     "Visual Studio Code",
				DisplayVersion:  "1.95.0",
				Publisher:       "Microsoft Corporation",
				InstallDate:     "20261001",
				InstallLocation: "C:\\Program Files\\Microsoft VS Code",
			},
		},
		CapturedAt: time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC),
	}
}

func TestJSONSerialization(t *testing.T) {
	orig := sampleSnapshot()

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded inventory.InventorySnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.Hardware.CPU.Model != orig.Hardware.CPU.Model {
		t.Errorf("expected CPU model %q, got %q", orig.Hardware.CPU.Model, decoded.Hardware.CPU.Model)
	}
	if decoded.Hardware.CPU.Cores != orig.Hardware.CPU.Cores {
		t.Errorf("expected CPU cores %d, got %d", orig.Hardware.CPU.Cores, decoded.Hardware.CPU.Cores)
	}
	if decoded.Hardware.Memory.TotalBytes != orig.Hardware.Memory.TotalBytes {
		t.Errorf("expected Memory total %d, got %d", orig.Hardware.Memory.TotalBytes, decoded.Hardware.Memory.TotalBytes)
	}
	if len(decoded.Hardware.Disks) != 1 || decoded.Hardware.Disks[0].FileSystem != "NTFS" {
		t.Errorf("expected 1 NTFS disk, got %+v", decoded.Hardware.Disks)
	}
	if decoded.Hardware.GPU.Model != orig.Hardware.GPU.Model {
		t.Errorf("expected GPU %q, got %q", orig.Hardware.GPU.Model, decoded.Hardware.GPU.Model)
	}
	if len(decoded.Hardware.Network) != 1 || decoded.Hardware.Network[0].MAC != "00:11:22:33:44:55" {
		t.Errorf("expected 1 network adapter, got %+v", decoded.Hardware.Network)
	}
	if decoded.System.OS.BuildNumber != "22631" {
		t.Errorf("expected build 22631, got %q", decoded.System.OS.BuildNumber)
	}
	if len(decoded.InstalledSoftware) != 1 || decoded.InstalledSoftware[0].DisplayName != "Visual Studio Code" {
		t.Errorf("expected 1 installed software, got %+v", decoded.InstalledSoftware)
	}
}

func TestFieldValidity(t *testing.T) {
	snap := sampleSnapshot()

	if snap.Hardware.CPU.UsagePercent < 0 || snap.Hardware.CPU.UsagePercent > 100 {
		t.Errorf("invalid CPU usage percent: %f", snap.Hardware.CPU.UsagePercent)
	}
	if snap.Hardware.Memory.UsedPercent < 0 || snap.Hardware.Memory.UsedPercent > 100 {
		t.Errorf("invalid Memory used percent: %f", snap.Hardware.Memory.UsedPercent)
	}
	if snap.Hardware.Memory.AvailableBytes > snap.Hardware.Memory.TotalBytes {
		t.Errorf("available bytes exceeds total bytes: %d > %d", snap.Hardware.Memory.AvailableBytes, snap.Hardware.Memory.TotalBytes)
	}
	for _, disk := range snap.Hardware.Disks {
		if disk.FreeBytes > disk.TotalBytes {
			t.Errorf("disk %s free bytes exceeds total bytes: %d > %d", disk.MountPoint, disk.FreeBytes, disk.TotalBytes)
		}
	}
}

func TestDefaultCollector(t *testing.T) {
	collector := inventory.NewDefaultCollector()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	snap, err := collector.CollectSnapshot(ctx)
	if err != nil {
		t.Fatalf("CollectSnapshot failed: %v", err)
	}

	if snap.Hardware.CPU.Cores <= 0 {
		t.Errorf("expected CPU cores > 0, got %d", snap.Hardware.CPU.Cores)
	}
	if snap.Hardware.CPU.UsagePercent < 0.0 || snap.Hardware.CPU.UsagePercent > 100.0 {
		t.Errorf("unexpected CPU usage: %f", snap.Hardware.CPU.UsagePercent)
	}

	if runtime.GOOS == "windows" {
		if snap.Hardware.Memory.TotalBytes == 0 {
			t.Error("expected non-zero physical memory on Windows")
		}
		if len(snap.Hardware.Disks) == 0 {
			t.Error("expected at least one disk on Windows")
		}
		if snap.System.OS.Name == "" {
			t.Error("expected OS name to be set on Windows")
		}
		if snap.System.Hostname == "" {
			t.Error("expected Hostname to be set")
		}
	}
}

func TestManagerCachingAndTTL(t *testing.T) {
	sample := sampleSnapshot()
	fake := &fakeCollector{
		hw:  sample.Hardware,
		sys: sample.System,
		sw:  sample.InstalledSoftware,
	}

	mgr, err := inventory.NewManagerWithTTL(fake, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewManagerWithTTL failed: %v", err)
	}

	ctx := context.Background()

	// Initial fetch
	snap1, err := mgr.GetSnapshot(ctx)
	if err != nil {
		t.Fatalf("first GetSnapshot failed: %v", err)
	}

	// Verify cached snapshot
	cached, ok := mgr.GetCachedSnapshot()
	if !ok || cached.CapturedAt != snap1.CapturedAt {
		t.Errorf("expected cached snapshot with timestamp %v, got %v", snap1.CapturedAt, cached.CapturedAt)
	}

	// Immediate fetch should return identical cached snapshot
	snap2, err := mgr.GetSnapshot(ctx)
	if err != nil {
		t.Fatalf("second GetSnapshot failed: %v", err)
	}
	if snap1.CapturedAt != snap2.CapturedAt {
		t.Errorf("expected identical timestamp from cache, got %v and %v", snap1.CapturedAt, snap2.CapturedAt)
	}

	// Wait for TTL expiry
	time.Sleep(120 * time.Millisecond)

	snap3, err := mgr.GetSnapshot(ctx)
	if err != nil {
		t.Fatalf("third GetSnapshot failed: %v", err)
	}
	if snap3.CapturedAt.Equal(snap1.CapturedAt) {
		t.Error("expected fresh snapshot after TTL expiry")
	}

	// Test direct getters
	hw, err := mgr.GetHardware(ctx)
	if err != nil || hw.CPU.Model != sample.Hardware.CPU.Model {
		t.Errorf("GetHardware returned unexpected result: %+v, err: %v", hw, err)
	}
	sys, err := mgr.GetSystem(ctx)
	if err != nil || sys.Hostname != sample.System.Hostname {
		t.Errorf("GetSystem returned unexpected result: %+v, err: %v", sys, err)
	}
	sw, err := mgr.GetSoftware(ctx)
	if err != nil || len(sw) != len(sample.InstalledSoftware) {
		t.Errorf("GetSoftware returned unexpected result: %+v, err: %v", sw, err)
	}
}

func TestConcurrentCollectionSafety(t *testing.T) {
	collector := inventory.NewDefaultCollector()
	mgr, err := inventory.NewManager(collector)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			for j := 0; j < 3; j++ {
				snap, err := mgr.GetSnapshot(ctx)
				if err != nil {
					t.Errorf("concurrent GetSnapshot failed: %v", err)
					return
				}
				if snap.Hardware.CPU.Cores <= 0 {
					t.Errorf("invalid CPU cores: %d", snap.Hardware.CPU.Cores)
				}
			}
		}()
	}

	wg.Wait()
}

func TestManagerNilCollector(t *testing.T) {
	_, err := inventory.NewManager(nil)
	if err != inventory.ErrNilCollector {
		t.Errorf("expected ErrNilCollector, got %v", err)
	}
}

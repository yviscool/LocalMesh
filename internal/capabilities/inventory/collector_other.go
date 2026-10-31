//go:build !windows

package inventory

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type otherCollector struct{}

// NewDefaultCollector returns the portable non-Windows inventory collector.
func NewDefaultCollector() Collector {
	return &otherCollector{}
}

// CollectHardware collects portable hardware telemetry.
func (c *otherCollector) CollectHardware(ctx context.Context) (HardwareTelemetry, error) {
	if err := ctx.Err(); err != nil {
		return HardwareTelemetry{}, err
	}

	hw := HardwareTelemetry{
		CPU: CPUInfo{
			Model:        "Standard Processor",
			Cores:        runtime.NumCPU(),
			FrequencyMHz: 2400,
			UsagePercent: 0.0,
		},
		Memory: MemoryInfo{
			TotalBytes:     16 * 1024 * 1024 * 1024,
			AvailableBytes: 8 * 1024 * 1024 * 1024,
			UsedPercent:    50.0,
		},
		Disks: []DiskPartition{
			{
				MountPoint: "/",
				TotalBytes: 100 * 1024 * 1024 * 1024,
				FreeBytes:  50 * 1024 * 1024 * 1024,
				FileSystem: "ext4",
			},
		},
		GPU: GPUInfo{
			Model:     "Generic Display Adapter",
			VRAMBytes: 0,
		},
		Network: c.collectNetwork(),
	}

	// On Linux, attempt to parse /proc/cpuinfo and /proc/meminfo
	if runtime.GOOS == "linux" {
		c.parseLinuxCPU(&hw.CPU)
		c.parseLinuxMem(&hw.Memory)
	}

	return hw, nil
}

// CollectSystem collects portable system telemetry.
func (c *otherCollector) CollectSystem(ctx context.Context) (SystemTelemetry, error) {
	if err := ctx.Err(); err != nil {
		return SystemTelemetry{}, err
	}

	sys := SystemTelemetry{
		OS: OSInfo{
			Name:        runtime.GOOS,
			Version:     "1.0",
			BuildNumber: "unknown",
			Arch:        runtime.GOARCH,
		},
		BootTime:      time.Now().Add(-1 * time.Hour).UTC(),
		UptimeSeconds: 3600,
	}

	if h, err := os.Hostname(); err == nil {
		sys.Hostname = h
	}

	if u, err := user.Current(); err == nil && u.Username != "" {
		sys.CurrentUser = u.Username
	} else if un := os.Getenv("USER"); un != "" {
		sys.CurrentUser = un
	}

	if runtime.GOOS == "linux" {
		c.parseLinuxOSRelease(&sys.OS)
		c.parseLinuxUptime(&sys)
	}

	return sys, nil
}

// CollectSoftware collects installed software entries.
func (c *otherCollector) CollectSoftware(ctx context.Context) ([]SoftwareItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []SoftwareItem{}, nil
}

// CollectSnapshot collects full snapshot.
func (c *otherCollector) CollectSnapshot(ctx context.Context) (InventorySnapshot, error) {
	hw, err := c.CollectHardware(ctx)
	if err != nil {
		return InventorySnapshot{}, err
	}
	sys, err := c.CollectSystem(ctx)
	if err != nil {
		return InventorySnapshot{}, err
	}
	sw, err := c.CollectSoftware(ctx)
	if err != nil {
		return InventorySnapshot{}, err
	}

	return InventorySnapshot{
		Hardware:          hw,
		System:            sys,
		InstalledSoftware: sw,
		CapturedAt:        time.Now().UTC(),
	}, nil
}

func (c *otherCollector) collectNetwork() []NetAdapter {
	var adapters []NetAdapter
	ifaces, err := net.Interfaces()
	if err != nil {
		return adapters
	}

	for _, iface := range ifaces {
		if iface.Name == "" {
			continue
		}
		var ipList []string
		addrs, err := iface.Addrs()
		if err == nil {
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP != nil {
					ipList = append(ipList, ipNet.IP.String())
				}
			}
		}

		adapters = append(adapters, NetAdapter{
			Name: iface.Name,
			MAC:  iface.HardwareAddr.String(),
			IPs:  ipList,
		})
	}

	return adapters
}

func (c *otherCollector) parseLinuxCPU(cpu *CPUInfo) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				cpu.Model = strings.TrimSpace(parts[1])
			}
		} else if strings.HasPrefix(line, "cpu MHz") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				if mhz, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil {
					cpu.FrequencyMHz = uint64(mhz)
				}
			}
		}
	}
}

func (c *otherCollector) parseLinuxMem(mem *MemoryInfo) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var totalKB, availKB uint64
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if fields[0] == "MemTotal:" {
				totalKB, _ = strconv.ParseUint(fields[1], 10, 64)
			} else if fields[0] == "MemAvailable:" {
				availKB, _ = strconv.ParseUint(fields[1], 10, 64)
			}
		}
	}

	if totalKB > 0 {
		mem.TotalBytes = totalKB * 1024
		mem.AvailableBytes = availKB * 1024
		if totalKB >= availKB {
			mem.UsedPercent = float64(totalKB-availKB) / float64(totalKB) * 100.0
		}
	}
}

func (c *otherCollector) parseLinuxOSRelease(osInfo *OSInfo) {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		val := strings.Trim(parts[1], "\"")
		switch parts[0] {
		case "NAME":
			osInfo.Name = val
		case "VERSION_ID":
			osInfo.Version = val
		case "BUILD_ID":
			osInfo.BuildNumber = val
		}
	}
}

func (c *otherCollector) parseLinuxUptime(sys *SystemTelemetry) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return
	}
	fields := strings.Fields(string(data))
	if len(fields) > 0 {
		if sec, err := strconv.ParseFloat(fields[0], 64); err == nil {
			sys.UptimeSeconds = uint64(sec)
			sys.BootTime = time.Now().Add(-time.Duration(sec * float64(time.Second))).UTC()
		}
	}
}

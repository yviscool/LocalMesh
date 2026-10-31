//go:build windows

package inventory

import (
	"context"
	"fmt"
	"math"
	"net"
	"os"
	"os/user"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	modKernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = modKernel32.NewProc("GlobalMemoryStatusEx")
	procGetTickCount64       = modKernel32.NewProc("GetTickCount64")
	procGetSystemTimes       = modKernel32.NewProc("GetSystemTimes")
)

type memoryStatusEx struct {
	cbSize                  uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

// windowsCollector implements native Windows asset telemetry collection.
type windowsCollector struct {
	cpuMu        sync.Mutex
	lastSampleAt time.Time
	lastIdle     uint64
	lastKernel   uint64
	lastUser     uint64
}

// NewDefaultCollector constructs the Windows native inventory collector.
func NewDefaultCollector() Collector {
	return &windowsCollector{}
}

// CollectHardware collects Windows CPU, Memory, Disk, GPU, and Network telemetry.
func (c *windowsCollector) CollectHardware(ctx context.Context) (HardwareTelemetry, error) {
	if err := ctx.Err(); err != nil {
		return HardwareTelemetry{}, err
	}

	hw := HardwareTelemetry{
		CPU:     c.collectCPU(),
		Memory:  c.collectMemory(),
		Disks:   c.collectDisks(),
		GPU:     c.collectGPU(),
		Network: c.collectNetwork(),
	}
	return hw, nil
}

// CollectSystem collects Windows operating system and runtime telemetry.
func (c *windowsCollector) CollectSystem(ctx context.Context) (SystemTelemetry, error) {
	if err := ctx.Err(); err != nil {
		return SystemTelemetry{}, err
	}

	sys := SystemTelemetry{
		OS: OSInfo{
			Name: "Windows",
			Arch: runtime.GOARCH,
		},
	}

	// Hostname
	if h, err := os.Hostname(); err == nil {
		sys.Hostname = h
	}

	// Current user
	if u, err := user.Current(); err == nil && u.Username != "" {
		sys.CurrentUser = u.Username
	} else if un := os.Getenv("USERNAME"); un != "" {
		sys.CurrentUser = un
	}

	// Query Windows Version and Build info from registry
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Microsoft\Windows NT\CurrentVersion`, registry.READ)
	if err == nil {
		defer k.Close()
		if prod, _, err := k.GetStringValue("ProductName"); err == nil && prod != "" {
			sys.OS.Name = prod
		}
		if ver, _, err := k.GetStringValue("DisplayVersion"); err == nil && ver != "" {
			sys.OS.Version = ver
		} else if release, _, err := k.GetStringValue("ReleaseId"); err == nil && release != "" {
			sys.OS.Version = release
		}
		if build, _, err := k.GetStringValue("CurrentBuildNumber"); err == nil && build != "" {
			sys.OS.BuildNumber = build
		} else if build, _, err := k.GetStringValue("CurrentBuild"); err == nil && build != "" {
			sys.OS.BuildNumber = build
		}
	}

	// Uptime and boot time using GetTickCount64
	if procGetTickCount64.Find() == nil {
		r1, _, _ := procGetTickCount64.Call()
		if r1 > 0 {
			uptime := time.Duration(r1) * time.Millisecond
			sys.UptimeSeconds = uint64(uptime.Seconds())
			sys.BootTime = time.Now().Add(-uptime).UTC()
		}
	}

	return sys, nil
}

// CollectSoftware collects installed applications from Windows 32-bit and 64-bit Uninstall registry keys.
func (c *windowsCollector) CollectSoftware(ctx context.Context) ([]SoftwareItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	keysToScan := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `Software\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
	}

	seen := make(map[string]struct{})
	var items []SoftwareItem

	for _, ks := range keysToScan {
		if ctx.Err() != nil {
			break
		}
		subItems := c.readUninstallKey(ks.root, ks.path)
		for _, item := range subItems {
			key := strings.ToLower(strings.TrimSpace(item.DisplayName)) + "#" + strings.TrimSpace(item.DisplayVersion)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				items = append(items, item)
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].DisplayName < items[j].DisplayName
	})

	return items, nil
}

// CollectSnapshot collects a complete inventory snapshot.
func (c *windowsCollector) CollectSnapshot(ctx context.Context) (InventorySnapshot, error) {
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

func (c *windowsCollector) readUninstallKey(root registry.Key, path string) []SoftwareItem {
	k, err := registry.OpenKey(root, path, registry.READ)
	if err != nil {
		return nil
	}
	defer k.Close()

	subkeys, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}

	var list []SoftwareItem
	for _, skName := range subkeys {
		sk, err := registry.OpenKey(k, skName, registry.READ)
		if err != nil {
			continue
		}

		name, _, err := sk.GetStringValue("DisplayName")
		name = strings.TrimSpace(name)
		if err != nil || name == "" {
			sk.Close()
			continue
		}

		// Filter out internal system components or updates without explicit display names
		if sysComp, _, err := sk.GetIntegerValue("SystemComponent"); err == nil && sysComp == 1 {
			sk.Close()
			continue
		}
		if parent, _, err := sk.GetStringValue("ParentKeyName"); err == nil && strings.TrimSpace(parent) != "" {
			sk.Close()
			continue
		}

		version, _, _ := sk.GetStringValue("DisplayVersion")
		publisher, _, _ := sk.GetStringValue("Publisher")
		installDate, _, _ := sk.GetStringValue("InstallDate")
		installLoc, _, _ := sk.GetStringValue("InstallLocation")
		sk.Close()

		list = append(list, SoftwareItem{
			DisplayName:     name,
			DisplayVersion:  strings.TrimSpace(version),
			Publisher:       strings.TrimSpace(publisher),
			InstallDate:     strings.TrimSpace(installDate),
			InstallLocation: strings.TrimSpace(installLoc),
		})
	}

	return list
}

func (c *windowsCollector) collectCPU() CPUInfo {
	info := CPUInfo{
		Cores: runtime.NumCPU(),
	}

	// Read CPU Model and Frequency from Registry
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.READ)
	if err == nil {
		defer k.Close()
		if model, _, err := k.GetStringValue("ProcessorNameString"); err == nil {
			info.Model = strings.TrimSpace(model)
		}
		if mhz, _, err := k.GetIntegerValue("~MHz"); err == nil {
			info.FrequencyMHz = mhz
		}
	}

	info.UsagePercent = c.sampleCPUUsage()
	return info
}

func (c *windowsCollector) sampleCPUUsage() float64 {
	c.cpuMu.Lock()
	defer c.cpuMu.Unlock()

	if procGetSystemTimes.Find() != nil {
		return 0.0
	}

	var idle, kernel, user windows.Filetime
	r1, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r1 == 0 {
		return 0.0
	}

	currIdle := filetimeToUint64(idle)
	currKernel := filetimeToUint64(kernel)
	currUser := filetimeToUint64(user)
	now := time.Now()

	// If we have a recent sample (<5 seconds), calculate usage from delta
	if !c.lastSampleAt.IsZero() && now.Sub(c.lastSampleAt) < 5*time.Second && now.Sub(c.lastSampleAt) > 10*time.Millisecond {
		deltaIdle := currIdle - c.lastIdle
		deltaKernel := currKernel - c.lastKernel
		deltaUser := currUser - c.lastUser
		deltaTotal := deltaKernel + deltaUser

		c.lastIdle = currIdle
		c.lastKernel = currKernel
		c.lastUser = currUser
		c.lastSampleAt = now

		if deltaTotal > 0 && deltaTotal >= deltaIdle {
			pct := float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100.0
			return clampAndRoundPercent(pct)
		}
		return 0.0
	}

	// Otherwise, perform a quick 25ms sample
	time.Sleep(25 * time.Millisecond)
	var idle2, kernel2, user2 windows.Filetime
	procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle2)),
		uintptr(unsafe.Pointer(&kernel2)),
		uintptr(unsafe.Pointer(&user2)),
	)

	currIdle2 := filetimeToUint64(idle2)
	currKernel2 := filetimeToUint64(kernel2)
	currUser2 := filetimeToUint64(user2)

	deltaIdle := currIdle2 - currIdle
	deltaKernel := currKernel2 - currKernel
	deltaUser := currUser2 - currUser
	deltaTotal := deltaKernel + deltaUser

	c.lastIdle = currIdle2
	c.lastKernel = currKernel2
	c.lastUser = currUser2
	c.lastSampleAt = time.Now()

	if deltaTotal > 0 && deltaTotal >= deltaIdle {
		pct := float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100.0
		return clampAndRoundPercent(pct)
	}
	return 0.0
}

func (c *windowsCollector) collectMemory() MemoryInfo {
	var mem MemoryInfo
	if procGlobalMemoryStatusEx.Find() != nil {
		return mem
	}

	var ms memoryStatusEx
	ms.cbSize = uint32(unsafe.Sizeof(ms))
	r1, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	if r1 != 0 {
		mem.TotalBytes = ms.ullTotalPhys
		mem.AvailableBytes = ms.ullAvailPhys
		if ms.ullTotalPhys > 0 {
			used := ms.ullTotalPhys - ms.ullAvailPhys
			pct := float64(used) / float64(ms.ullTotalPhys) * 100.0
			mem.UsedPercent = clampAndRoundPercent(pct)
		}
	}
	return mem
}

func (c *windowsCollector) collectDisks() []DiskPartition {
	var partitions []DiskPartition
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return partitions
	}

	for i := 0; i < 26; i++ {
		if (mask & (1 << i)) == 0 {
			continue
		}
		driveRoot := fmt.Sprintf("%c:\\", 'A'+i)
		p, err := windows.UTF16PtrFromString(driveRoot)
		if err != nil {
			continue
		}

		var freeAvail, total, totalFree uint64
		if err := windows.GetDiskFreeSpaceEx(p, &freeAvail, &total, &totalFree); err == nil && total > 0 {
			var volName [260]uint16
			var fsName [260]uint16
			var serial, maxLen, flags uint32
			fsStr := ""
			if err := windows.GetVolumeInformation(p, &volName[0], uint32(len(volName)), &serial, &maxLen, &flags, &fsName[0], uint32(len(fsName))); err == nil {
				fsStr = syscall.UTF16ToString(fsName[:])
			}

			partitions = append(partitions, DiskPartition{
				MountPoint: driveRoot,
				TotalBytes: total,
				FreeBytes:  totalFree,
				FileSystem: fsStr,
			})
		}
	}

	return partitions
}

func (c *windowsCollector) collectGPU() GPUInfo {
	gpu := GPUInfo{
		Model: "Standard Graphics Adapter",
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`, registry.READ)
	if err != nil {
		return gpu
	}
	defer k.Close()

	subkeys, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return gpu
	}

	for _, sk := range subkeys {
		// Only check numeric entries such as 0000, 0001
		if strings.HasPrefix(sk, "Properties") {
			continue
		}
		subKey, err := registry.OpenKey(k, sk, registry.READ)
		if err != nil {
			continue
		}

		desc, _, errDesc := subKey.GetStringValue("DriverDesc")
		if errDesc == nil && strings.TrimSpace(desc) != "" {
			gpu.Model = strings.TrimSpace(desc)

			// Try 64-bit qwMemorySize, then 32-bit MemorySize
			if vram64, _, errVram := subKey.GetIntegerValue("HardwareInformation.qwMemorySize"); errVram == nil && vram64 > 0 {
				gpu.VRAMBytes = vram64
			} else if vram32, _, errVram := subKey.GetIntegerValue("HardwareInformation.MemorySize"); errVram == nil && vram32 > 0 {
				gpu.VRAMBytes = vram32
			}
			subKey.Close()
			break
		}
		subKey.Close()
	}

	return gpu
}

func (c *windowsCollector) collectNetwork() []NetAdapter {
	var adapters []NetAdapter
	ifaces, err := net.Interfaces()
	if err != nil {
		return adapters
	}

	for _, iface := range ifaces {
		// Filter out down interfaces or interfaces with no name
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

func filetimeToUint64(ft windows.Filetime) uint64 {
	return (uint64(ft.HighDateTime) << 32) | uint64(ft.LowDateTime)
}

func clampAndRoundPercent(val float64) float64 {
	if val < 0.0 {
		val = 0.0
	} else if val > 100.0 {
		val = 100.0
	}
	return math.Round(val*100) / 100
}

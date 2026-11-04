// Package restrictions — system-level restriction policies (JiYu-style
// student machine lockdown): task manager, USB storage, wallpaper, control
// panel and registry editor.
package restrictions

import (
	"fmt"
	"strconv"
)

// SystemRestriction is one named lockdown switch.
type SystemRestriction string

const (
	RestrictTaskMgr       SystemRestriction = "disable_taskmgr"
	RestrictUSBStorage    SystemRestriction = "disable_usb_storage"
	RestrictWallpaper     SystemRestriction = "lock_wallpaper"
	RestrictControlPanel  SystemRestriction = "disable_control_panel"
	RestrictRegistryEdit  SystemRestriction = "disable_registry"
)

// AllSystemRestrictions lists every supported switch.
func AllSystemRestrictions() []SystemRestriction {
	return []SystemRestriction{
		RestrictTaskMgr, RestrictUSBStorage, RestrictWallpaper,
		RestrictControlPanel, RestrictRegistryEdit,
	}
}

// SystemPolicy is the desired set of active restrictions.
type SystemPolicy struct {
	Enabled map[SystemRestriction]bool `json:"enabled"`
}

// Validate rejects unknown restriction names.
func (p SystemPolicy) Validate() error {
	for name := range p.Enabled {
		known := false
		for _, knownName := range AllSystemRestrictions() {
			if name == knownName {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unknown restriction %q", name)
		}
	}
	return nil
}

// registryOp is one concrete registry write/delete the applier must perform.
type registryOp struct {
	Key   string
	Name  string
	Value uint32
	// Delete=true means remove the value (policy off).
	Delete bool
}

// HKCU / HKLM key paths per restriction. USB storage is machine-wide (HKLM,
// requires admin); the rest are per-user policies.
var restrictionOps = map[SystemRestriction][]registryOp{
	RestrictTaskMgr: {{
		Key:   `HKCU\Software\Microsoft\Windows\CurrentVersion\Policies\System`,
		Name:  "DisableTaskMgr",
		Value: 1,
	}},
	RestrictUSBStorage: {{
		Key:   `HKLM\SYSTEM\CurrentControlSet\Services\USBSTOR`,
		Name:  "Start",
		Value: 4,
	}},
	RestrictWallpaper: {{
		Key:   `HKCU\Software\Microsoft\Windows\CurrentVersion\Policies\ActiveDesktop`,
		Name:  "NoChangingWallPaper",
		Value: 1,
	}},
	RestrictControlPanel: {{
		Key:   `HKCU\Software\Microsoft\Windows\CurrentVersion\Policies\Explorer`,
		Name:  "NoControlPanel",
		Value: 1,
	}},
	RestrictRegistryEdit: {{
		Key:   `HKCU\Software\Microsoft\Windows\CurrentVersion\Policies\System`,
		Name:  "DisableRegistryTools",
		Value: 1,
	}},
}

// RegistryCommand is a single shell-level registry instruction produced by
// the policy compiler (executed via `reg`, testable without touching the
// real hive).
type RegistryCommand struct {
	Args []string
}

// CompileSystemPolicy turns a desired policy into an ordered list of
// registry commands. Enabled restrictions are written; previously enabled
// ones missing from the new policy are removed.
func CompileSystemPolicy(current, desired SystemPolicy) []RegistryCommand {
	commands := make([]RegistryCommand, 0, 8)
	for _, name := range AllSystemRestrictions() {
		ops, ok := restrictionOps[name]
		if !ok {
			continue
		}
		want := desired.Enabled[name]
		have := current.Enabled[name]
		if want == have {
			continue
		}
		for _, op := range ops {
			if want {
				if name == RestrictWallpaper && op.Name == "Wallpaper" {
					continue // string value handled by the executor separately
				}
				commands = append(commands, RegistryCommand{
					Args:  []string{"add", op.Key, "/v", op.Name, "/t", "REG_DWORD", "/d", fmtUint32(op.Value), "/f"},
				})
			} else {
				commands = append(commands, RegistryCommand{
					Args: []string{"delete", op.Key, "/v", op.Name, "/f"},
				})
			}
		}
	}
	return commands
}

func fmtUint32(v uint32) string {
	return strconv.FormatUint(uint64(v), 10)
}

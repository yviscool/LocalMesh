package restrictions

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidUSBPolicyMode = errors.New("invalid USB policy mode")
)

type USBPolicyMode string

const (
	USBAllowAll USBPolicyMode = "allow_all"
	USBReadOnly USBPolicyMode = "read_only"
	USBDisabled USBPolicyMode = "disabled"
)

// USBPolicy configures storage restriction for removable USB devices.
type USBPolicy struct {
	Mode      USBPolicyMode `json:"mode"`
	UpdatedAt time.Time     `json:"updated_at,omitempty"`
	Reason    string        `json:"reason,omitempty"`
}

// Validate checks whether the USB policy mode is recognized.
func (p USBPolicy) Validate() error {
	switch p.Mode {
	case USBAllowAll, USBReadOnly, USBDisabled:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidUSBPolicyMode, p.Mode)
	}
}

// USBPort defines the platform port for reading and applying USB storage restrictions.
type USBPort interface {
	ApplyPolicy(ctx context.Context, policy USBPolicy) error
	CurrentPolicy(ctx context.Context) (USBPolicy, error)
}

// USBManager coordinates policy application and audit event generation.
type USBManager struct {
	port  USBPort
	audit AuditSink
	clock func() time.Time
}

// NewUSBManager creates a USBManager with the given platform port and audit sink.
func NewUSBManager(port USBPort, audit AuditSink) *USBManager {
	return &USBManager{
		port:  port,
		audit: audit,
		clock: time.Now,
	}
}

// SetClock configures a custom time source.
func (m *USBManager) SetClock(clock func() time.Time) {
	m.clock = clock
}

// Apply applies a USB restriction policy and records an audit event.
func (m *USBManager) Apply(ctx context.Context, policy USBPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if m.port == nil {
		return errors.New("USB port not configured")
	}

	if policy.UpdatedAt.IsZero() {
		policy.UpdatedAt = m.clock()
	}

	if err := m.port.ApplyPolicy(ctx, policy); err != nil {
		return fmt.Errorf("apply USB policy: %w", err)
	}

	if m.audit != nil {
		_ = m.audit.Record(ctx, AuditEvent{
			Timestamp:  policy.UpdatedAt,
			Action:     AuditUSBPolicyChanged,
			PolicyMode: PolicyMode(policy.Mode),
			Reason:     policy.Reason,
		})
	}
	return nil
}

// Current retrieves the active USB policy from the platform port.
func (m *USBManager) Current(ctx context.Context) (USBPolicy, error) {
	if m.port == nil {
		return USBPolicy{}, errors.New("USB port not configured")
	}
	return m.port.CurrentPolicy(ctx)
}

// RegistryOperator defines the registry key operations needed for Windows USB policies.
type RegistryOperator interface {
	GetDWORD(path, name string) (uint32, error)
	SetDWORD(path, name string, val uint32) error
}

// WindowsRegistryUSBPort implements USBPort using Windows Registry keys.
// Keys:
// 1. USBSTOR Start: HKLM\SYSTEM\CurrentControlSet\Services\USBSTOR (3=Enabled, 4=Disabled)
// 2. StorageDevicePolicies WriteProtect: HKLM\SYSTEM\CurrentControlSet\Control\StorageDevicePolicies (0=RW, 1=RO)
type WindowsRegistryUSBPort struct {
	Registry RegistryOperator
}

const (
	USBStorKey             = `SYSTEM\CurrentControlSet\Services\USBSTOR`
	StoragePoliciesKey     = `SYSTEM\CurrentControlSet\Control\StorageDevicePolicies`
	USBStorStartVal        = "Start"
	StorageWriteProtectVal = "WriteProtect"

	USBStorEnabled       uint32 = 3
	USBStorDisabled      uint32 = 4
	WriteProtectDisabled uint32 = 0
	WriteProtectEnabled  uint32 = 1
)

func (w *WindowsRegistryUSBPort) ApplyPolicy(ctx context.Context, policy USBPolicy) error {
	if w.Registry == nil {
		return errors.New("registry operator not configured")
	}

	switch policy.Mode {
	case USBAllowAll:
		if err := w.Registry.SetDWORD(USBStorKey, USBStorStartVal, USBStorEnabled); err != nil {
			return fmt.Errorf("enable USBSTOR: %w", err)
		}
		if err := w.Registry.SetDWORD(StoragePoliciesKey, StorageWriteProtectVal, WriteProtectDisabled); err != nil {
			return fmt.Errorf("disable WriteProtect: %w", err)
		}
		return nil

	case USBReadOnly:
		if err := w.Registry.SetDWORD(USBStorKey, USBStorStartVal, USBStorEnabled); err != nil {
			return fmt.Errorf("enable USBSTOR: %w", err)
		}
		if err := w.Registry.SetDWORD(StoragePoliciesKey, StorageWriteProtectVal, WriteProtectEnabled); err != nil {
			return fmt.Errorf("enable WriteProtect: %w", err)
		}
		return nil

	case USBDisabled:
		if err := w.Registry.SetDWORD(USBStorKey, USBStorStartVal, USBStorDisabled); err != nil {
			return fmt.Errorf("disable USBSTOR: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("%w: %q", ErrInvalidUSBPolicyMode, policy.Mode)
	}
}

func (w *WindowsRegistryUSBPort) CurrentPolicy(ctx context.Context) (USBPolicy, error) {
	if w.Registry == nil {
		return USBPolicy{}, errors.New("registry operator not configured")
	}

	startVal, err := w.Registry.GetDWORD(USBStorKey, USBStorStartVal)
	if err != nil {
		return USBPolicy{}, fmt.Errorf("read USBSTOR start: %w", err)
	}

	if startVal == USBStorDisabled {
		return USBPolicy{Mode: USBDisabled}, nil
	}

	wpVal, err := w.Registry.GetDWORD(StoragePoliciesKey, StorageWriteProtectVal)
	if err != nil {
		return USBPolicy{Mode: USBAllowAll}, nil
	}

	if wpVal == WriteProtectEnabled {
		return USBPolicy{Mode: USBReadOnly}, nil
	}

	return USBPolicy{Mode: USBAllowAll}, nil
}

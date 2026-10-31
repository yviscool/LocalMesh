package registry

import (
	"errors"
	"sort"
	"sync"

	"localmesh/internal/platform"
)

var ErrDuplicate = errors.New("capability already registered")
var ErrUnknown = errors.New("capability is not registered")
var ErrUnsupported = errors.New("capability is unsupported on platform")

type Descriptor struct {
	ID               string
	Version          uint16
	Dangerous        bool
	Platforms        []platform.OS
	Component        string
	AuditAction      string
	EnabledByDefault bool
}

type Registry struct {
	mu     sync.RWMutex
	values map[string]Descriptor
}

func Default(os platform.OS) (*Registry, error) {
	r := New()
	all := []platform.OS{platform.Windows, platform.Linux, platform.Darwin}
	values := []Descriptor{
		{ID: "classroom.control", Version: 1, Platforms: all, Component: "service", AuditAction: "classroom.control", EnabledByDefault: true},
		{ID: "process.launch", Version: 1, Platforms: all, Component: "worker", AuditAction: "process.launch", EnabledByDefault: false},
		{ID: "process.kill", Version: 1, Dangerous: true, Platforms: all, Component: "worker", AuditAction: "process.kill", EnabledByDefault: false},
		{ID: "power.shutdown", Version: 1, Dangerous: true, Platforms: all, Component: "service", AuditAction: "power.shutdown", EnabledByDefault: false},
		{ID: "power.reboot", Version: 1, Dangerous: true, Platforms: all, Component: "service", AuditAction: "power.reboot", EnabledByDefault: false},
		{ID: "power.wol", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "power.wol", EnabledByDefault: false},
		{ID: "power.logoff", Version: 1, Dangerous: true, Platforms: all, Component: "service", AuditAction: "power.logoff", EnabledByDefault: false},
		{ID: "restrictions.process", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "restrictions.process", EnabledByDefault: false},
		{ID: "restrictions.usb", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "restrictions.usb", EnabledByDefault: false},
		{ID: "file.distribute", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "file.distribute", EnabledByDefault: false},
		{ID: "file.collect", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "file.collect", EnabledByDefault: false},
		{ID: "screenlock.lock", Version: 1, Dangerous: false, Platforms: all, Component: "worker", AuditAction: "screenlock.lock", EnabledByDefault: false},
		{ID: "screenlock.unlock", Version: 1, Dangerous: false, Platforms: all, Component: "worker", AuditAction: "screenlock.unlock", EnabledByDefault: false},
		{ID: "screen.thumbnail", Version: 1, Dangerous: false, Platforms: all, Component: "worker", AuditAction: "screen.thumbnail", EnabledByDefault: false},
		{ID: "screen.stream", Version: 1, Dangerous: false, Platforms: all, Component: "worker", AuditAction: "screen.stream", EnabledByDefault: false},
		{ID: "screen.input", Version: 1, Dangerous: true, Platforms: all, Component: "worker", AuditAction: "screen.input", EnabledByDefault: false},
		{ID: "interaction.chat", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "interaction.chat", EnabledByDefault: false},
		{ID: "interaction.raisehand", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "interaction.raisehand", EnabledByDefault: false},
		{ID: "interaction.seating", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "interaction.seating", EnabledByDefault: false},
		{ID: "quiz.session", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "quiz.session", EnabledByDefault: false},
		{ID: "quiz.buzzer", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "quiz.buzzer", EnabledByDefault: false},
		{ID: "netfilter.firewall", Version: 1, Dangerous: true, Platforms: all, Component: "service", AuditAction: "netfilter.firewall", EnabledByDefault: false},
		{ID: "broadcast.stream", Version: 1, Dangerous: false, Platforms: all, Component: "worker", AuditAction: "broadcast.stream", EnabledByDefault: false},
		{ID: "watchdog.protect", Version: 1, Dangerous: true, Platforms: all, Component: "service", AuditAction: "watchdog.protect", EnabledByDefault: false},
		{ID: "whiteboard.draw", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "whiteboard.draw", EnabledByDefault: false},
		{ID: "remoteshell.exec", Version: 1, Dangerous: true, Platforms: all, Component: "worker", AuditAction: "remoteshell.exec", EnabledByDefault: false},
		{ID: "inventory.collect", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "inventory.collect", EnabledByDefault: false},
		{ID: "attendance.rollcall", Version: 1, Dangerous: false, Platforms: all, Component: "service", AuditAction: "attendance.rollcall", EnabledByDefault: false},
	}
	for _, value := range values {
		if err := r.Register(value); err != nil {
			return nil, err
		}
	}
	if err := r.ValidatePlatform(os); err != nil {
		return nil, err
	}
	return r, nil
}

func New() *Registry { return &Registry{values: map[string]Descriptor{}} }
func (r *Registry) Register(value Descriptor) error {
	if value.ID == "" || value.Version == 0 || value.Component == "" || value.AuditAction == "" {
		return errors.New("invalid capability descriptor")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.values[value.ID]; ok {
		return ErrDuplicate
	}
	r.values[value.ID] = value
	return nil
}
func (r *Registry) Get(id string) (Descriptor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.values[id]
	if !ok {
		return Descriptor{}, ErrUnknown
	}
	return value, nil
}
func (r *Registry) List() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Descriptor, 0, len(r.values))
	for _, v := range r.values {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (r *Registry) Supported(id string, os platform.OS) (bool, error) {
	value, err := r.Get(id)
	if err != nil {
		return false, err
	}
	for _, candidate := range value.Platforms {
		if candidate == os {
			return value.EnabledByDefault, nil
		}
	}
	return false, nil
}

func (r *Registry) ValidatePlatform(os platform.OS) error {
	for _, value := range r.List() {
		if value.EnabledByDefault {
			supported, err := r.Supported(value.ID, os)
			if err != nil {
				return err
			}
			if !supported {
				return ErrUnsupported
			}
		}
	}
	return nil
}

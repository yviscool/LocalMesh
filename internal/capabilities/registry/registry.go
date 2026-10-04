package registry

import (
	"errors"
	"sort"
	"sync"

	"localmesh/internal/platform"
)

var ErrDuplicate = errors.New("capability already registered")
var ErrUnknown = errors.New("capability is not registered")

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

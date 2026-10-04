package common

import (
	"context"
	"sync"
)

type FakeService struct {
	mu                 sync.Mutex
	Installed, Running map[string]bool
}

func NewFakeService() *FakeService {
	return &FakeService{Installed: map[string]bool{}, Running: map[string]bool{}}
}
func (f *FakeService) Install(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Installed[name] = true
	return nil
}
func (f *FakeService) Start(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Running[name] = true
	return nil
}
func (f *FakeService) Stop(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.Running, name)
	return nil
}

type FakeCapabilities struct{ SupportedNames map[string]bool }

func (f FakeCapabilities) Supported(context.Context, string) (bool, error) { return false, nil }

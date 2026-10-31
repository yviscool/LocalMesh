//go:build !windows

package screenlock

import "sync/atomic"

// MockOverlay is a stub overlay for non-Windows platforms.
type MockOverlay struct {
	visible atomic.Bool
	title   string
	message string
}

var defaultMockOverlay = &MockOverlay{}

// NewPlatformOverlay returns the non-Windows platform stub.
func NewPlatformOverlay() Overlay {
	return defaultMockOverlay
}

func (m *MockOverlay) Show(title, message string) error {
	m.title = title
	m.message = message
	m.visible.Store(true)
	return nil
}

func (m *MockOverlay) Hide() error {
	m.visible.Store(false)
	return nil
}

func (m *MockOverlay) Destroy() error {
	return m.Hide()
}

func (m *MockOverlay) Update(title, message string) error {
	m.title = title
	m.message = message
	return nil
}

func (m *MockOverlay) IsVisible() bool {
	return m.visible.Load()
}

// ShowOverlay displays the stub lock screen.
func ShowOverlay(title, message string) error {
	return defaultMockOverlay.Show(title, message)
}

// HideOverlay hides the stub lock screen.
func HideOverlay() error {
	return defaultMockOverlay.Hide()
}

// DestroyOverlay destroys the stub lock screen.
func DestroyOverlay() error {
	return defaultMockOverlay.Destroy()
}

// IsOverlayVisible reports whether the stub overlay is currently active.
func IsOverlayVisible() bool {
	return defaultMockOverlay.IsVisible()
}

// UpdateOverlay updates title and message on the stub overlay.
func UpdateOverlay(title, message string) error {
	return defaultMockOverlay.Update(title, message)
}

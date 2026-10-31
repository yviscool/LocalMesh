//go:build !windows

package screen

// NewPlatformInjector returns a stub injector for non-Windows platforms.
func NewPlatformInjector() Injector {
	return NewMockInjector()
}

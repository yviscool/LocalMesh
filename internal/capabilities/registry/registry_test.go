package registry

import (
	"localmesh/internal/platform"
	"testing"
)

func TestRegistryValidatesAndListsCapabilities(t *testing.T) {
	r := New()
	value := Descriptor{ID: "process.launch", Version: 1, Dangerous: false, Platforms: []platform.OS{platform.Windows, platform.Linux, platform.Darwin}, Component: "service", AuditAction: "process.launch", EnabledByDefault: true}
	if err := r.Register(value); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(value); err != ErrDuplicate {
		t.Fatalf("duplicate=%v", err)
	}
	ok, err := r.Supported("process.launch", platform.Linux)
	if err != nil || !ok {
		t.Fatalf("supported=%v err=%v", ok, err)
	}
	if _, err := r.Get("missing"); err != ErrUnknown {
		t.Fatalf("unknown=%v", err)
	}
	if err := r.ValidatePlatform(platform.Linux); err != nil {
		t.Fatal(err)
	}
	if err := r.ValidatePlatform(platform.Windows); err != nil {
		t.Fatal(err)
	}
}

package registry

import (
	"localmesh/internal/platform"
	"testing"
)

func TestDefaultRegistryIsSafeAndPortable(t *testing.T) {
	for _, os := range []platform.OS{platform.Windows, platform.Linux, platform.Darwin} {
		r, err := Default(os)
		if err != nil {
			t.Fatal(err)
		}
		value, err := r.Get("power.shutdown")
		if err != nil {
			t.Fatal(err)
		}
		if !value.Dangerous || value.EnabledByDefault {
			t.Fatalf("unsafe default: %#v", value)
		}
	}
}

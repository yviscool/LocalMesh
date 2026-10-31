package registry

import (
	"localmesh/internal/platform"
	"testing"
)

func TestDefaultRegistryIsSafeAndPortable(t *testing.T) {
	requiredCaps := []struct {
		id        string
		dangerous bool
	}{
		{"classroom.control", false},
		{"process.launch", false},
		{"process.kill", true},
		{"power.shutdown", true},
		{"power.reboot", true},
		{"power.wol", false},
		{"power.logoff", true},
		{"restrictions.process", false},
		{"restrictions.usb", false},
		{"file.distribute", false},
		{"file.collect", false},
		{"screenlock.lock", false},
		{"screenlock.unlock", false},
		{"screen.thumbnail", false},
		{"screen.stream", false},
		{"screen.input", true},
		{"interaction.chat", false},
		{"interaction.raisehand", false},
		{"interaction.seating", false},
		{"quiz.session", false},
		{"quiz.buzzer", false},
		{"netfilter.firewall", true},
	}

	for _, os := range []platform.OS{platform.Windows, platform.Linux, platform.Darwin} {
		r, err := Default(os)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range requiredCaps {
			value, err := r.Get(tc.id)
			if err != nil {
				t.Fatalf("missing registered capability %s on %s: %v", tc.id, os, err)
			}
			if value.Dangerous != tc.dangerous {
				t.Fatalf("capability %s dangerous mismatch: expected %v, got %v", tc.id, tc.dangerous, value.Dangerous)
			}
			if tc.id != "classroom.control" && value.EnabledByDefault {
				t.Fatalf("capability %s should not be enabled by default", tc.id)
			}
		}
	}
}

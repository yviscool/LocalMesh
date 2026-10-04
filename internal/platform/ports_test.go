package platform

import "testing"

func TestSupportedOperatingSystemsAreExplicit(t *testing.T) {
	for _, value := range []OS{Windows, Linux, Darwin} {
		if value == "" {
			t.Fatal("empty OS")
		}
	}
}

func TestRuntimeRejectsIncompletePorts(t *testing.T) {
	if err := (Runtime{}).Validate(); err == nil {
		t.Fatal("incomplete runtime accepted")
	}
}

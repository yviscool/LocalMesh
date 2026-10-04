package platform

import "testing"

func TestSupportedOperatingSystemsAreExplicit(t *testing.T) {
	for _, value := range []OS{Windows, Linux, Darwin} {
		if value == "" {
			t.Fatal("empty OS")
		}
	}
}

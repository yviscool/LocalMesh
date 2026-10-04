package common

import "testing"

func TestRuntimeForCurrentOSProvidesCompletePorts(t *testing.T) {
	runtime, err := RuntimeForCurrentOS()
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Validate(); err != nil {
		t.Fatal(err)
	}
}

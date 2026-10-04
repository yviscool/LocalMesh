package bootstrap

import (
	"localmesh/internal/capabilities/registry"
	"localmesh/internal/platform/common"
	"testing"
)

func TestComposeValidatesPlatformAndCapabilitiesTogether(t *testing.T) {
	runtime, err := common.RuntimeForCurrentOS()
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := registry.Default(runtime.OS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compose(runtime, capabilities); err != nil {
		t.Fatal(err)
	}
}

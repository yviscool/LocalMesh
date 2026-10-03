package discovery

import (
	"net/netip"
	"testing"

	"localmesh/internal/domain/identity"
)

func TestObservationIsNotAuthorization(t *testing.T) {
	deviceID, err := identity.NewDeviceID("CPC-8F23-19A7")
	if err != nil {
		t.Fatal(err)
	}
	o := Observation{DeviceID: deviceID, Address: netip.MustParseAddrPort("192.168.1.20:7443"), Reachable: true}
	if !o.Candidate() {
		t.Fatal("reachable observation should be a pairing candidate")
	}
	// The observation has no classroom membership or capability and therefore grants no control.
}

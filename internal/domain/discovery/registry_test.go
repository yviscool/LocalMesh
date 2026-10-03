package discovery

import (
	"net/netip"
	"sync"
	"testing"
	"time"

	"localmesh/internal/domain/identity"
)

func TestRegistryExpiresStaleObservations(t *testing.T) {
	deviceID, err := identity.NewDeviceID("CPC-8F23-19A7")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	r := NewRegistry()
	r.Observe(Observation{DeviceID: deviceID, Address: netip.MustParseAddrPort("192.168.1.20:7443"), SeenAt: now, Reachable: true})
	if got := len(r.Candidates(now.Add(10*time.Second), time.Minute)); got != 1 {
		t.Fatalf("Candidates() = %d, want 1", got)
	}
	if got := len(r.Candidates(now.Add(2*time.Minute), time.Minute)); got != 0 {
		t.Fatalf("Candidates() = %d, want 0", got)
	}
}

func TestRegistrySupportsConcurrentObservation(t *testing.T) {
	deviceID, err := identity.NewDeviceID("CPC-8F23-19A7")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	observation := Observation{DeviceID: deviceID, Address: netip.MustParseAddrPort("192.168.1.20:7443"), SeenAt: time.Now(), Reachable: true}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Observe(observation)
			_ = r.Candidates(time.Now(), time.Minute)
		}()
	}
	wg.Wait()
}

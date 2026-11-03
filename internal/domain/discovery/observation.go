package discovery

import (
	"net/netip"
	"time"

	"localmesh/internal/domain/identity"
)

type Method string

const (
	MethodServer    Method = "server"
	MethodMulticast Method = "multicast"
	MethodBroadcast Method = "broadcast"
	MethodMDNS      Method = "mdns"
	MethodManual    Method = "manual"
)

// Observation is evidence about a device, never an authorization decision.
type Observation struct {
	DeviceID  identity.DeviceID
	Address   netip.AddrPort
	Method    Method
	SeenAt    time.Time
	TLSName   string
	Reachable bool
	// ChannelID is the classroom channel announced by the device (1-255).
	// Zero means legacy announcement, treated as the default channel.
	ChannelID int
}

func (o Observation) Candidate() bool {
	return o.DeviceID != "" && o.Address.IsValid() && o.Reachable
}

package platform

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// AdapterType categorizes the physical or virtual nature and speed tier of a network interface.
type AdapterType string

const (
	AdapterTypeGigabitEthernet AdapterType = "gigabit_ethernet" // Physical Wired Gigabit / Multi-Gigabit (>= 1000 Mbps)
	AdapterTypeEthernet        AdapterType = "ethernet"         // Physical Wired Standard Ethernet (< 1000 Mbps or unspecified)
	AdapterTypeWiFi            AdapterType = "wifi"             // Wireless 802.11 LAN
	AdapterTypeVirtual         AdapterType = "virtual"          // Virtual interface (VMware, VirtualBox, Hyper-V, WSL, Docker, VPN)
	AdapterTypeLoopback        AdapterType = "loopback"         // Loopback interface (127.0.0.1)
	AdapterTypeUnknown         AdapterType = "unknown"
)

var (
	ErrNoSuitableAdapter = errors.New("no suitable classroom network adapter found")
	ErrNilAdapter        = errors.New("network adapter cannot be nil")
	ErrAdapterDown       = errors.New("selected network adapter is administratively down")
)

// NetworkAdapter contains analyzed network interface metadata and smart classroom priority ranking.
type NetworkAdapter struct {
	Index             int           `json:"index"`
	Name              string        `json:"name"`
	Description       string        `json:"description,omitempty"`
	HardwareAddr      string        `json:"hardware_addr"`
	IPAddresses       []net.IP      `json:"ip_addresses"`
	Type              AdapterType   `json:"type"`
	SpeedMbps         int64         `json:"speed_mbps"`
	IsPhysical        bool          `json:"is_physical"`
	IsUp              bool          `json:"is_up"`
	SupportsMulticast bool          `json:"supports_multicast"`
	Score             int           `json:"score"`
	Interface         net.Interface `json:"-"`
}

// virtualKeywords are substrings found in virtual adapter names or descriptions.
var virtualKeywords = []string{
	"vmware", "vmnet",
	"virtualbox", "vbox", "vboxnet",
	"hyper-v", "vethernet", "default switch", "wsl",
	"docker", "cni", "flannel", "weave", "kube", "podman",
	"tap", "tun", "utun", "wireguard", "wg", "tailscale", "zerotier",
	"openvpn", "ppp", "bridge", "br-", "veth",
}

// virtualMACPrefixes are standard IEEE OUI prefixes assigned to virtual machine hypervisors.
var virtualMACPrefixes = []string{
	"00:05:69", // VMware
	"00:0c:29", // VMware
	"00:50:56", // VMware
	"08:00:27", // VirtualBox
	"00:15:5d", // Hyper-V
	"52:54:00", // QEMU / KVM
}

// wifiKeywords identify wireless interfaces.
var wifiKeywords = []string{
	"wi-fi", "wifi", "wlan", "wireless", "802.11", "airport", "wl",
}

// gigabitKeywords identify gigabit or multi-gigabit wired adapters.
var gigabitKeywords = []string{
	"gigabit", "gbe", "1000m", "1000 mbps", "1g", "2.5g", "5g", "10g",
	"pcie gbe", "i21", "i22", "e1000", "tg3", "bcm57", "rtl8111", "rtl8125",
}

// IdentifyAdapter analyzes a network interface and assigns its adapter type, physical classification, and score.
func IdentifyAdapter(iface net.Interface, description string, speedMbps int64) NetworkAdapter {
	nameLower := strings.ToLower(iface.Name)
	descLower := strings.ToLower(description)
	combined := nameLower + " " + descLower
	mac := strings.ToLower(iface.HardwareAddr.String())

	isUp := iface.Flags&net.FlagUp != 0
	supportsMulticast := iface.Flags&net.FlagMulticast != 0
	isLoopback := iface.Flags&net.FlagLoopback != 0 || nameLower == "lo" || strings.Contains(combined, "loopback")

	adapter := NetworkAdapter{
		Index:             iface.Index,
		Name:              iface.Name,
		Description:       description,
		HardwareAddr:      iface.HardwareAddr.String(),
		IsUp:              isUp,
		SupportsMulticast: supportsMulticast,
		Interface:         iface,
		SpeedMbps:         speedMbps,
	}

	// 1. Loopback
	if isLoopback {
		adapter.Type = AdapterTypeLoopback
		adapter.IsPhysical = false
		adapter.Score = 0
		return adapter
	}

	// 2. Virtual Adapter Detection
	isVirtual := false
	for _, kw := range virtualKeywords {
		if strings.Contains(combined, kw) {
			isVirtual = true
			break
		}
	}
	if !isVirtual && len(mac) >= 8 {
		for _, prefix := range virtualMACPrefixes {
			if strings.HasPrefix(mac, prefix) {
				isVirtual = true
				break
			}
		}
	}

	if isVirtual {
		adapter.Type = AdapterTypeVirtual
		adapter.IsPhysical = false
		adapter.Score = calculateScore(&adapter)
		return adapter
	}

	// 3. Wi-Fi Detection
	isWiFi := false
	for _, kw := range wifiKeywords {
		if strings.Contains(combined, kw) {
			isWiFi = true
			break
		}
	}

	if isWiFi {
		adapter.Type = AdapterTypeWiFi
		adapter.IsPhysical = true
		adapter.Score = calculateScore(&adapter)
		return adapter
	}

	// 4. Wired Ethernet
	adapter.IsPhysical = true
	isGigabit := speedMbps >= 1000
	if !isGigabit {
		for _, kw := range gigabitKeywords {
			if strings.Contains(combined, kw) {
				isGigabit = true
				if adapter.SpeedMbps < 1000 {
					adapter.SpeedMbps = 1000
				}
				break
			}
		}
	}

	if isGigabit {
		adapter.Type = AdapterTypeGigabitEthernet
	} else {
		adapter.Type = AdapterTypeEthernet
		if adapter.SpeedMbps == 0 {
			adapter.SpeedMbps = 100 // Standard Fast Ethernet baseline
		}
	}

	adapter.Score = calculateScore(&adapter)
	return adapter
}

// calculateScore computes the smart prioritization score for an adapter.
// Higher score indicates a higher preference for classroom broadcast transmission.
func calculateScore(a *NetworkAdapter) int {
	// Adapters that are down or cannot multicast receive 0 score
	if !a.IsUp || !a.SupportsMulticast {
		return 0
	}

	score := 0
	switch a.Type {
	case AdapterTypeGigabitEthernet:
		score = 10000 // Highest priority: Physical Wired Gigabit Dedicated NIC
	case AdapterTypeEthernet:
		score = 7000 // Physical Wired Fast Ethernet
	case AdapterTypeWiFi:
		score = 4000 // Wi-Fi has lower stability than wired
	case AdapterTypeVirtual:
		score = 500 // Virtual adapters are deprioritized
	default:
		score = 100
	}

	if a.IsPhysical {
		score += 1000
	}

	// Prefer interfaces with active IPv4 addresses
	hasIPv4 := false
	for _, ip := range a.IPAddresses {
		if ip4 := ip.To4(); ip4 != nil && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
			hasIPv4 = true
			break
		}
	}
	if hasIPv4 {
		score += 500
	}

	// Link speed tie-breaker
	if a.SpeedMbps > 0 {
		score += int(a.SpeedMbps / 10)
	}

	return score
}

// EnumerateAdapters discovers all system network interfaces, evaluates their characteristics,
// and returns them sorted by priority score descending.
func EnumerateAdapters() ([]NetworkAdapter, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate network interfaces: %w", err)
	}

	adapters := make([]NetworkAdapter, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		var ips []net.IP
		if err == nil {
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok {
					ips = append(ips, ipNet.IP)
				} else if ipAddr, ok := addr.(*net.IPAddr); ok {
					ips = append(ips, ipAddr.IP)
				}
			}
		}

		adapter := IdentifyAdapter(iface, "", 0)
		adapter.IPAddresses = ips
		adapter.Score = calculateScore(&adapter)
		adapters = append(adapters, adapter)
	}

	// Sort by Score descending
	sort.Slice(adapters, func(i, j int) bool {
		return adapters[i].Score > adapters[j].Score
	})

	return adapters, nil
}

// SelectClassroomAdapter selects the best adapter from a given slice of adapters.
// Prioritizes Physical Wired Gigabit Ethernet over Wi-Fi and Virtual adapters.
func SelectClassroomAdapter(adapters []NetworkAdapter) (*NetworkAdapter, error) {
	if len(adapters) == 0 {
		return nil, ErrNoSuitableAdapter
	}

	var best *NetworkAdapter
	maxScore := 0

	for i := range adapters {
		a := &adapters[i]
		if a.Score > maxScore {
			maxScore = a.Score
			best = a
		}
	}

	if best == nil || maxScore == 0 {
		return nil, ErrNoSuitableAdapter
	}

	return best, nil
}

// GetBestClassroomAdapter automatically discovers and selects the optimal physical NIC.
func GetBestClassroomAdapter() (*NetworkAdapter, error) {
	adapters, err := EnumerateAdapters()
	if err != nil {
		return nil, err
	}
	return SelectClassroomAdapter(adapters)
}

// BindMulticastListener binds a UDP multicast listener to the specified network adapter.
// This ensures classroom multicast packets are received on the dedicated physical adapter.
func BindMulticastListener(adapter *NetworkAdapter, multicastAddr string) (*net.UDPConn, error) {
	if adapter == nil {
		return nil, ErrNilAdapter
	}
	if !adapter.IsUp {
		return nil, ErrAdapterDown
	}

	udpAddr, err := net.ResolveUDPAddr("udp4", multicastAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve multicast address failed: %w", err)
	}

	// Bind specifically to adapter.Interface
	conn, err := net.ListenMulticastUDP("udp4", &adapter.Interface, udpAddr)
	if err != nil {
		return nil, fmt.Errorf("listen multicast on %s failed: %w", adapter.Name, err)
	}

	return conn, nil
}

// BindMulticastSender creates a UDP connection for broadcasting/multicasting outbound packets
// bound to the specified adapter's IPv4 address.
func BindMulticastSender(adapter *NetworkAdapter) (*net.UDPConn, error) {
	if adapter == nil {
		return nil, ErrNilAdapter
	}
	if !adapter.IsUp {
		return nil, ErrAdapterDown
	}

	// Find the adapter's primary IPv4 address
	var localIP net.IP
	for _, ip := range adapter.IPAddresses {
		if ip4 := ip.To4(); ip4 != nil && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
			localIP = ip4
			break
		}
	}

	localAddr := ":0"
	if localIP != nil {
		localAddr = net.JoinHostPort(localIP.String(), "0")
	}

	lAddr, err := net.ResolveUDPAddr("udp4", localAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve local sender address failed: %w", err)
	}

	conn, err := net.ListenUDP("udp4", lAddr)
	if err != nil {
		return nil, fmt.Errorf("bind sender on %s failed: %w", adapter.Name, err)
	}

	return conn, nil
}

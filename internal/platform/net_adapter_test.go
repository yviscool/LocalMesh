package platform

import (
	"net"
	"testing"
)

func TestIdentifyAdapterTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		iface        net.Interface
		description  string
		speed        int64
		expectedType AdapterType
		isPhysical   bool
	}{
		{
			name: "Intel Gigabit Ethernet",
			iface: net.Interface{
				Index:        1,
				Name:         "Ethernet 1",
				HardwareAddr: net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
				Flags:        net.FlagUp | net.FlagMulticast,
			},
			description:  "Intel(R) Ethernet Connection I219-V Gigabit",
			speed:        1000,
			expectedType: AdapterTypeGigabitEthernet,
			isPhysical:   true,
		},
		{
			name: "Realtek Fast Ethernet",
			iface: net.Interface{
				Index:        2,
				Name:         "eth0",
				HardwareAddr: net.HardwareAddr{0x00, 0x1A, 0x2B, 0x3C, 0x4D, 0x5E},
				Flags:        net.FlagUp | net.FlagMulticast,
			},
			description:  "Realtek PCIe FE Family Controller",
			speed:        100,
			expectedType: AdapterTypeEthernet,
			isPhysical:   true,
		},
		{
			name: "Intel Wi-Fi 6 Adapter",
			iface: net.Interface{
				Index:        3,
				Name:         "Wi-Fi",
				HardwareAddr: net.HardwareAddr{0x60, 0x57, 0x18, 0x11, 0x22, 0x33},
				Flags:        net.FlagUp | net.FlagMulticast,
			},
			description:  "Intel(R) Wi-Fi 6 AX200 160MHz",
			speed:        300,
			expectedType: AdapterTypeWiFi,
			isPhysical:   true,
		},
		{
			name: "VMware Virtual Adapter by Name",
			iface: net.Interface{
				Index:        4,
				Name:         "VMware Network Adapter VMnet1",
				HardwareAddr: net.HardwareAddr{0x00, 0x50, 0x56, 0xC0, 0x00, 0x01},
				Flags:        net.FlagUp | net.FlagMulticast,
			},
			description:  "VMware Virtual Ethernet Adapter for VMnet1",
			speed:        100,
			expectedType: AdapterTypeVirtual,
			isPhysical:   false,
		},
		{
			name: "VirtualBox Adapter by OUI MAC",
			iface: net.Interface{
				Index:        5,
				Name:         "vboxnet0",
				HardwareAddr: net.HardwareAddr{0x08, 0x00, 0x27, 0x12, 0x34, 0x56},
				Flags:        net.FlagUp | net.FlagMulticast,
			},
			description:  "VirtualBox Host-Only Ethernet Adapter",
			speed:        1000,
			expectedType: AdapterTypeVirtual,
			isPhysical:   false,
		},
		{
			name: "Hyper-V Virtual Switch",
			iface: net.Interface{
				Index:        6,
				Name:         "vEthernet (WSL)",
				HardwareAddr: net.HardwareAddr{0x00, 0x15, 0x5D, 0xAA, 0xBB, 0xCC},
				Flags:        net.FlagUp | net.FlagMulticast,
			},
			description:  "Hyper-V Virtual Ethernet Adapter",
			speed:        10000,
			expectedType: AdapterTypeVirtual,
			isPhysical:   false,
		},
		{
			name: "Loopback Interface",
			iface: net.Interface{
				Index: 7,
				Name:  "lo",
				Flags: net.FlagUp | net.FlagLoopback,
			},
			description:  "Software Loopback",
			speed:        0,
			expectedType: AdapterTypeLoopback,
			isPhysical:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adapter := IdentifyAdapter(tc.iface, tc.description, tc.speed)
			if adapter.Type != tc.expectedType {
				t.Errorf("expected type %s, got %s", tc.expectedType, adapter.Type)
			}
			if adapter.IsPhysical != tc.isPhysical {
				t.Errorf("expected IsPhysical=%v, got %v", tc.isPhysical, adapter.IsPhysical)
			}
		})
	}
}

func TestSmartClassroomAdapterSelection(t *testing.T) {
	t.Parallel()

	// Construct a realistic multi-NIC lab workstation:
	// - VMware virtual adapter
	// - VirtualBox virtual adapter
	// - Wi-Fi adapter
	// - Physical Wired Gigabit Ethernet
	// - Down secondary ethernet adapter

	vmware := IdentifyAdapter(net.Interface{
		Index:        10,
		Name:         "VMware Network Adapter VMnet8",
		HardwareAddr: net.HardwareAddr{0x00, 0x50, 0x56, 0xC0, 0x00, 0x08},
		Flags:        net.FlagUp | net.FlagMulticast,
	}, "VMware Virtual Ethernet Adapter", 100)
	vmware.IPAddresses = []net.IP{net.ParseIP("192.168.100.1")}
	vmware.Score = calculateScore(&vmware)

	vbox := IdentifyAdapter(net.Interface{
		Index:        11,
		Name:         "VirtualBox Host-Only Network",
		HardwareAddr: net.HardwareAddr{0x08, 0x00, 0x27, 0x00, 0x11, 0x22},
		Flags:        net.FlagUp | net.FlagMulticast,
	}, "VirtualBox Adapter", 1000)
	vbox.IPAddresses = []net.IP{net.ParseIP("192.168.56.1")}
	vbox.Score = calculateScore(&vbox)

	wifi := IdentifyAdapter(net.Interface{
		Index:        12,
		Name:         "Wi-Fi",
		HardwareAddr: net.HardwareAddr{0x34, 0x29, 0x8F, 0x11, 0x22, 0x33},
		Flags:        net.FlagUp | net.FlagMulticast,
	}, "Intel Wi-Fi 6 Adapter", 300)
	wifi.IPAddresses = []net.IP{net.ParseIP("10.0.0.15")}
	wifi.Score = calculateScore(&wifi)

	gigabit := IdentifyAdapter(net.Interface{
		Index:        13,
		Name:         "Ethernet",
		HardwareAddr: net.HardwareAddr{0x00, 0x1A, 0x2B, 0x44, 0x55, 0x66},
		Flags:        net.FlagUp | net.FlagMulticast,
	}, "Intel(R) Ethernet Connection I219-LM Gigabit", 1000)
	gigabit.IPAddresses = []net.IP{net.ParseIP("172.16.1.50")}
	gigabit.Score = calculateScore(&gigabit)

	downEthernet := IdentifyAdapter(net.Interface{
		Index:        14,
		Name:         "Ethernet 2",
		HardwareAddr: net.HardwareAddr{0x00, 0x1A, 0x2B, 0x77, 0x88, 0x99},
		Flags:        0, // Down
	}, "Realtek Gigabit Ethernet", 1000)
	downEthernet.Score = calculateScore(&downEthernet)

	adapters := []NetworkAdapter{vmware, vbox, wifi, gigabit, downEthernet}

	// 1. Primary Test: Physical Wired Gigabit must win
	best, err := SelectClassroomAdapter(adapters)
	if err != nil {
		t.Fatalf("SelectClassroomAdapter failed: %v", err)
	}
	if best.Name != "Ethernet" || best.Type != AdapterTypeGigabitEthernet {
		t.Fatalf("expected Gigabit Ethernet to win, got %s (%s, score=%d)", best.Name, best.Type, best.Score)
	}
	if best.Score <= wifi.Score || best.Score <= vmware.Score {
		t.Errorf("gigabit score (%d) should be strictly higher than wifi (%d) and vmware (%d)", best.Score, wifi.Score, vmware.Score)
	}

	// 2. Secondary Test: If Gigabit Ethernet unplugged/down, Wi-Fi should take over before Virtual adapters
	fallbackAdapters := []NetworkAdapter{vmware, vbox, wifi, downEthernet}
	fallbackBest, err := SelectClassroomAdapter(fallbackAdapters)
	if err != nil {
		t.Fatalf("fallback SelectClassroomAdapter failed: %v", err)
	}
	if fallbackBest.Name != "Wi-Fi" || fallbackBest.Type != AdapterTypeWiFi {
		t.Fatalf("expected Wi-Fi to win when wired is down, got %s (%s)", fallbackBest.Name, fallbackBest.Type)
	}

	// 3. Down adapter receives 0 score and is not eligible
	if downEthernet.Score != 0 {
		t.Errorf("down adapter should have score 0, got %d", downEthernet.Score)
	}
}

func TestEnumerateAdaptersLive(t *testing.T) {
	t.Parallel()

	adapters, err := EnumerateAdapters()
	if err != nil {
		t.Fatalf("EnumerateAdapters failed on live system: %v", err)
	}

	if len(adapters) == 0 {
		t.Skip("no network adapters found on test host")
	}

	for _, a := range adapters {
		t.Logf("Adapter: %s, Type: %s, Physical: %v, Up: %v, Multicast: %v, Score: %d",
			a.Name, a.Type, a.IsPhysical, a.IsUp, a.SupportsMulticast, a.Score)
	}
}

func TestBindMulticastErrors(t *testing.T) {
	t.Parallel()

	// Nil adapter error
	if _, err := BindMulticastListener(nil, "239.255.42.1:8899"); err != ErrNilAdapter {
		t.Errorf("expected ErrNilAdapter, got %v", err)
	}
	if _, err := BindMulticastSender(nil); err != ErrNilAdapter {
		t.Errorf("expected ErrNilAdapter, got %v", err)
	}

	// Down adapter error
	downAdapter := &NetworkAdapter{
		Name: "DownInterface",
		IsUp: false,
	}
	if _, err := BindMulticastListener(downAdapter, "239.255.42.1:8899"); err != ErrAdapterDown {
		t.Errorf("expected ErrAdapterDown, got %v", err)
	}
	if _, err := BindMulticastSender(downAdapter); err != ErrAdapterDown {
		t.Errorf("expected ErrAdapterDown, got %v", err)
	}
}

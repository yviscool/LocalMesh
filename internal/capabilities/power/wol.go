package power

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

var (
	ErrInvalidMAC           = errors.New("invalid MAC address: must be 6 bytes")
	ErrInvalidBroadcastAddr = errors.New("invalid broadcast address")
)

const (
	macLen         = 6
	magicPacketLen = 6 + 16*macLen // 102 bytes
	defaultWOLPort = "9"
)

// BuildMagicPacket constructs a Wake-on-LAN magic packet consisting of
// 6 bytes of 0xFF followed by 16 repetitions of the 6-byte target MAC address.
func BuildMagicPacket(mac net.HardwareAddr) ([]byte, error) {
	if len(mac) != macLen {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidMAC, len(mac))
	}

	packet := make([]byte, magicPacketLen)
	for i := 0; i < 6; i++ {
		packet[i] = 0xFF
	}
	for i := 0; i < 16; i++ {
		copy(packet[6+i*macLen:6+(i+1)*macLen], mac)
	}
	return packet, nil
}

// SendWakeOnLAN broadcasts a magic packet to the specified broadcast address.
// If broadcastAddr is empty, "255.255.255.255:9" is used.
// If broadcastAddr does not specify a port, default port 9 is appended.
func SendWakeOnLAN(mac net.HardwareAddr, broadcastAddr string) error {
	packet, err := BuildMagicPacket(mac)
	if err != nil {
		return err
	}

	addrStr := strings.TrimSpace(broadcastAddr)
	if addrStr == "" {
		addrStr = net.JoinHostPort("255.255.255.255", defaultWOLPort)
	} else {
		_, _, err := net.SplitHostPort(addrStr)
		if err != nil {
			addrStr = net.JoinHostPort(addrStr, defaultWOLPort)
		}
	}

	dstAddr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBroadcastAddr, err)
	}

	conn, err := net.DialUDP("udp", nil, dstAddr)
	if err != nil {
		return fmt.Errorf("dial broadcast UDP: %w", err)
	}
	defer conn.Close()

	n, err := conn.Write(packet)
	if err != nil {
		return fmt.Errorf("send magic packet: %w", err)
	}
	if n != len(packet) {
		return fmt.Errorf("short write: sent %d of %d bytes", n, len(packet))
	}
	return nil
}

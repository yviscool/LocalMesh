package classroom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	MinChannelID     = 1
	MaxChannelID     = 255
	DefaultChannelID = 1

	DefaultBaseMulticastAddr = "239.255.42.0:8899"
	DefaultMulticastPort     = "8899"
	CurrentHandshakeVersion  = 1
)

// HandshakeMagic identifies LocalMesh Handshake Packets: "LMHS"
var HandshakeMagic = [4]byte{'L', 'M', 'H', 'S'}

var (
	ErrInvalidChannelID       = errors.New("channel ID must be between 1 and 255")
	ErrEmptyClassroomID       = errors.New("classroom ID cannot be empty")
	ErrInvalidHandshakePacket = errors.New("invalid classroom handshake packet")
	ErrInvalidMagic           = errors.New("invalid handshake packet magic bytes")
	ErrUnsupportedVersion     = errors.New("unsupported handshake packet version")
	ErrChannelMismatch        = errors.New("packet channel ID does not match receiver channel")
	ErrFingerprintMismatch    = errors.New("packet classroom fingerprint does not match receiver")
)

// ValidateChannelID verifies channel ID is within legal range [1, 255].
func ValidateChannelID(id int) error {
	if id < MinChannelID || id > MaxChannelID {
		return fmt.Errorf("%w: got %d", ErrInvalidChannelID, id)
	}
	return nil
}

// SanitizeChannelID normalizes channel ID, returning DefaultChannelID (1) if out of bounds.
func SanitizeChannelID(id int) int {
	if id < MinChannelID || id > MaxChannelID {
		return DefaultChannelID
	}
	return id
}

// DeriveMulticastAddr derives an isolated multicast IP and port for the given channelID.
// For example: channelID=42, baseAddr="239.255.42.0:8899" yields "239.255.42.42:8899".
// If baseAddr is empty or invalid, DefaultBaseMulticastAddr is used.
func DeriveMulticastAddr(channelID int, baseAddr string) string {
	cid := SanitizeChannelID(channelID)

	target := strings.TrimSpace(baseAddr)
	if target == "" {
		target = DefaultBaseMulticastAddr
	}

	host, port, err := net.SplitHostPort(target)
	if err != nil {
		// If baseAddr had no port specified (e.g. "239.255.42.0")
		host = target
		port = DefaultMulticastPort
	}

	ip := net.ParseIP(host).To4()
	if ip == nil {
		// Fallback to default base IPv4 multicast group
		ip = net.ParseIP("239.255.42.0").To4()
	}

	// Substitute the 4th octet with channelID (1~255)
	ip[3] = byte(cid)

	if port == "" {
		port = DefaultMulticastPort
	}

	return net.JoinHostPort(ip.String(), port)
}

// DeriveClassroomFingerprint generates a deterministic, collision-resistant fingerprint
// for logical isolation of classrooms and channels sharing the same physical network.
func DeriveClassroomFingerprint(classroomID string, channelID int) string {
	trimmedID := strings.TrimSpace(classroomID)
	cid := SanitizeChannelID(channelID)

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "LocalMesh-RoomFingerprint:v1:%s:%d", trimmedID, cid)
	sum := h.Sum(nil)
	return "CFP-" + hex.EncodeToString(sum[:16])
}

// VerifyClassroomFingerprint checks if a fingerprint matches the given classroom and channel.
func VerifyClassroomFingerprint(classroomID string, channelID int, fingerprint string) bool {
	expected := DeriveClassroomFingerprint(classroomID, channelID)
	return strings.TrimSpace(fingerprint) == expected
}

// HandshakePacket represents a control and discovery packet exchanged between teacher and students.
// Carries ChannelID and ClassroomFingerprint for zero-crosstalk packet filtering.
type HandshakePacket struct {
	Magic                [4]byte `json:"-"`
	Version              uint8   `json:"version"`
	ChannelID            int     `json:"channel_id"`
	ClassroomFingerprint string  `json:"classroom_fingerprint"`
	ClassroomID          string  `json:"classroom_id"`
	DeviceID             string  `json:"device_id"`
	Role                 string  `json:"role"` // e.g. "teacher", "student"
	Timestamp            int64   `json:"timestamp"`
	Payload              []byte  `json:"payload,omitempty"`
}

// Marshal encodes HandshakePacket into binary datagram with 5-byte header [LMHS][ver] + JSON body.
func (p *HandshakePacket) Marshal() ([]byte, error) {
	if p.ChannelID < MinChannelID || p.ChannelID > MaxChannelID {
		return nil, ErrInvalidChannelID
	}
	if strings.TrimSpace(p.ClassroomFingerprint) == "" {
		return nil, ErrFingerprintMismatch
	}

	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal handshake failed: %w", err)
	}

	buf := make([]byte, 5+len(body))
	copy(buf[0:4], HandshakeMagic[:])
	buf[4] = CurrentHandshakeVersion
	copy(buf[5:], body)

	return buf, nil
}

// Unmarshal decodes a binary datagram into HandshakePacket.
func (p *HandshakePacket) Unmarshal(data []byte) error {
	if len(data) < 5 {
		return ErrInvalidHandshakePacket
	}
	if data[0] != HandshakeMagic[0] || data[1] != HandshakeMagic[1] || data[2] != HandshakeMagic[2] || data[3] != HandshakeMagic[3] {
		return ErrInvalidMagic
	}
	if data[4] != CurrentHandshakeVersion {
		return ErrUnsupportedVersion
	}

	p.Magic = HandshakeMagic
	p.Version = data[4]

	if err := json.Unmarshal(data[5:], p); err != nil {
		return fmt.Errorf("%w: json decode failed: %v", ErrInvalidHandshakePacket, err)
	}
	return nil
}

// ChannelFilter inspects incoming packets and drops non-matching classroom or channel packets silently.
type ChannelFilter struct {
	channelID            int
	classroomFingerprint string
}

// NewChannelFilter constructs a ChannelFilter for the specified channel and fingerprint.
func NewChannelFilter(channelID int, classroomFingerprint string) *ChannelFilter {
	return &ChannelFilter{
		channelID:            SanitizeChannelID(channelID),
		classroomFingerprint: strings.TrimSpace(classroomFingerprint),
	}
}

// Accept checks whether a HandshakePacket matches the filter criteria.
// Returns false if packet is from another room or channel (indicating it should be dropped silently).
func (f *ChannelFilter) Accept(pkt *HandshakePacket) bool {
	if pkt == nil {
		return false
	}
	if pkt.ChannelID != f.channelID {
		return false
	}
	if f.classroomFingerprint != "" && pkt.ClassroomFingerprint != f.classroomFingerprint {
		return false
	}
	return true
}

// FilterRaw parses a raw UDP datagram. If the datagram belongs to this channel and classroom,
// it returns the decoded packet and true. Otherwise, it returns nil and false (silent discard).
func (f *ChannelFilter) FilterRaw(data []byte) (*HandshakePacket, bool) {
	var pkt HandshakePacket
	if err := pkt.Unmarshal(data); err != nil {
		return nil, false // Silently discard corrupt or foreign protocol packet
	}
	if !f.Accept(&pkt) {
		return nil, false // Silently discard packets from another room/channel
	}
	return &pkt, true
}

// ClassroomChannel bundles channel ID, multicast address, fingerprint, and packet filtering.
type ClassroomChannel struct {
	ClassroomID   string         `json:"classroom_id"`
	ChannelID     int            `json:"channel_id"`
	Fingerprint   string         `json:"fingerprint"`
	MulticastAddr string         `json:"multicast_addr"`
	Filter        *ChannelFilter `json:"-"`
}

// NewClassroomChannel creates a ClassroomChannel instance with default Channel 1 if unspecified.
func NewClassroomChannel(classroomID string, channelID int, baseAddr string) (*ClassroomChannel, error) {
	cID := strings.TrimSpace(classroomID)
	if cID == "" {
		return nil, ErrEmptyClassroomID
	}
	chID := SanitizeChannelID(channelID)
	fp := DeriveClassroomFingerprint(cID, chID)
	mAddr := DeriveMulticastAddr(chID, baseAddr)

	return &ClassroomChannel{
		ClassroomID:   cID,
		ChannelID:     chID,
		Fingerprint:   fp,
		MulticastAddr: mAddr,
		Filter:        NewChannelFilter(chID, fp),
	}, nil
}

// CreateHandshake generates a HandshakePacket stamped with the channel's fingerprint and channel ID.
func (c *ClassroomChannel) CreateHandshake(deviceID, role string, payload []byte) (*HandshakePacket, error) {
	return &HandshakePacket{
		Magic:                HandshakeMagic,
		Version:              CurrentHandshakeVersion,
		ChannelID:            c.ChannelID,
		ClassroomFingerprint: c.Fingerprint,
		ClassroomID:          c.ClassroomID,
		DeviceID:             strings.TrimSpace(deviceID),
		Role:                 strings.TrimSpace(role),
		Timestamp:            time.Now().UnixNano(),
		Payload:              payload,
	}, nil
}

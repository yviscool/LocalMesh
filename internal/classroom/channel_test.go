package classroom

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestChannelIDValidationAndSanitization(t *testing.T) {
	t.Parallel()

	// Valid channels 1 to 255
	for _, id := range []int{1, 42, 100, 255} {
		if err := ValidateChannelID(id); err != nil {
			t.Errorf("channel %d should be valid, got: %v", id, err)
		}
		if sanitized := SanitizeChannelID(id); sanitized != id {
			t.Errorf("sanitized %d should equal original %d, got %d", id, id, sanitized)
		}
	}

	// Invalid channels
	for _, id := range []int{0, -1, -99, 256, 1000} {
		if err := ValidateChannelID(id); !errors.Is(err, ErrInvalidChannelID) {
			t.Errorf("expected ErrInvalidChannelID for %d, got %v", id, err)
		}
		if sanitized := SanitizeChannelID(id); sanitized != DefaultChannelID {
			t.Errorf("expected default channel %d for invalid %d, got %d", DefaultChannelID, id, sanitized)
		}
	}
}

func TestDeriveMulticastAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		channelID int
		baseAddr  string
		expected  string
	}{
		{
			name:      "Default base and channel 1",
			channelID: 1,
			baseAddr:  "",
			expected:  "239.255.42.1:8899",
		},
		{
			name:      "Channel 42 with explicit base",
			channelID: 42,
			baseAddr:  "239.255.42.0:8899",
			expected:  "239.255.42.42:8899",
		},
		{
			name:      "Channel 255 with custom subnet and port",
			channelID: 255,
			baseAddr:  "239.255.10.99:9900",
			expected:  "239.255.10.255:9900",
		},
		{
			name:      "Invalid channel 0 clamped to 1",
			channelID: 0,
			baseAddr:  "239.255.42.0:8899",
			expected:  "239.255.42.1:8899",
		},
		{
			name:      "Invalid channel 300 clamped to 1",
			channelID: 300,
			baseAddr:  "239.255.42.0:8899",
			expected:  "239.255.42.1:8899",
		},
		{
			name:      "Base address without port defaults to 8899",
			channelID: 5,
			baseAddr:  "239.255.42.0",
			expected:  "239.255.42.5:8899",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := DeriveMulticastAddr(tc.channelID, tc.baseAddr)
			if actual != tc.expected {
				t.Errorf("DeriveMulticastAddr(%d, %q) = %q; want %q", tc.channelID, tc.baseAddr, actual, tc.expected)
			}
		})
	}
}

func TestClassroomFingerprint(t *testing.T) {
	t.Parallel()

	fp1 := DeriveClassroomFingerprint("lab-101", 1)
	fp2 := DeriveClassroomFingerprint("lab-101", 1)
	fp3 := DeriveClassroomFingerprint("lab-101", 2)
	fp4 := DeriveClassroomFingerprint("lab-102", 1)

	// Deterministic
	if fp1 != fp2 {
		t.Errorf("fingerprint must be deterministic, got %s and %s", fp1, fp2)
	}

	// Distinct across channels
	if fp1 == fp3 {
		t.Errorf("different channels in same room must have different fingerprints: %s vs %s", fp1, fp3)
	}

	// Distinct across rooms
	if fp1 == fp4 {
		t.Errorf("different rooms on same channel must have different fingerprints: %s vs %s", fp1, fp4)
	}

	if !VerifyClassroomFingerprint("lab-101", 1, fp1) {
		t.Errorf("VerifyClassroomFingerprint failed for valid fingerprint %s", fp1)
	}
	if VerifyClassroomFingerprint("lab-101", 1, "CFP-bogus") {
		t.Error("VerifyClassroomFingerprint should fail for bogus fingerprint")
	}
}

func TestHandshakePacketSerialization(t *testing.T) {
	t.Parallel()

	pkt := HandshakePacket{
		ChannelID:            5,
		ClassroomFingerprint: "CFP-1234567890abcdef",
		ClassroomID:          "lab-science-3",
		DeviceID:             "teacher-main-host",
		Role:                 "teacher",
		Timestamp:            time.Now().UnixNano(),
		Payload:              []byte("ready_for_broadcast"),
	}

	data, err := pkt.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded HandshakePacket
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ChannelID != pkt.ChannelID {
		t.Errorf("channel ID mismatch: got %d, want %d", decoded.ChannelID, pkt.ChannelID)
	}
	if decoded.ClassroomFingerprint != pkt.ClassroomFingerprint {
		t.Errorf("fingerprint mismatch: got %s, want %s", decoded.ClassroomFingerprint, pkt.ClassroomFingerprint)
	}
	if decoded.ClassroomID != pkt.ClassroomID {
		t.Errorf("classroom ID mismatch: got %s, want %s", decoded.ClassroomID, pkt.ClassroomID)
	}
	if decoded.DeviceID != pkt.DeviceID {
		t.Errorf("device ID mismatch: got %s, want %s", decoded.DeviceID, pkt.DeviceID)
	}
	if decoded.Role != pkt.Role {
		t.Errorf("role mismatch: got %s, want %s", decoded.Role, pkt.Role)
	}
	if !bytes.Equal(decoded.Payload, pkt.Payload) {
		t.Errorf("payload mismatch")
	}
}

func TestMultiRoomChannelSilentDropAndIsolation(t *testing.T) {
	t.Parallel()

	// Room A: Lab 101, Channel 1
	roomA, err := NewClassroomChannel("lab-101", 1, "")
	if err != nil {
		t.Fatalf("NewClassroomChannel for Room A failed: %v", err)
	}

	// Room B: Lab 102, Channel 2
	roomB, err := NewClassroomChannel("lab-102", 2, "")
	if err != nil {
		t.Fatalf("NewClassroomChannel for Room B failed: %v", err)
	}

	// Room C: Lab 201, Channel 1 (Same channel ID as Room A, but different room/fingerprint!)
	roomC, err := NewClassroomChannel("lab-201", 1, "")
	if err != nil {
		t.Fatalf("NewClassroomChannel for Room C failed: %v", err)
	}

	// Teacher A creates handshake
	pktA, err := roomA.CreateHandshake("teacher-a", "teacher", []byte("room_a_start"))
	if err != nil {
		t.Fatalf("CreateHandshake for Room A failed: %v", err)
	}
	dataA, _ := pktA.Marshal()

	// Teacher B creates handshake
	pktB, err := roomB.CreateHandshake("teacher-b", "teacher", []byte("room_b_start"))
	if err != nil {
		t.Fatalf("CreateHandshake for Room B failed: %v", err)
	}
	dataB, _ := pktB.Marshal()

	// Teacher C creates handshake
	pktC, err := roomC.CreateHandshake("teacher-c", "teacher", []byte("room_c_start"))
	if err != nil {
		t.Fatalf("CreateHandshake for Room C failed: %v", err)
	}
	dataC, _ := pktC.Marshal()

	filterA := roomA.Filter

	// 1. Room A accepts its own packet
	recvA, ok := filterA.FilterRaw(dataA)
	if !ok || recvA == nil {
		t.Fatal("filterA should accept Room A packet")
	}
	if recvA.DeviceID != "teacher-a" {
		t.Errorf("expected teacher-a, got %s", recvA.DeviceID)
	}

	// 2. Room A silently drops Room B packet (different channel ID and different fingerprint)
	recvB, okB := filterA.FilterRaw(dataB)
	if okB || recvB != nil {
		t.Fatal("filterA should SILENTLY DROP packet from Room B (different channel)")
	}

	// 3. Room A silently drops Room C packet (same channel 1, but DIFFERENT classroom fingerprint)
	recvC, okC := filterA.FilterRaw(dataC)
	if okC || recvC != nil {
		t.Fatal("filterA should SILENTLY DROP packet from Room C (different classroom fingerprint)")
	}

	// 4. Garbage / Corrupted packets are silently dropped without panic
	corrupted := append([]byte("INVALID_BYTES"), dataA...)
	recvCorrupt, okCorrupt := filterA.FilterRaw(corrupted)
	if okCorrupt || recvCorrupt != nil {
		t.Fatal("filterA should SILENTLY DROP corrupted packet")
	}

	// 5. Short payload (<5 bytes) is silently dropped
	recvShort, okShort := filterA.FilterRaw([]byte{1, 2, 3})
	if okShort || recvShort != nil {
		t.Fatal("filterA should SILENTLY DROP short packet")
	}
}

package discovery

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	domain "localmesh/internal/domain/discovery"
)

type observer struct{ values chan domain.Observation }

func (o *observer) Observe(v domain.Observation) { o.values <- v }

func TestServerAcceptsVersionedAnnouncementAndSuppressesDuplicates(t *testing.T) {
	o := &observer{values: make(chan domain.Observation, 2)}
	s, err := Listen(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}, o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.MinInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	client, err := net.DialUDP("udp", nil, s.Conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	payload, _ := json.Marshal(Announcement{Version: ProtocolVersion, DeviceID: "CPC-0001-AGENT", TTLSeconds: 10})
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-o.values:
		if got.DeviceID != "CPC-0001-AGENT" || !got.Reachable {
			t.Fatalf("observation = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for announcement")
	}
	select {
	case <-o.values:
		t.Fatal("duplicate announcement was not suppressed")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	_ = s.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func TestServerRejectsInvalidVersionAndDeviceID(t *testing.T) {
	o := &observer{values: make(chan domain.Observation, 1)}
	s, err := Listen(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}, o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Serve(ctx) }()
	client, _ := net.DialUDP("udp", nil, s.Conn.LocalAddr().(*net.UDPAddr))
	defer client.Close()
	for _, a := range []Announcement{{Version: 9, DeviceID: "CPC-0001-AGENT"}, {Version: ProtocolVersion, DeviceID: "bad!"}} {
		payload, _ := json.Marshal(a)
		_, _ = client.Write(payload)
	}
	select {
	case got := <-o.values:
		t.Fatalf("unexpected observation: %#v", got)
	case <-time.After(150 * time.Millisecond):
	}
}

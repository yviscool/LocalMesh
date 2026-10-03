package pairing

import (
	"testing"
	"time"

	"localmesh/internal/domain/identity"
)

func TestPairingApprovalAndRevocation(t *testing.T) {
	deviceID, err := identity.NewDeviceID("CPC-8F23-19A7")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	r, err := NewRequest("req-1", deviceID, "CLASS-2026-001", "739284", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Approve(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := r.Revoke(now.Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusRevoked {
		t.Fatalf("status = %s, want %s", r.Status, StatusRevoked)
	}
	if err := r.Approve(now.Add(3 * time.Second)); err != ErrInvalidStatus {
		t.Fatalf("Approve() error = %v, want %v", err, ErrInvalidStatus)
	}
}

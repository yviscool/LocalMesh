package sessionrecovery

import (
	"context"
	"testing"
	"time"

	"localmesh/internal/application"
	"localmesh/internal/domain/identity"
	"localmesh/internal/domain/session"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeRepository struct {
	rows    []application.SessionRecord
	members map[string]application.MemberRecord
	closed  map[string]string
}

func (f *fakeRepository) ListSessions(context.Context, string) ([]application.SessionRecord, error) {
	return f.rows, nil
}
func (f *fakeRepository) FindMember(_ context.Context, classroomID, memberID string) (application.MemberRecord, error) {
	return f.members[classroomID+":"+memberID], nil
}
func (f *fakeRepository) CloseSession(_ context.Context, id, reason string, _ time.Time) error {
	f.closed[id] = reason
	return nil
}

func TestRecoverClosesExpiredRevokedAndDuplicateSessions(t *testing.T) {
	now := time.Unix(1000, 0)
	device, _ := identity.NewDeviceID("CPC-0001-AGENT")
	repo := &fakeRepository{members: map[string]application.MemberRecord{}, closed: map[string]string{}}
	repo.rows = []application.SessionRecord{
		{ID: "expired", DeviceID: device, ClassroomID: "class-1", State: session.StateActive, ExpiresAt: now.Add(-time.Second)},
		{ID: "new", DeviceID: device, ClassroomID: "class-1", State: session.StateActive, ExpiresAt: now.Add(time.Hour)},
		{ID: "old", DeviceID: device, ClassroomID: "class-1", State: session.StateActive, ExpiresAt: now.Add(time.Hour)},
		{ID: "closed", DeviceID: device, ClassroomID: "class-1", State: session.StateClosed, ExpiresAt: now.Add(time.Hour)},
	}
	repo.members["class-1:"+string(device)] = application.MemberRecord{ClassroomID: "class-1", MemberID: string(device)}
	count, err := (&Service{Clock: fixedClock{now}, Sessions: repo}).Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("closed count = %d, want 2", count)
	}
	if repo.closed["expired"] == "" || repo.closed["old"] == "" {
		t.Fatalf("closed sessions = %#v", repo.closed)
	}
}

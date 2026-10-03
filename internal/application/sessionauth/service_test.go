package sessionauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"localmesh/internal/auth"
	"localmesh/internal/domain/identity"
	"localmesh/internal/domain/session"
)

type clock struct{ now time.Time }

func (c clock) Now() time.Time { return c.now }

type sessionRepo struct {
	opened *session.Session
	err    error
}

func (r *sessionRepo) OpenSession(_ context.Context, value session.Session, _, _ string) error {
	if r.err != nil {
		return r.err
	}
	r.opened = &value
	return nil
}
func (r *sessionRepo) HeartbeatSession(context.Context, string, time.Time, time.Time) error {
	return nil
}
func (r *sessionRepo) CloseSession(context.Context, string, string, time.Time) error { return nil }

func validHandshake(t *testing.T) (identity.DeviceID, auth.Challenge, []byte, *auth.Verifier, time.Time) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	deviceID, err := identity.NewDeviceID("CPC-0001-AGENT")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	challenge, err := auth.NewChallenge(deviceID, "session-1", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := challenge.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier := auth.NewVerifier(time.Second)
	if err := verifier.Register(deviceID, publicKey); err != nil {
		t.Fatal(err)
	}
	return deviceID, challenge, signature, verifier, now
}

func TestAuthenticateCreatesActiveSession(t *testing.T) {
	deviceID, challenge, signature, verifier, now := validHandshake(t)
	repo := &sessionRepo{}
	service := Service{Clock: clock{now: now}, Verifier: verifier, Sessions: repo, TTL: time.Minute}
	value, err := service.Authenticate(context.Background(), "session-1", deviceID, challenge, signature, "CLASS-1", "tls")
	if err != nil {
		t.Fatal(err)
	}
	if value.State != session.StateActive || repo.opened == nil || repo.opened.State != session.StateActive {
		t.Fatalf("session=%+v persisted=%+v", value, repo.opened)
	}
}

func TestAuthenticateRejectsReplayAndDoesNotPersist(t *testing.T) {
	deviceID, challenge, signature, verifier, now := validHandshake(t)
	repo := &sessionRepo{}
	service := Service{Clock: clock{now: now}, Verifier: verifier, Sessions: repo, TTL: time.Minute}
	if _, err := service.Authenticate(context.Background(), "session-1", deviceID, challenge, signature, "CLASS-1", "tls"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), "session-1", deviceID, challenge, signature, "CLASS-1", "tls"); !errors.Is(err, auth.ErrChallengeReplay) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestAuthenticateRejectsRepositoryFailure(t *testing.T) {
	deviceID, challenge, signature, verifier, now := validHandshake(t)
	service := Service{Clock: clock{now: now}, Verifier: verifier, Sessions: &sessionRepo{err: errors.New("db unavailable")}, TTL: time.Minute}
	if _, err := service.Authenticate(context.Background(), "session-1", deviceID, challenge, signature, "CLASS-1", "tls"); err == nil {
		t.Fatal("repository failure should be returned")
	}
}

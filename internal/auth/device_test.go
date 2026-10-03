package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"localmesh/internal/domain/identity"
)

func TestDeviceChallengeResponse(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	deviceID, err := identity.NewDeviceID("CPC-0001-AGENT")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	challenge, err := NewChallenge(deviceID, "session-1", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := challenge.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewVerifier(time.Second)
	if err := verifier.Register(deviceID, publicKey); err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify(challenge, signature, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify(challenge, signature, now.Add(2*time.Second)); !errors.Is(err, ErrChallengeReplay) {
		t.Fatalf("second verify = %v, want replay", err)
	}
}

func TestDeviceChallengeRejectsWrongKeyAndExpiry(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	wrongPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	deviceID, _ := identity.NewDeviceID("CPC-0001-AGENT")
	now := time.Unix(100, 0)
	challenge, _ := NewChallenge(deviceID, "session-1", now, time.Second)
	signature, _ := challenge.Sign(privateKey)
	verifier := NewVerifier(0)
	if err := verifier.Register(deviceID, wrongPublic); err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify(challenge, signature, now); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("wrong key = %v", err)
	}
	verifier = NewVerifier(0)
	_ = verifier.Register(deviceID, publicKey)
	if err := verifier.Verify(challenge, signature, now.Add(2*time.Second)); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("expired challenge = %v", err)
	}
}

func TestFingerprintIsStable(t *testing.T) {
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	if Fingerprint(publicKey) != Fingerprint(append(ed25519.PublicKey(nil), publicKey...)) {
		t.Fatal("fingerprint changed for same public key")
	}
}

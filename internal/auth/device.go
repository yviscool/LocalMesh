package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"localmesh/internal/domain/identity"
)

const NonceSize = 32

var (
	ErrInvalidKey       = errors.New("invalid device public key")
	ErrUnknownDevice    = errors.New("unknown device")
	ErrInvalidChallenge = errors.New("invalid authentication challenge")
	ErrChallengeExpired = errors.New("authentication challenge expired")
	ErrChallengeReplay  = errors.New("authentication challenge already used")
	ErrInvalidSignature = errors.New("invalid device signature")
)

type Challenge struct {
	DeviceID  identity.DeviceID
	SessionID string
	Nonce     []byte
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type challengeWire struct {
	DeviceID  string `json:"device_id"`
	SessionID string `json:"session_id"`
	Nonce     []byte `json:"nonce"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
}

func (c Challenge) signingBytes() []byte {
	value, _ := json.Marshal(challengeWire{DeviceID: string(c.DeviceID), SessionID: c.SessionID, Nonce: c.Nonce, IssuedAt: c.IssuedAt.UnixNano(), ExpiresAt: c.ExpiresAt.UnixNano()})
	return value
}

func NewChallenge(deviceID identity.DeviceID, sessionID string, now time.Time, ttl time.Duration) (Challenge, error) {
	if deviceID == "" || sessionID == "" || ttl <= 0 {
		return Challenge{}, ErrInvalidChallenge
	}
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return Challenge{}, fmt.Errorf("generate challenge nonce: %w", err)
	}
	return Challenge{DeviceID: deviceID, SessionID: sessionID, Nonce: nonce, IssuedAt: now, ExpiresAt: now.Add(ttl)}, nil
}

func (c Challenge) Sign(privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize || len(c.Nonce) != NonceSize {
		return nil, ErrInvalidChallenge
	}
	return ed25519.Sign(privateKey, c.signingBytes()), nil
}

type Verifier struct {
	mu      sync.Mutex
	keys    map[identity.DeviceID]ed25519.PublicKey
	used    map[string]time.Time
	maxSkew time.Duration
}

func NewVerifier(maxSkew time.Duration) *Verifier {
	if maxSkew < 0 {
		maxSkew = 0
	}
	return &Verifier{keys: make(map[identity.DeviceID]ed25519.PublicKey), used: make(map[string]time.Time), maxSkew: maxSkew}
}

func (v *Verifier) Register(deviceID identity.DeviceID, publicKey ed25519.PublicKey) error {
	if deviceID == "" || len(publicKey) != ed25519.PublicKeySize {
		return ErrInvalidKey
	}
	v.mu.Lock()
	v.keys[deviceID] = append(ed25519.PublicKey(nil), publicKey...)
	v.mu.Unlock()
	return nil
}

func (v *Verifier) Revoke(deviceID identity.DeviceID) {
	v.mu.Lock()
	delete(v.keys, deviceID)
	v.mu.Unlock()
}

func (v *Verifier) Verify(challenge Challenge, signature []byte, now time.Time) error {
	if challenge.DeviceID == "" || challenge.SessionID == "" || len(challenge.Nonce) != NonceSize || challenge.ExpiresAt.Before(challenge.IssuedAt) {
		return ErrInvalidChallenge
	}
	if now.After(challenge.ExpiresAt.Add(v.maxSkew)) || now.Before(challenge.IssuedAt.Add(-v.maxSkew)) {
		return ErrChallengeExpired
	}
	nonceDigest := sha256.Sum256(challenge.Nonce)
	nonceID := hex.EncodeToString(nonceDigest[:])
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, exists := v.used[nonceID]; exists {
		return ErrChallengeReplay
	}
	publicKey, exists := v.keys[challenge.DeviceID]
	if !exists {
		return ErrUnknownDevice
	}
	if !ed25519.Verify(publicKey, challenge.signingBytes(), signature) {
		return ErrInvalidSignature
	}
	v.used[nonceID] = now
	return nil
}

func Fingerprint(publicKey ed25519.PublicKey) string {
	digest := sha256.Sum256(publicKey)
	return hex.EncodeToString(digest[:])
}

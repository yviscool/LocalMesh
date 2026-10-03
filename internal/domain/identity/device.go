package identity

import (
	"errors"
	"strings"
)

var ErrInvalidDeviceID = errors.New("invalid device id")

// DeviceID is stable across DHCP changes, network-interface changes, and reconnects.
type DeviceID string

func NewDeviceID(value string) (DeviceID, error) {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > 64 {
		return "", ErrInvalidDeviceID
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return "", ErrInvalidDeviceID
		}
	}
	return DeviceID(value), nil
}

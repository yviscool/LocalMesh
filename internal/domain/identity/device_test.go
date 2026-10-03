package identity

import "testing"

func TestNewDeviceID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "stable id", input: "CPC-8F23-19A7"},
		{name: "trim whitespace", input: "  device-001  "},
		{name: "too short", input: "abc", wantErr: true},
		{name: "invalid character", input: "device/001", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDeviceID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewDeviceID() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

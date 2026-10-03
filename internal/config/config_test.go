package config

import "testing"

func TestDefaultConfigIsSafeAndValid(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Node.ListenAddress != "127.0.0.1:7443" {
		t.Fatalf("default listen address = %q, want loopback", c.Node.ListenAddress)
	}
}

func TestConfigRejectsUnsafeSessionTiming(t *testing.T) {
	c := Default()
	c.Session.TTL = c.Session.HeartbeatInterval
	if err := c.Validate(); err != ErrInvalidConfig {
		t.Fatalf("Validate() error = %v, want %v", err, ErrInvalidConfig)
	}
}

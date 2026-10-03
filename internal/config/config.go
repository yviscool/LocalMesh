package config

import (
	"errors"
	"strings"
	"time"
)

var ErrInvalidConfig = errors.New("invalid configuration")

type Config struct {
	Node      NodeConfig
	Session   SessionConfig
	Discovery DiscoveryConfig
	Command   CommandConfig
	Data      DataConfig
}

type NodeConfig struct {
	Role          string
	ListenAddress string
	DataDirectory string
}

type SessionConfig struct {
	TTL               time.Duration
	HeartbeatInterval time.Duration
	ReconnectInitial  time.Duration
	ReconnectMaximum  time.Duration
}

type DiscoveryConfig struct {
	Enabled        bool
	ObservationTTL time.Duration
	MaxPacketBytes int
}

type CommandConfig struct {
	DefaultTimeout  time.Duration
	MaxPayloadBytes int
	MaxAttempts     int
}

type DataConfig struct {
	DatabasePath   string
	AuditRetention time.Duration
}

func Default() Config {
	return Config{
		Node:      NodeConfig{Role: "teacher", ListenAddress: "127.0.0.1:7443", DataDirectory: "data"},
		Session:   SessionConfig{TTL: 2 * time.Minute, HeartbeatInterval: 15 * time.Second, ReconnectInitial: time.Second, ReconnectMaximum: 30 * time.Second},
		Discovery: DiscoveryConfig{Enabled: true, ObservationTTL: 30 * time.Second, MaxPacketBytes: 64 * 1024},
		Command:   CommandConfig{DefaultTimeout: 10 * time.Second, MaxPayloadBytes: 256 * 1024, MaxAttempts: 3},
		Data:      DataConfig{DatabasePath: "data/localmesh.db", AuditRetention: 180 * 24 * time.Hour},
	}
}

func (c Config) Validate() error {
	if c.Node.Role != "teacher" && c.Node.Role != "student" && c.Node.Role != "server" {
		return ErrInvalidConfig
	}
	if strings.TrimSpace(c.Node.ListenAddress) == "" || strings.TrimSpace(c.Node.DataDirectory) == "" || strings.TrimSpace(c.Data.DatabasePath) == "" {
		return ErrInvalidConfig
	}
	if c.Session.TTL <= c.Session.HeartbeatInterval || c.Session.HeartbeatInterval <= 0 || c.Session.ReconnectInitial <= 0 || c.Session.ReconnectMaximum < c.Session.ReconnectInitial {
		return ErrInvalidConfig
	}
	if c.Discovery.ObservationTTL <= 0 || c.Discovery.MaxPacketBytes <= 0 || c.Command.DefaultTimeout <= 0 || c.Command.MaxPayloadBytes <= 0 || c.Command.MaxAttempts < 1 || c.Data.AuditRetention <= 0 {
		return ErrInvalidConfig
	}
	return nil
}

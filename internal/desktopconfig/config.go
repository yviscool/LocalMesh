// Package desktopconfig loads and persists the teacher console runtime
// configuration (data/config.toml).
package desktopconfig

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config is the teacher console runtime configuration root.
type Config struct {
	Broadcast  BroadcastConfig  `toml:"broadcast" json:"broadcast"`
	Thumbnails ThumbnailsConfig `toml:"thumbnails" json:"thumbnails"`
	Offline    OfflineConfig    `toml:"offline" json:"offline"`
	Log        LogConfig        `toml:"log" json:"log"`
}

// BroadcastConfig controls the screen multicast pipeline.
type BroadcastConfig struct {
	FPS             int `toml:"fps" json:"fps"`
	Quality         int `toml:"quality" json:"quality"`
	BudgetMBPerSec  int `toml:"budget_mb_per_sec" json:"budget_mb_per_sec"`
}

// ThumbnailsConfig controls the student preview push cadence.
type ThumbnailsConfig struct {
	IntervalMS int `toml:"interval_ms" json:"interval_ms"`
	Quality    int `toml:"quality" json:"quality"`
}

// OfflineConfig holds the default offline-guard policy.
type OfflineConfig struct {
	Enabled      bool `toml:"enabled" json:"enabled"`
	LeaseSeconds int  `toml:"lease_seconds" json:"lease_seconds"`
}

// LogConfig holds logging preferences.
type LogConfig struct {
	Level string `toml:"level" json:"level"`
	File  string `toml:"file" json:"file"`
}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		Broadcast:  BroadcastConfig{FPS: 15, Quality: 70, BudgetMBPerSec: 4},
		Thumbnails: ThumbnailsConfig{IntervalMS: 2000, Quality: 70},
		Offline:    OfflineConfig{Enabled: true, LeaseSeconds: 10},
		Log:        LogConfig{Level: "info"},
	}
}

// Load reads path, falling back to defaults for missing sections. A missing
// file is not an error.
func Load(path string) (Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.normalize()
	return cfg, nil
}

// Save writes the config back as TOML.
func (c Config) Save(path string) error {
	c.normalize()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

// Path returns the config file location.
func Path() string { return "data/config.toml" }

func (c *Config) normalize() {
	if c.Broadcast.FPS <= 0 || c.Broadcast.FPS > 30 {
		c.Broadcast.FPS = 15
	}
	if c.Broadcast.Quality <= 0 || c.Broadcast.Quality > 95 {
		c.Broadcast.Quality = 70
	}
	if c.Broadcast.BudgetMBPerSec < 0 {
		c.Broadcast.BudgetMBPerSec = 4
	}
	if c.Thumbnails.IntervalMS < 500 {
		c.Thumbnails.IntervalMS = 2000
	}
	if c.Thumbnails.Quality <= 0 || c.Thumbnails.Quality > 95 {
		c.Thumbnails.Quality = 70
	}
	if c.Offline.LeaseSeconds <= 0 {
		c.Offline.LeaseSeconds = 10
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
}

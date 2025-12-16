package config

import (
	"fmt"
	"os"
	"runtime"

	"gopkg.in/yaml.v3"
)

// DefaultConfigFileName is the name of the config file to look for at runtime
const DefaultConfigFileName = "cbz-compress.yaml"

// embeddedDefaults holds the config parsed from the embedded YAML at build time
var embeddedDefaults *Config

// Config holds all settings for compression
type Config struct {
	// Configurable via YAML file
	MaxDimension    int     `yaml:"max_dimension"`         // Maximum dimension in pixels
	JPEGQuality     int     `yaml:"jpeg_quality"`          // JPEG quality 1-100
	BackupDir       string  `yaml:"backup_dir"`            // Where to move originals
	ThresholdMBPage float64 `yaml:"threshold_mb_per_page"` // MB per page threshold for skip heuristic

	// Runtime flags (not in YAML)
	Recursive bool // Process directories recursively
	Force     bool // Process even if file appears optimized
	DryRun    bool // Preview mode without changes
	Verbose   bool // Detailed output
	Workers   int  // Concurrent processing
}

// InitEmbedded initializes the embedded defaults from build-time YAML data.
// This must be called before any other config functions.
func InitEmbedded(data []byte) error {
	cfg := &Config{
		// Hardcoded fallbacks (should never be needed if embedded YAML is valid)
		MaxDimension:    1800,
		JPEGQuality:     90,
		BackupDir:       "originals_backup",
		ThresholdMBPage: 1.5,
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to parse embedded config: %w", err)
	}

	embeddedDefaults = cfg
	return nil
}

// DefaultConfig returns the default configuration.
// Uses embedded build-time values if InitEmbedded was called, otherwise hardcoded fallbacks.
func DefaultConfig() Config {
	cfg := Config{
		Recursive: true,
		Force:     false,
		DryRun:    false,
		Verbose:   false,
		Workers:   runtime.NumCPU(),
	}

	if embeddedDefaults != nil {
		cfg.MaxDimension = embeddedDefaults.MaxDimension
		cfg.JPEGQuality = embeddedDefaults.JPEGQuality
		cfg.BackupDir = embeddedDefaults.BackupDir
		cfg.ThresholdMBPage = embeddedDefaults.ThresholdMBPage
	} else {
		// Hardcoded fallbacks
		cfg.MaxDimension = 1800
		cfg.JPEGQuality = 90
		cfg.BackupDir = "originals_backup"
		cfg.ThresholdMBPage = 1.5
	}

	return cfg
}

// LoadFromFile loads configuration from a YAML file.
// Only the YAML-tagged fields are overwritten; runtime flags retain defaults.
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// LoadWithDefaults attempts to load config from DefaultConfigFileName in the current directory.
// If the file doesn't exist, returns DefaultConfig. Returns an error only if the file exists but is invalid.
func LoadWithDefaults() (*Config, error) {
	cfg, err := LoadFromFile(DefaultConfigFileName)
	if err != nil {
		if os.IsNotExist(err) {
			defaultCfg := DefaultConfig()
			return &defaultCfg, nil
		}
		return nil, err
	}
	return cfg, nil
}

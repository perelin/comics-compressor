package config

import (
	"math"
	"testing"
)

func validConfig() Config {
	return Config{
		MaxDimension:    1800,
		JPEGQuality:     90,
		BackupDir:       "backup",
		ThresholdMBPage: 1.5,
		MinSavingsPct:   5,
		Workers:         4,
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid", func(c *Config) {}, false},
		{"min savings zero", func(c *Config) { c.MinSavingsPct = 0 }, false},
		{"min savings hundred", func(c *Config) { c.MinSavingsPct = 100 }, false},
		{"min savings negative", func(c *Config) { c.MinSavingsPct = -10 }, true},
		{"min savings over 100", func(c *Config) { c.MinSavingsPct = 150 }, true},
		{"min savings NaN", func(c *Config) { c.MinSavingsPct = math.NaN() }, true},
		{"min savings Inf", func(c *Config) { c.MinSavingsPct = math.Inf(1) }, true},
		{"threshold NaN", func(c *Config) { c.ThresholdMBPage = math.NaN() }, true},
		{"threshold negative", func(c *Config) { c.ThresholdMBPage = -1 }, true},
		{"quality zero", func(c *Config) { c.JPEGQuality = 0 }, true},
		{"quality over 100", func(c *Config) { c.JPEGQuality = 101 }, true},
		{"max dimension zero", func(c *Config) { c.MaxDimension = 0 }, true},
		{"workers zero", func(c *Config) { c.Workers = 0 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

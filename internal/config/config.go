package config

import "runtime"

// Config holds all settings for compression
type Config struct {
	MaxDimension    int     // Maximum dimension in pixels (default: 1800)
	JPEGQuality     int     // JPEG quality 1-100 (default: 90)
	BackupDir       string  // Where to move originals (default: "originals_backup")
	ThresholdMBPage float64 // MB per page threshold for skip heuristic (default: 1.5)
	Recursive       bool    // Process directories recursively (default: true)
	Force           bool    // Process even if file appears optimized (default: false)
	DryRun          bool    // Preview mode without changes (default: false)
	Verbose         bool    // Detailed output (default: false)
	Workers         int     // Concurrent processing (default: runtime.NumCPU())
}

// DefaultConfig returns sensible defaults for quality-priority compression
func DefaultConfig() Config {
	return Config{
		MaxDimension:    1800,
		JPEGQuality:     90,
		BackupDir:       "originals_backup",
		ThresholdMBPage: 1.5,
		Recursive:       true,
		Force:           false,
		DryRun:          false,
		Verbose:         false,
		Workers:         runtime.NumCPU(),
	}
}

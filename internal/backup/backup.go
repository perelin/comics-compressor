package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Manager handles backup operations for original files
type Manager struct {
	backupDir string
	mu        sync.Mutex
}

// NewManager creates a backup manager with the specified directory
func NewManager(backupDir string) *Manager {
	return &Manager{backupDir: backupDir}
}

// MoveToBackup moves the original file to the backup directory
// Preserves the filename but flattens the path structure
// Thread-safe: uses mutex to prevent TOCTOU race when finding unique paths
func (m *Manager) MoveToBackup(originalPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure backup directory exists
	if err := os.MkdirAll(m.backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup dir: %w", err)
	}

	// Create backup path preserving filename
	backupPath := filepath.Join(m.backupDir, filepath.Base(originalPath))

	// Handle duplicates by adding suffix (safe under lock)
	if _, err := os.Stat(backupPath); err == nil {
		backupPath = m.uniquePathLocked(backupPath)
	}

	// Move file
	if err := os.Rename(originalPath, backupPath); err != nil {
		return fmt.Errorf("failed to move %s to backup: %w", originalPath, err)
	}

	return nil
}

// GetBackupPath returns the path where a file would be backed up
func (m *Manager) GetBackupPath(originalPath string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	backupPath := filepath.Join(m.backupDir, filepath.Base(originalPath))
	if _, err := os.Stat(backupPath); err == nil {
		return m.uniquePathLocked(backupPath)
	}
	return backupPath
}

// RestoreFromBackup restores a file from backup (for error recovery)
func (m *Manager) RestoreFromBackup(originalPath string) error {
	backupPath := filepath.Join(m.backupDir, filepath.Base(originalPath))

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", backupPath)
	}

	return os.Rename(backupPath, originalPath)
}

// uniquePathLocked generates a unique path by adding numeric suffix
// Must be called with m.mu held
func (m *Manager) uniquePathLocked(path string) string {
	ext := filepath.Ext(path)
	base := path[:len(path)-len(ext)]

	for i := 1; ; i++ {
		newPath := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
	}
}

// BackupDir returns the configured backup directory
func (m *Manager) BackupDir() string {
	return m.backupDir
}

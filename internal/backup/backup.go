package backup

import (
	"fmt"
	"os"
	"path/filepath"
)

// Manager handles backup operations for original files
type Manager struct {
	backupDir string
}

// NewManager creates a backup manager with the specified directory
func NewManager(backupDir string) *Manager {
	return &Manager{backupDir: backupDir}
}

// MoveToBackup moves the original file to the backup directory
// Preserves the filename but flattens the path structure
func (m *Manager) MoveToBackup(originalPath string) error {
	// Ensure backup directory exists
	if err := os.MkdirAll(m.backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup dir: %w", err)
	}

	// Create backup path preserving filename
	backupPath := filepath.Join(m.backupDir, filepath.Base(originalPath))

	// Handle duplicates by adding suffix
	if _, err := os.Stat(backupPath); err == nil {
		backupPath = m.uniquePath(backupPath)
	}

	// Move file
	if err := os.Rename(originalPath, backupPath); err != nil {
		return fmt.Errorf("failed to move %s to backup: %w", originalPath, err)
	}

	return nil
}

// GetBackupPath returns the path where a file would be backed up
func (m *Manager) GetBackupPath(originalPath string) string {
	backupPath := filepath.Join(m.backupDir, filepath.Base(originalPath))
	if _, err := os.Stat(backupPath); err == nil {
		return m.uniquePath(backupPath)
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

// uniquePath generates a unique path by adding numeric suffix
func (m *Manager) uniquePath(path string) string {
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

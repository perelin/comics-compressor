package cbz

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
)

// WriteEntry represents a file to write into the CBZ
type WriteEntry struct {
	Path string
	Data []byte
}

// Writer handles CBZ creation with atomic writes
type Writer struct{}

// NewWriter creates a new CBZ writer
func NewWriter() *Writer {
	return &Writer{}
}

// Create builds a new CBZ file from entries using atomic write pattern
// Writes to temp file first, then renames to final path
func (w *Writer) Create(outputPath string, entries []WriteEntry) error {
	// Create parent directory if needed
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Create temporary file in same directory for atomic rename
	tempPath := outputPath + ".tmp"

	f, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	zipWriter := zip.NewWriter(f)

	for _, entry := range entries {
		header := &zip.FileHeader{
			Name:   entry.Path,
			Method: zip.Deflate,
		}
		header.SetMode(0644)

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			f.Close()
			os.Remove(tempPath)
			return fmt.Errorf("failed to create entry %s: %w", entry.Path, err)
		}

		if _, err := writer.Write(entry.Data); err != nil {
			f.Close()
			os.Remove(tempPath)
			return fmt.Errorf("failed to write entry %s: %w", entry.Path, err)
		}
	}

	if err := zipWriter.Close(); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to close zip writer: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to close file: %w", err)
	}

	// Atomically rename temp to final
	if err := os.Rename(tempPath, outputPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// CreateTemp creates a CBZ at a temporary path (for verification before replacing original)
func (w *Writer) CreateTemp(basePath string, entries []WriteEntry) (string, error) {
	tempPath := basePath + ".compressed.tmp.cbz"
	if err := w.Create(tempPath, entries); err != nil {
		return "", err
	}
	return tempPath, nil
}

package cbz

import (
	"archive/zip"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ImageEntry represents an image file within a CBZ
type ImageEntry struct {
	Path         string    // Full path within archive (e.g., "chapter1/page01.jpg")
	OriginalSize int64     // Original file size in bytes
	Data         []byte    // Raw image data
	ModTime      time.Time // Preserve modification time
}

// OtherEntry represents non-image files to preserve (e.g., ComicInfo.xml)
type OtherEntry struct {
	Path    string
	Data    []byte
	ModTime time.Time
}

// Contents holds all extracted content from a CBZ file
type Contents struct {
	SourcePath string
	Images     []ImageEntry
	OtherFiles []OtherEntry
}

// SupportedImageExtensions for filtering
var SupportedImageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
}

// Reader handles CBZ extraction
type Reader struct{}

// NewReader creates a new CBZ reader
func NewReader() *Reader {
	return &Reader{}
}

// Extract opens a CBZ and returns all contents
func (r *Reader) Extract(cbzPath string) (*Contents, error) {
	zipReader, err := zip.OpenReader(cbzPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CBZ %s: %w", cbzPath, err)
	}
	defer zipReader.Close()

	contents := &Contents{
		SourcePath: cbzPath,
		Images:     make([]ImageEntry, 0),
		OtherFiles: make([]OtherEntry, 0),
	}

	for _, file := range zipReader.File {
		// Skip directories
		if file.FileInfo().IsDir() {
			continue
		}

		// Skip hidden files (macOS resource forks, etc.)
		baseName := filepath.Base(file.Name)
		if strings.HasPrefix(baseName, ".") || strings.HasPrefix(baseName, "__MACOSX") {
			continue
		}
		if strings.Contains(file.Name, "__MACOSX") {
			continue
		}

		// Read file data
		data, err := r.readFileFromZip(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file.Name, err)
		}

		ext := strings.ToLower(filepath.Ext(file.Name))
		if SupportedImageExtensions[ext] {
			contents.Images = append(contents.Images, ImageEntry{
				Path:         file.Name,
				OriginalSize: int64(len(data)),
				Data:         data,
				ModTime:      file.Modified,
			})
		} else {
			// Preserve non-image files (e.g., ComicInfo.xml)
			contents.OtherFiles = append(contents.OtherFiles, OtherEntry{
				Path:    file.Name,
				Data:    data,
				ModTime: file.Modified,
			})
		}
	}

	// Sort images by path for consistent page order
	sort.Slice(contents.Images, func(i, j int) bool {
		return naturalLess(contents.Images[i].Path, contents.Images[j].Path)
	})

	return contents, nil
}

func (r *Reader) readFileFromZip(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// naturalLess compares strings with natural number ordering
// e.g., "page2" < "page10" (unlike lexicographic where "page10" < "page2")
func naturalLess(a, b string) bool {
	ai, bi := 0, 0
	for ai < len(a) && bi < len(b) {
		// Check if both are at a digit
		if isDigit(a[ai]) && isDigit(b[bi]) {
			// Extract numbers
			numA, endA := extractNumber(a, ai)
			numB, endB := extractNumber(b, bi)
			if numA != numB {
				return numA < numB
			}
			ai, bi = endA, endB
		} else {
			// Compare characters
			ca, cb := a[ai], b[bi]
			if ca != cb {
				return ca < cb
			}
			ai++
			bi++
		}
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func extractNumber(s string, start int) (int, int) {
	end := start
	for end < len(s) && isDigit(s[end]) {
		end++
	}
	num := 0
	for i := start; i < end; i++ {
		num = num*10 + int(s[i]-'0')
	}
	return num, end
}

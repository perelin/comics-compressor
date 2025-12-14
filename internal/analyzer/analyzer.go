package analyzer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

// SupportedImageExtensions for filtering
var supportedImageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
}

// AnalysisResult contains the quick scan results for a CBZ file
type AnalysisResult struct {
	FilePath       string
	FileSize       int64   // Total file size in bytes
	PageCount      int     // Number of images (pages)
	MaxWidth       int     // Maximum image width found
	MaxHeight      int     // Maximum image height found
	MBPerPage      float64 // Megabytes per page
	HasOversized   bool    // Any image exceeds max dimension
	HasNonJPEG     bool    // Any image is not JPEG (PNG, GIF, etc.)
	NeedsProcessing bool   // Final verdict: should this file be processed?
	SkipReason     string  // Why it's being skipped (if NeedsProcessing is false)
}

// Analyzer performs quick scans of CBZ files to determine if they need processing
type Analyzer struct {
	maxDimension    int
	thresholdMBPage float64
}

// NewAnalyzer creates a new analyzer with the given settings
func NewAnalyzer(maxDimension int, thresholdMBPage float64) *Analyzer {
	return &Analyzer{
		maxDimension:    maxDimension,
		thresholdMBPage: thresholdMBPage,
	}
}

// Analyze performs a quick scan of a CBZ file to determine if it needs processing
func (a *Analyzer) Analyze(cbzPath string) (*AnalysisResult, error) {
	result := &AnalysisResult{
		FilePath: cbzPath,
	}

	// Get file size
	info, err := os.Stat(cbzPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat %s: %w", cbzPath, err)
	}
	result.FileSize = info.Size()

	// Open the ZIP archive
	zipReader, err := zip.OpenReader(cbzPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CBZ %s: %w", cbzPath, err)
	}
	defer zipReader.Close()

	// Scan all images
	for _, file := range zipReader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		// Skip hidden files
		baseName := filepath.Base(file.Name)
		if strings.HasPrefix(baseName, ".") || strings.Contains(file.Name, "__MACOSX") {
			continue
		}

		ext := strings.ToLower(filepath.Ext(file.Name))
		if !supportedImageExtensions[ext] {
			continue
		}

		result.PageCount++

		// Check if non-JPEG
		if ext != ".jpg" && ext != ".jpeg" {
			result.HasNonJPEG = true
		}

		// Decode image config (header only, not full image)
		rc, err := file.Open()
		if err != nil {
			continue // Skip files we can't open
		}

		// Read just enough for header
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}

		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			continue // Skip files we can't decode
		}

		// Track max dimensions
		if cfg.Width > result.MaxWidth {
			result.MaxWidth = cfg.Width
		}
		if cfg.Height > result.MaxHeight {
			result.MaxHeight = cfg.Height
		}

		// Check if oversized
		if cfg.Width > a.maxDimension || cfg.Height > a.maxDimension {
			result.HasOversized = true
		}
	}

	// Calculate MB per page
	if result.PageCount > 0 {
		result.MBPerPage = float64(result.FileSize) / float64(result.PageCount) / (1024 * 1024)
	}

	// Determine if processing is needed
	result.NeedsProcessing = a.shouldProcess(result)

	return result, nil
}

// shouldProcess determines if a file needs processing based on analysis results
func (a *Analyzer) shouldProcess(result *AnalysisResult) bool {
	// Always process if has oversized images
	if result.HasOversized {
		return true
	}

	// Always process if has non-JPEG images (PNG, GIF, etc.)
	if result.HasNonJPEG {
		return true
	}

	// Process if exceeds MB/page threshold
	if result.MBPerPage > a.thresholdMBPage {
		return true
	}

	// File appears optimized, skip it
	result.SkipReason = fmt.Sprintf("already optimized (%.2f MB/page, max %dx%d)",
		result.MBPerPage, result.MaxWidth, result.MaxHeight)
	return false
}

// FormatAnalysis returns a human-readable summary of the analysis
func (a *Analyzer) FormatAnalysis(result *AnalysisResult) string {
	status := "[PROCESS]"
	reason := ""

	if !result.NeedsProcessing {
		status = "[SKIP]"
		reason = " - " + result.SkipReason
	} else {
		reasons := []string{}
		if result.HasOversized {
			reasons = append(reasons, fmt.Sprintf("oversized images (max %dx%d)", result.MaxWidth, result.MaxHeight))
		}
		if result.HasNonJPEG {
			reasons = append(reasons, "non-JPEG images")
		}
		if result.MBPerPage > a.thresholdMBPage {
			reasons = append(reasons, fmt.Sprintf("%.2f MB/page > %.2f threshold", result.MBPerPage, a.thresholdMBPage))
		}
		if len(reasons) > 0 {
			reason = " - " + strings.Join(reasons, ", ")
		}
	}

	return fmt.Sprintf("%s %s%s", status, filepath.Base(result.FilePath), reason)
}

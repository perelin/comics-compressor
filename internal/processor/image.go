package processor

import (
	"bytes"
	"fmt"
	"image"
	"path/filepath"
	"strings"

	"compress_comics/internal/cbz"

	"github.com/disintegration/imaging"
)

// ProcessedImage holds the result of processing one image
type ProcessedImage struct {
	NewPath      string // May change extension (.png -> .jpg)
	Data         []byte
	WasResized   bool
	WasConverted bool
	OriginalSize int64
	NewSize      int64
}

// ImageProcessor handles image resizing and conversion
type ImageProcessor struct {
	maxDimension int
	jpegQuality  int
}

// NewImageProcessor creates a processor with given settings
func NewImageProcessor(maxDim, quality int) *ImageProcessor {
	return &ImageProcessor{
		maxDimension: maxDim,
		jpegQuality:  quality,
	}
}

// Process takes a raw image entry and returns processed data
func (p *ImageProcessor) Process(entry cbz.ImageEntry) (*ProcessedImage, error) {
	// Decode image with auto-orientation (handles EXIF rotation)
	img, err := imaging.Decode(bytes.NewReader(entry.Data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", entry.Path, err)
	}

	result := &ProcessedImage{
		OriginalSize: entry.OriginalSize,
	}

	// Determine new filename (convert non-JPEG to .jpg)
	ext := strings.ToLower(filepath.Ext(entry.Path))
	if ext != ".jpg" && ext != ".jpeg" {
		// Change extension to .jpg
		result.NewPath = strings.TrimSuffix(entry.Path, ext) + ".jpg"
		result.WasConverted = true
	} else {
		result.NewPath = entry.Path
	}

	// Check if resize needed
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if width > p.maxDimension || height > p.maxDimension {
		// Use Fit to resize while preserving aspect ratio
		// Lanczos filter provides best quality for photographic content
		img = imaging.Fit(img, p.maxDimension, p.maxDimension, imaging.Lanczos)
		result.WasResized = true
	}

	// Encode as JPEG at target quality
	newData, err := p.encodeJPEG(img, p.jpegQuality)
	if err != nil {
		return nil, fmt.Errorf("failed to encode %s: %w", entry.Path, err)
	}
	newSize := int64(len(newData))

	isAlreadyJPEG := ext == ".jpg" || ext == ".jpeg"

	// If the new file is LARGER than original, we have a problem.
	// Try adaptive quality reduction to get it smaller.
	if newSize > entry.OriginalSize {
		// Try progressively lower quality until smaller or hit minimum (60)
		for quality := p.jpegQuality - 5; quality >= 60; quality -= 5 {
			attemptData, err := p.encodeJPEG(img, quality)
			if err != nil {
				break
			}
			attemptSize := int64(len(attemptData))
			if attemptSize < entry.OriginalSize {
				newData = attemptData
				newSize = attemptSize
				break
			}
			// Keep trying lower quality
			newData = attemptData
			newSize = attemptSize
		}
	}

	// Final check: if still larger and it was already a JPEG, keep original
	if newSize >= entry.OriginalSize && isAlreadyJPEG && !result.WasResized {
		result.Data = entry.Data
		result.NewSize = entry.OriginalSize
		result.NewPath = entry.Path
		result.WasConverted = false
		return result, nil
	}

	result.Data = newData
	result.NewSize = newSize

	return result, nil
}

// encodeJPEG encodes image as JPEG at given quality
func (p *ImageProcessor) encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(quality))
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ShouldProcess returns true if this image needs processing
func (p *ImageProcessor) ShouldProcess(entry cbz.ImageEntry, width, height int) bool {
	// Check if resize needed
	if width > p.maxDimension || height > p.maxDimension {
		return true
	}

	// Check if format conversion needed
	ext := strings.ToLower(filepath.Ext(entry.Path))
	if ext != ".jpg" && ext != ".jpeg" {
		return true
	}

	return false
}

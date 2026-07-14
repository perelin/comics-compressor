package analyzer

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), uint8((x + y) % 256), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func writeCBZ(t *testing.T, path string, pages map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create cbz: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, data := range pages {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create entry: %v", err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("write entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

// TestEstimateSavingsPerPage verifies that under per-page gating the dry-run
// estimate is based on the oversized pages' share of the archive, not on the
// whole file size.
func TestEstimateSavingsPerPage(t *testing.T) {
	dir := t.TempDir()
	cbzPath := filepath.Join(dir, "comic.cbz")

	oversized := makeJPEG(t, 400, 100)
	writeCBZ(t, cbzPath, map[string][]byte{
		"page1.jpg": makeJPEG(t, 150, 150),
		"page2.jpg": oversized,
		"page3.jpg": makeJPEG(t, 150, 150),
		"page4.jpg": makeJPEG(t, 150, 150),
	})

	// High threshold: only the oversized page triggers processing
	a := NewAnalyzer(200, 1000)
	result, err := a.Analyze(cbzPath)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !result.NeedsProcessing || !result.HasOversized || result.ExceedsThreshold {
		t.Fatalf("unexpected analysis: needs=%t oversized=%t threshold=%t",
			result.NeedsProcessing, result.HasOversized, result.ExceedsThreshold)
	}

	a.EstimateSavings(result)

	if result.EstimatedSavingsBytes <= 0 {
		t.Error("expected positive savings estimate for oversized page")
	}
	// The estimate must be bounded by the oversized page itself — the three
	// pass-through pages must not contribute.
	if result.EstimatedSavingsBytes > int64(len(oversized)) {
		t.Errorf("estimate %d exceeds oversized page size %d: whole-file estimation leaked back in",
			result.EstimatedSavingsBytes, len(oversized))
	}
}

// TestEstimateSavingsThreshold verifies the whole-file estimate still applies
// when the MB/page threshold fires (full re-encode).
func TestEstimateSavingsThreshold(t *testing.T) {
	dir := t.TempDir()
	cbzPath := filepath.Join(dir, "comic.cbz")
	writeCBZ(t, cbzPath, map[string][]byte{
		"page1.jpg": makeJPEG(t, 150, 150),
		"page2.jpg": makeJPEG(t, 150, 150),
	})

	a := NewAnalyzer(200, 0.000001) // everything exceeds the threshold
	result, err := a.Analyze(cbzPath)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !result.ExceedsThreshold {
		t.Fatal("expected threshold to fire")
	}

	a.EstimateSavings(result)

	want := int64(float64(result.FileSize) * 0.25)
	if result.EstimatedSavingsBytes < want {
		t.Errorf("estimate %d below whole-file re-encode estimate %d", result.EstimatedSavingsBytes, want)
	}
}

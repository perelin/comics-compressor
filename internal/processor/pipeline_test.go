package processor

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"cbz-compress/internal/cbz"
	"cbz-compress/internal/config"
)

// testConfig returns a config suitable for tests: high MB/page threshold so
// the full re-encode trigger stays off unless a test lowers it explicitly.
func testConfig(t *testing.T, dir string) config.Config {
	t.Helper()
	return config.Config{
		MaxDimension:    200,
		JPEGQuality:     90,
		BackupDir:       filepath.Join(dir, "backup"),
		ThresholdMBPage: 1000,
		MinSavingsPct:   0,
		Workers:         1,
	}
}

// makeJPEG encodes a gradient image (compressible, non-trivial) as JPEG.
func makeJPEG(t *testing.T, w, h int) []byte {
	return makeJPEGQuality(t, w, h, 90)
}

func makeJPEGQuality(t *testing.T, w, h, quality int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), uint8((x + y) % 256), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// makePNG encodes a flat-color image as PNG (tiny, converts poorly to JPEG).
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{40, 120, 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

type page struct {
	name string
	data []byte
}

func writeCBZ(t *testing.T, path string, pages []page) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create cbz: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, p := range pages {
		w, err := zw.Create(p.name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", p.name, err)
		}
		if _, err := w.Write(p.data); err != nil {
			t.Fatalf("write zip entry %s: %v", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func readCBZ(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open cbz: %v", err)
	}
	defer zr.Close()

	out := make(map[string][]byte)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", f.Name, err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("read entry %s: %v", f.Name, err)
		}
		rc.Close()
		out[f.Name] = buf.Bytes()
	}
	return out
}

// TestPerPageGating verifies that only pages needing work are modified and
// all other pages survive byte-identical.
func TestPerPageGating(t *testing.T) {
	dir := t.TempDir()
	cbzPath := filepath.Join(dir, "comic.cbz")

	oversized := makeJPEG(t, 400, 100) // exceeds MaxDimension 200
	small1 := makeJPEG(t, 100, 100)
	small2 := makeJPEG(t, 120, 80)
	small3 := makeJPEG(t, 90, 150)

	writeCBZ(t, cbzPath, []page{
		{"page1.jpg", small1},
		{"page2.jpg", oversized},
		{"page3.jpg", small2},
		{"page4.jpg", small3},
	})

	p := NewPipeline(testConfig(t, dir), nil)
	result, err := p.ProcessFile(cbzPath)
	if err != nil {
		t.Fatalf("ProcessFile: %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected processing, got skipped: %s", result.SkipReason)
	}
	if result.ImagesPassedThrough != 3 {
		t.Errorf("ImagesPassedThrough = %d, want 3", result.ImagesPassedThrough)
	}
	if result.ImagesProcessed != 1 {
		t.Errorf("ImagesProcessed = %d, want 1", result.ImagesProcessed)
	}

	out := readCBZ(t, cbzPath)
	for name, want := range map[string][]byte{
		"page1.jpg": small1, "page3.jpg": small2, "page4.jpg": small3,
	} {
		if !bytes.Equal(out[name], want) {
			t.Errorf("%s was modified, want byte-identical pass-through", name)
		}
	}

	if bytes.Equal(out["page2.jpg"], oversized) {
		t.Error("oversized page2.jpg was not modified")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out["page2.jpg"]))
	if err != nil {
		t.Fatalf("decode resized page: %v", err)
	}
	if cfg.Width > 200 || cfg.Height > 200 {
		t.Errorf("page2.jpg not resized: %dx%d", cfg.Width, cfg.Height)
	}
}

// TestSavingsGuardKeepsOriginal verifies that a file whose processing yields
// less than min_savings_pct is left completely untouched.
func TestSavingsGuardKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	cbzPath := filepath.Join(dir, "comic.cbz")

	// One tiny PNG triggers processing (HasNonJPEG); everything else passes
	// through, so total savings stay near zero — far below the 5% guard.
	writeCBZ(t, cbzPath, []page{
		{"page1.jpg", makeJPEG(t, 150, 150)},
		{"page2.jpg", makeJPEG(t, 150, 150)},
		{"page3.png", makePNG(t, 16, 16)},
	})

	before, err := os.ReadFile(cbzPath)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}

	cfg := testConfig(t, dir)
	cfg.MinSavingsPct = 5
	p := NewPipeline(cfg, nil)
	result, err := p.ProcessFile(cbzPath)
	if err != nil {
		t.Fatalf("ProcessFile: %v", err)
	}

	if !result.KeptOriginal {
		t.Errorf("KeptOriginal = false, want true (reason: %s)", result.SkipReason)
	}
	if !result.Skipped {
		t.Error("Skipped = false, want true for guard-kept file")
	}

	after, err := os.ReadFile(cbzPath)
	if err != nil {
		t.Fatalf("read file after processing: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("original file was modified despite savings guard")
	}

	// No temp files left behind, no backup created
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp*"))
	if len(matches) > 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
	if _, err := os.Stat(cfg.BackupDir); !os.IsNotExist(err) {
		t.Error("backup dir was created despite savings guard")
	}
}

// TestThresholdTriggersFullReencode verifies that the MB/page trigger still
// re-encodes every page (no pass-through) and that the file is replaced.
// q100 source pages guarantee that the q90 re-encode is genuinely smaller.
func TestThresholdTriggersFullReencode(t *testing.T) {
	dir := t.TempDir()
	cbzPath := filepath.Join(dir, "comic.cbz")

	p1 := makeJPEGQuality(t, 150, 150, 100)
	p2 := makeJPEGQuality(t, 150, 150, 100)
	writeCBZ(t, cbzPath, []page{
		{"page1.jpg", p1},
		{"page2.jpg", p2},
	})

	cfg := testConfig(t, dir)
	cfg.ThresholdMBPage = 0.000001 // every file exceeds this
	p := NewPipeline(cfg, nil)
	result, err := p.ProcessFile(cbzPath)
	if err != nil {
		t.Fatalf("ProcessFile: %v", err)
	}
	if result.ImagesPassedThrough != 0 {
		t.Errorf("ImagesPassedThrough = %d, want 0 when threshold trigger fires", result.ImagesPassedThrough)
	}
	if result.Skipped || result.KeptOriginal {
		t.Fatalf("file was not replaced: %s", result.SkipReason)
	}
	if result.ImagesProcessed != 2 {
		t.Errorf("ImagesProcessed = %d, want 2 (re-encoded pages count as modified)", result.ImagesProcessed)
	}

	out := readCBZ(t, cbzPath)
	if bytes.Equal(out["page1.jpg"], p1) || bytes.Equal(out["page2.jpg"], p2) {
		t.Error("pages were not re-encoded despite threshold trigger")
	}
}

// TestForceBypassesSavingsGuard verifies that -force replaces the file even
// when the savings guard would keep it.
func TestForceBypassesSavingsGuard(t *testing.T) {
	dir := t.TempDir()
	cbzPath := filepath.Join(dir, "comic.cbz")

	// q90 pages re-encoded at q90 yield ~zero savings: without force the
	// guard would keep the original (see TestSavingsGuardKeepsOriginal).
	writeCBZ(t, cbzPath, []page{
		{"page1.jpg", makeJPEG(t, 150, 150)},
		{"page2.jpg", makeJPEG(t, 150, 150)},
	})

	cfg := testConfig(t, dir)
	cfg.MinSavingsPct = 5
	cfg.Force = true
	p := NewPipeline(cfg, nil)
	result, err := p.ProcessFile(cbzPath)
	if err != nil {
		t.Fatalf("ProcessFile: %v", err)
	}
	if result.Skipped || result.KeptOriginal {
		t.Errorf("force run was kept/skipped: %s", result.SkipReason)
	}
	if _, err := os.Stat(filepath.Join(cfg.BackupDir, "comic.cbz")); err != nil {
		t.Errorf("original not found in backup after force replacement: %v", err)
	}
}

func TestShouldReplace(t *testing.T) {
	tests := []struct {
		name       string
		original   int64
		compressed int64
		minSavings float64
		want       bool
	}{
		{"exactly at threshold", 1000, 950, 5, true},
		{"just above threshold", 1000, 951, 5, false},
		{"well below threshold", 1000, 500, 5, true},
		{"zero guard, equal size", 1000, 1000, 0, true},
		{"zero guard, larger", 1000, 1001, 0, false},
		{"full guard never replaces", 1000, 1, 100, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldReplace(tt.original, tt.compressed, tt.minSavings); got != tt.want {
				t.Errorf("shouldReplace(%d, %d, %.1f) = %t, want %t",
					tt.original, tt.compressed, tt.minSavings, got, tt.want)
			}
		})
	}
}

func TestPageNeedsWork(t *testing.T) {
	dir := t.TempDir()
	p := NewPipeline(testConfig(t, dir), nil)

	tests := []struct {
		name  string
		entry cbz.ImageEntry
		want  bool
	}{
		{"non-JPEG extension", cbz.ImageEntry{Path: "a.png", Data: makePNG(t, 10, 10)}, true},
		{"oversized JPEG", cbz.ImageEntry{Path: "a.jpg", Data: makeJPEG(t, 400, 100)}, true},
		{"JPEG within limits", cbz.ImageEntry{Path: "a.jpg", Data: makeJPEG(t, 100, 100)}, false},
		{"undecodable JPEG passes through", cbz.ImageEntry{Path: "a.jpg", Data: []byte("not an image")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.pageNeedsWork(tt.entry); got != tt.want {
				t.Errorf("pageNeedsWork(%s) = %t, want %t", tt.entry.Path, got, tt.want)
			}
		})
	}
}

package processor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"compress_comics/internal/analyzer"
	"compress_comics/internal/backup"
	"compress_comics/internal/cbz"
	"compress_comics/internal/config"
)

// Result tracks the outcome of processing a single CBZ
type Result struct {
	SourcePath      string
	OutputPath      string
	OriginalSize    int64
	CompressedSize  int64
	ImagesProcessed int
	ImagesSkipped   int
	PNGsConverted   int
	Skipped         bool
	SkipReason      string
	Errors          []error
	Duration        time.Duration
	Analysis        *analyzer.AnalysisResult // For dry-run reporting
}

// BatchResult aggregates results for multiple files
type BatchResult struct {
	Results         []Result
	TotalOriginal   int64
	TotalCompressed int64
	TotalFiles      int
	ProcessedFiles  int
	SkippedFiles    int
	FailedFiles     int
	TotalDuration   time.Duration
}

// FileJob represents a file to be processed by a worker
type FileJob struct {
	Path  string
	Index int
	Total int
}

// FileResult wraps Result with job context for parallel processing
type FileResult struct {
	Job    FileJob
	Result *Result
	Error  error
}

// ProgressReporter interface for flexible progress output
type ProgressReporter interface {
	OnFileStart(path string, index, total int)
	OnFileSkipped(path string, reason string)
	OnImageProcessed(imagePath string, originalSize, newSize int64)
	OnFileComplete(result Result)
	OnBatchComplete(result BatchResult)
	OnDryRunFile(result *analyzer.AnalysisResult)
	OnDryRunComplete(summary *analyzer.DryRunSummary)
}

// Pipeline orchestrates the full compression process
type Pipeline struct {
	config    config.Config
	reader    *cbz.Reader
	writer    *cbz.Writer
	processor *ImageProcessor
	analyzer  *analyzer.Analyzer
	backup    *backup.Manager
	reporter  ProgressReporter
}

// NewPipeline creates a configured pipeline
func NewPipeline(cfg config.Config, reporter ProgressReporter) *Pipeline {
	return &Pipeline{
		config:    cfg,
		reader:    cbz.NewReader(),
		writer:    cbz.NewWriter(),
		processor: NewImageProcessor(cfg.MaxDimension, cfg.JPEGQuality),
		analyzer:  analyzer.NewAnalyzer(cfg.MaxDimension, cfg.ThresholdMBPage),
		backup:    backup.NewManager(cfg.BackupDir),
		reporter:  reporter,
	}
}

// ProcessFile handles a single CBZ file
func (p *Pipeline) ProcessFile(cbzPath string) (*Result, error) {
	startTime := time.Now()
	result := &Result{
		SourcePath: cbzPath,
		Errors:     make([]error, 0),
	}

	// Get original file info
	info, err := os.Stat(cbzPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat %s: %w", cbzPath, err)
	}
	result.OriginalSize = info.Size()

	// Analyze file first (unless force mode)
	var analysis *analyzer.AnalysisResult
	if !p.config.Force {
		var err error
		analysis, err = p.analyzer.Analyze(cbzPath)
		if err != nil {
			return nil, fmt.Errorf("analysis failed: %w", err)
		}

		// Dry run - report all files (skipped and to-process) via OnDryRunFile
		if p.config.DryRun {
			result.Duration = time.Since(startTime)
			// Calculate estimated savings for files that need processing
			p.analyzer.EstimateSavings(analysis)
			result.Analysis = analysis
			if !analysis.NeedsProcessing {
				result.Skipped = true
				result.SkipReason = analysis.SkipReason
			}
			if p.reporter != nil {
				p.reporter.OnDryRunFile(analysis)
			}
			return result, nil
		}

		if !analysis.NeedsProcessing {
			result.Skipped = true
			result.SkipReason = analysis.SkipReason
			result.Duration = time.Since(startTime)
			if p.reporter != nil {
				p.reporter.OnFileSkipped(cbzPath, analysis.SkipReason)
			}
			return result, nil
		}
	}

	// Extract CBZ
	contents, err := p.reader.Extract(cbzPath)
	if err != nil {
		return nil, err
	}

	// Process images
	entries := make([]cbz.WriteEntry, 0, len(contents.Images)+len(contents.OtherFiles))

	for _, img := range contents.Images {
		processed, err := p.processor.Process(img)
		if err != nil {
			// Log error but continue with other images
			result.Errors = append(result.Errors, err)
			// Keep original on error
			entries = append(entries, cbz.WriteEntry{
				Path: img.Path,
				Data: img.Data,
			})
			continue
		}

		entries = append(entries, cbz.WriteEntry{
			Path: processed.NewPath,
			Data: processed.Data,
		})

		if processed.WasResized || processed.WasConverted {
			result.ImagesProcessed++
		} else {
			result.ImagesSkipped++
		}
		if processed.WasConverted {
			result.PNGsConverted++
		}

		if p.reporter != nil && p.config.Verbose {
			p.reporter.OnImageProcessed(img.Path, processed.OriginalSize, processed.NewSize)
		}
	}

	// Include non-image files (like ComicInfo.xml)
	for _, other := range contents.OtherFiles {
		entries = append(entries, cbz.WriteEntry{
			Path: other.Path,
			Data: other.Data,
		})
	}

	// Create temporary output
	tempOutput, err := p.writer.CreateTemp(cbzPath, entries)
	if err != nil {
		return nil, fmt.Errorf("failed to create compressed CBZ: %w", err)
	}

	// Get compressed size
	compressedInfo, err := os.Stat(tempOutput)
	if err != nil {
		os.Remove(tempOutput)
		return nil, fmt.Errorf("failed to stat compressed file: %w", err)
	}
	result.CompressedSize = compressedInfo.Size()

	// Verify the new CBZ is valid before proceeding
	if err := p.verifyCompressedCBZ(tempOutput); err != nil {
		os.Remove(tempOutput)
		return nil, fmt.Errorf("verification failed: %w", err)
	}

	// Move original to backup
	if err := p.backup.MoveToBackup(cbzPath); err != nil {
		os.Remove(tempOutput)
		return nil, fmt.Errorf("backup failed: %w", err)
	}

	// Rename compressed to original location
	if err := os.Rename(tempOutput, cbzPath); err != nil {
		// Try to restore from backup
		if restoreErr := p.backup.RestoreFromBackup(cbzPath); restoreErr != nil {
			return nil, fmt.Errorf("CRITICAL: rename failed and restore failed: %w (restore: %v)", err, restoreErr)
		}
		os.Remove(tempOutput)
		return nil, fmt.Errorf("rename failed (original restored): %w", err)
	}

	result.OutputPath = cbzPath
	result.Duration = time.Since(startTime)

	return result, nil
}

// verifyCompressedCBZ checks that the new CBZ is valid
func (p *Pipeline) verifyCompressedCBZ(path string) error {
	contents, err := p.reader.Extract(path)
	if err != nil {
		return fmt.Errorf("cannot read compressed CBZ: %w", err)
	}
	if len(contents.Images) == 0 {
		return fmt.Errorf("compressed CBZ has no images")
	}
	return nil
}

// ProcessDirectory processes all CBZ files in a directory
func (p *Pipeline) ProcessDirectory(dirPath string) (*BatchResult, error) {
	// Find all CBZ files
	var cbzFiles []string

	// Get absolute path of backup directory to skip it during walk
	backupDirAbs, _ := filepath.Abs(p.config.BackupDir)

	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip backup directory entirely (idempotency: never process backups)
		if info.IsDir() {
			absPath, _ := filepath.Abs(path)
			if absPath == backupDirAbs {
				return filepath.SkipDir
			}
		}

		if !info.IsDir() && strings.ToLower(filepath.Ext(path)) == ".cbz" {
			cbzFiles = append(cbzFiles, path)
		}
		if !p.config.Recursive && info.IsDir() && path != dirPath {
			return filepath.SkipDir
		}
		return nil
	}

	if err := filepath.Walk(dirPath, walkFn); err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	totalFiles := len(cbzFiles)
	if totalFiles == 0 {
		return &BatchResult{TotalFiles: 0}, nil
	}

	// Determine worker count
	workers := p.config.Workers
	if workers > totalFiles {
		workers = totalFiles // No point having more workers than files
	}
	if workers < 1 {
		workers = 1
	}

	// Single worker path (avoid goroutine overhead)
	if workers == 1 {
		return p.processDirectorySequential(cbzFiles)
	}

	return p.processDirectoryParallel(cbzFiles, workers)
}

// processDirectorySequential processes files one at a time (original behavior)
func (p *Pipeline) processDirectorySequential(cbzFiles []string) (*BatchResult, error) {
	batch := &BatchResult{
		Results:    make([]Result, 0, len(cbzFiles)),
		TotalFiles: len(cbzFiles),
	}
	startTime := time.Now()

	for i, cbzPath := range cbzFiles {
		if p.reporter != nil {
			p.reporter.OnFileStart(cbzPath, i+1, len(cbzFiles))
		}

		result, err := p.ProcessFile(cbzPath)
		if err != nil {
			batch.FailedFiles++
			batch.Results = append(batch.Results, Result{
				SourcePath: cbzPath,
				Errors:     []error{err},
			})
			continue
		}

		batch.Results = append(batch.Results, *result)

		if result.Skipped {
			batch.SkippedFiles++
		} else {
			batch.ProcessedFiles++
			batch.TotalOriginal += result.OriginalSize
			batch.TotalCompressed += result.CompressedSize
		}

		if p.reporter != nil {
			p.reporter.OnFileComplete(*result)
		}
	}

	batch.TotalDuration = time.Since(startTime)

	if p.reporter != nil {
		// In dry-run mode, show the dry-run summary instead of batch summary
		if p.config.DryRun {
			analysisResults := make([]*analyzer.AnalysisResult, 0, len(batch.Results))
			for i := range batch.Results {
				if batch.Results[i].Analysis != nil {
					analysisResults = append(analysisResults, batch.Results[i].Analysis)
				}
			}
			summary := analyzer.NewDryRunSummary(analysisResults)
			p.reporter.OnDryRunComplete(summary)
		} else {
			p.reporter.OnBatchComplete(*batch)
		}
	}

	return batch, nil
}

// processDirectoryParallel processes files concurrently using a worker pool
func (p *Pipeline) processDirectoryParallel(cbzFiles []string, numWorkers int) (*BatchResult, error) {
	startTime := time.Now()
	totalFiles := len(cbzFiles)

	// Create a safe reporter for concurrent use
	var safeReporter ProgressReporter
	if p.reporter != nil {
		safeReporter = NewSafeReporter(p.reporter)
	}

	// Create channels
	jobs := make(chan FileJob, numWorkers)
	results := make(chan FileResult, numWorkers)

	// Start worker pool
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.worker(jobs, results, safeReporter)
		}()
	}

	// Send jobs (in separate goroutine to avoid deadlock)
	go func() {
		for i, path := range cbzFiles {
			jobs <- FileJob{Path: path, Index: i + 1, Total: totalFiles}
		}
		close(jobs)
	}()

	// Close results when all workers done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	batch := &BatchResult{
		Results:    make([]Result, 0, totalFiles),
		TotalFiles: totalFiles,
	}

	for res := range results {
		if res.Error != nil {
			batch.FailedFiles++
			batch.Results = append(batch.Results, Result{
				SourcePath: res.Job.Path,
				Errors:     []error{res.Error},
			})
			continue
		}

		batch.Results = append(batch.Results, *res.Result)

		if res.Result.Skipped {
			batch.SkippedFiles++
		} else {
			batch.ProcessedFiles++
			batch.TotalOriginal += res.Result.OriginalSize
			batch.TotalCompressed += res.Result.CompressedSize
		}

		if safeReporter != nil {
			safeReporter.OnFileComplete(*res.Result)
		}
	}

	batch.TotalDuration = time.Since(startTime)

	if p.reporter != nil {
		// In dry-run mode, show the dry-run summary instead of batch summary
		if p.config.DryRun {
			analysisResults := make([]*analyzer.AnalysisResult, 0, len(batch.Results))
			for i := range batch.Results {
				if batch.Results[i].Analysis != nil {
					analysisResults = append(analysisResults, batch.Results[i].Analysis)
				}
			}
			summary := analyzer.NewDryRunSummary(analysisResults)
			p.reporter.OnDryRunComplete(summary)
		} else {
			p.reporter.OnBatchComplete(*batch)
		}
	}

	return batch, nil
}

// worker processes files from the jobs channel and sends results
func (p *Pipeline) worker(jobs <-chan FileJob, results chan<- FileResult, reporter ProgressReporter) {
	for job := range jobs {
		if reporter != nil {
			reporter.OnFileStart(job.Path, job.Index, job.Total)
		}

		result, err := p.ProcessFile(job.Path)
		results <- FileResult{
			Job:    job,
			Result: result,
			Error:  err,
		}
	}
}

// ConsoleReporter implements ProgressReporter for terminal output
type ConsoleReporter struct {
	verbose bool
	writer  io.Writer
}

// NewConsoleReporter creates a console reporter
func NewConsoleReporter(verbose bool, writer io.Writer) *ConsoleReporter {
	return &ConsoleReporter{
		verbose: verbose,
		writer:  writer,
	}
}

func (r *ConsoleReporter) OnFileStart(path string, index, total int) {
	fmt.Fprintf(r.writer, "[%d/%d] Processing: %s\n", index, total, filepath.Base(path))
}

func (r *ConsoleReporter) OnFileSkipped(path string, reason string) {
	fmt.Fprintf(r.writer, "  [SKIP] %s\n", reason)
}

func (r *ConsoleReporter) OnImageProcessed(imagePath string, originalSize, newSize int64) {
	if r.verbose {
		savings := float64(originalSize-newSize) / float64(originalSize) * 100
		fmt.Fprintf(r.writer, "    %s: %s -> %s (%.1f%% saved)\n",
			filepath.Base(imagePath),
			formatBytes(originalSize),
			formatBytes(newSize),
			savings)
	}
}

func (r *ConsoleReporter) OnFileComplete(result Result) {
	if result.Skipped {
		return // Already reported in OnFileSkipped
	}
	if result.OriginalSize > 0 && result.CompressedSize > 0 {
		savings := float64(result.OriginalSize-result.CompressedSize) / float64(result.OriginalSize) * 100
		fmt.Fprintf(r.writer, "  [DONE] %s -> %s (%.1f%% saved, %d images, %v)\n",
			formatBytes(result.OriginalSize),
			formatBytes(result.CompressedSize),
			savings,
			result.ImagesProcessed,
			result.Duration.Round(time.Millisecond))
	}
}

func (r *ConsoleReporter) OnBatchComplete(result BatchResult) {
	fmt.Fprintln(r.writer)
	fmt.Fprintln(r.writer, "=== Summary ===")
	fmt.Fprintf(r.writer, "Total files:    %d\n", result.TotalFiles)
	fmt.Fprintf(r.writer, "Processed:      %d\n", result.ProcessedFiles)
	fmt.Fprintf(r.writer, "Skipped:        %d\n", result.SkippedFiles)
	fmt.Fprintf(r.writer, "Failed:         %d\n", result.FailedFiles)

	if result.TotalOriginal > 0 {
		savings := float64(result.TotalOriginal-result.TotalCompressed) / float64(result.TotalOriginal) * 100
		fmt.Fprintf(r.writer, "Original size:  %s\n", formatBytes(result.TotalOriginal))
		fmt.Fprintf(r.writer, "Compressed:     %s\n", formatBytes(result.TotalCompressed))
		fmt.Fprintf(r.writer, "Savings:        %s (%.1f%%)\n",
			formatBytes(result.TotalOriginal-result.TotalCompressed), savings)
	}
	fmt.Fprintf(r.writer, "Duration:       %v\n", result.TotalDuration.Round(time.Second))
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func (r *ConsoleReporter) OnDryRunFile(result *analyzer.AnalysisResult) {
	fileName := filepath.Base(result.FilePath)
	sizeStr := formatBytes(result.FileSize)

	if result.NeedsProcessing {
		savingsStr := fmt.Sprintf("~%s (%.0f%%)",
			formatBytes(result.EstimatedSavingsBytes),
			result.EstimatedSavingsPct)
		reasonStr := strings.Join(result.ProcessingReasons, ", ")
		fmt.Fprintf(r.writer, "  %-40s %10s  %15s  %s\n",
			truncateString(fileName, 40), sizeStr, savingsStr, reasonStr)
	} else {
		fmt.Fprintf(r.writer, "  %-40s %10s  %15s  [SKIP] %s\n",
			truncateString(fileName, 40), sizeStr, "-", result.SkipReason)
	}
}

func (r *ConsoleReporter) OnDryRunComplete(summary *analyzer.DryRunSummary) {
	fmt.Fprintln(r.writer)
	fmt.Fprintln(r.writer, "=== DRY RUN SUMMARY ===")
	fmt.Fprintf(r.writer, "Files to process: %d\n", len(summary.FilesToProcess))
	fmt.Fprintf(r.writer, "Files to skip:    %d\n", len(summary.FilesToSkip))

	if len(summary.FilesToProcess) > 0 {
		fmt.Fprintln(r.writer)
		fmt.Fprintln(r.writer, "ESTIMATED TOTALS:")
		fmt.Fprintf(r.writer, "  Current size:      %s\n", formatBytes(summary.TotalCurrentSize))
		fmt.Fprintf(r.writer, "  Estimated after:   ~%s\n", formatBytes(summary.TotalEstimatedNew))
		fmt.Fprintf(r.writer, "  Estimated savings: ~%s (%.1f%%)\n",
			formatBytes(summary.TotalSavings), summary.SavingsPercent)
		fmt.Fprintln(r.writer)
		fmt.Fprintln(r.writer, "Note: Estimates are approximate. Actual savings may vary.")
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// SafeReporter wraps a ProgressReporter with mutex protection for concurrent use
type SafeReporter struct {
	reporter ProgressReporter
	mu       sync.Mutex
}

// NewSafeReporter creates a thread-safe reporter wrapper
func NewSafeReporter(reporter ProgressReporter) *SafeReporter {
	return &SafeReporter{reporter: reporter}
}

func (s *SafeReporter) OnFileStart(path string, index, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reporter != nil {
		s.reporter.OnFileStart(path, index, total)
	}
}

func (s *SafeReporter) OnFileSkipped(path string, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reporter != nil {
		s.reporter.OnFileSkipped(path, reason)
	}
}

func (s *SafeReporter) OnImageProcessed(imagePath string, originalSize, newSize int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reporter != nil {
		s.reporter.OnImageProcessed(imagePath, originalSize, newSize)
	}
}

func (s *SafeReporter) OnFileComplete(result Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reporter != nil {
		s.reporter.OnFileComplete(result)
	}
}

func (s *SafeReporter) OnBatchComplete(result BatchResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reporter != nil {
		s.reporter.OnBatchComplete(result)
	}
}

func (s *SafeReporter) OnDryRunFile(result *analyzer.AnalysisResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reporter != nil {
		s.reporter.OnDryRunFile(result)
	}
}

func (s *SafeReporter) OnDryRunComplete(summary *analyzer.DryRunSummary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reporter != nil {
		s.reporter.OnDryRunComplete(summary)
	}
}

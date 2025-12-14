package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"compress_comics/internal/config"
	"compress_comics/internal/processor"
)

var version = "1.0.0"

func main() {
	// Define flags
	var (
		inputPath   string
		backupDir   string
		maxDim      int
		quality     int
		threshold   float64
		recursive   bool
		force       bool
		dryRun      bool
		verbose     bool
		workers     int
		showVersion bool
	)

	flag.StringVar(&inputPath, "input", "", "Path to CBZ file or directory (required)")
	flag.StringVar(&inputPath, "i", "", "Path to CBZ file or directory (shorthand)")

	flag.StringVar(&backupDir, "backup", "originals_backup", "Directory to store original files")
	flag.StringVar(&backupDir, "b", "originals_backup", "Backup directory (shorthand)")

	flag.IntVar(&maxDim, "max-dim", 1800, "Maximum dimension in pixels (long edge)")
	flag.IntVar(&quality, "quality", 90, "JPEG quality (1-100)")
	flag.IntVar(&quality, "q", 90, "JPEG quality (shorthand)")

	flag.Float64Var(&threshold, "threshold", 1.5, "MB per page threshold for skip heuristic")
	flag.Float64Var(&threshold, "t", 1.5, "MB per page threshold (shorthand)")

	flag.BoolVar(&recursive, "recursive", true, "Process directories recursively")
	flag.BoolVar(&recursive, "r", true, "Recursive (shorthand)")

	flag.BoolVar(&force, "force", false, "Process even if file appears optimized")
	flag.BoolVar(&force, "f", false, "Force processing (shorthand)")

	flag.BoolVar(&dryRun, "dry-run", false, "Preview changes without modifying files")
	flag.BoolVar(&verbose, "verbose", false, "Show detailed progress")
	flag.BoolVar(&verbose, "v", false, "Verbose (shorthand)")

	flag.IntVar(&workers, "workers", runtime.NumCPU(), "Number of parallel workers for directory processing")
	flag.IntVar(&workers, "w", runtime.NumCPU(), "Parallel workers (shorthand)")

	flag.BoolVar(&showVersion, "version", false, "Show version information")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "CBZ Compressor v%s\n\n", version)
		fmt.Fprintf(os.Stderr, "Compresses CBZ comic book files for tablet reading.\n")
		fmt.Fprintf(os.Stderr, "Optimizes images to max %d pixels, JPEG quality %d.\n\n", maxDim, quality)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s -input <path> [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s -input comic.cbz\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -input ./comics -recursive\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -input ./comics -dry-run -verbose\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -input ./comics -q 85 -max-dim 1600\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -input ./comics -force\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -input ./comics -w 4\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("cbz-compress v%s\n", version)
		os.Exit(0)
	}

	if inputPath == "" {
		fmt.Fprintln(os.Stderr, "Error: -input is required")
		flag.Usage()
		os.Exit(1)
	}

	// Validate quality
	if quality < 1 || quality > 100 {
		fmt.Fprintln(os.Stderr, "Error: quality must be between 1 and 100")
		os.Exit(1)
	}

	// Validate workers
	if workers < 1 {
		fmt.Fprintln(os.Stderr, "Error: workers must be at least 1")
		os.Exit(1)
	}

	// Build config
	cfg := config.Config{
		MaxDimension:    maxDim,
		JPEGQuality:     quality,
		BackupDir:       backupDir,
		ThresholdMBPage: threshold,
		Recursive:       recursive,
		Force:           force,
		DryRun:          dryRun,
		Verbose:         verbose,
		Workers:         workers,
	}

	// Create reporter
	reporter := processor.NewConsoleReporter(verbose, os.Stdout)

	// Create pipeline
	pipeline := processor.NewPipeline(cfg, reporter)

	// Determine if input is file or directory
	info, err := os.Stat(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot access %s: %v\n", inputPath, err)
		os.Exit(1)
	}

	if dryRun {
		fmt.Println("=== DRY RUN MODE - No files will be modified ===")
		fmt.Println()
	}

	if info.IsDir() {
		result, err := pipeline.ProcessDirectory(inputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if result.FailedFiles > 0 {
			os.Exit(1)
		}
	} else {
		result, err := pipeline.ProcessFile(inputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(result.Errors) > 0 {
			for _, e := range result.Errors {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", e)
			}
		}
	}
}

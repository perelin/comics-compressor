# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CBZ Compressor is a Go CLI tool that compresses CBZ (Comic Book ZIP) files for tablet reading. It resizes images to a maximum dimension, converts non-JPEG formats to JPEG, and uses smart heuristics to skip already-optimized files.

## Build and Run Commands

```bash
# Build the binary
go build -o cbz-compress .

# Run with a single file
./cbz-compress -input comic.cbz

# Run with a directory (recursive by default)
./cbz-compress -input ./comics

# Preview what would be processed without making changes
./cbz-compress -input ./comics -dry-run -verbose

# Force processing even for optimized files
./cbz-compress -input ./comics -force

# Custom quality and max dimension
./cbz-compress -input ./comics -q 85 -max-dim 1600

# Parallel processing (4 workers)
./cbz-compress -input ./comics -w 4
```

## Architecture

The project follows a pipeline architecture with clear separation of concerns:

```
main.go           # CLI entry point, flag parsing, config building
internal/
  config/         # Config struct with compression settings
  analyzer/       # Quick scan to determine if CBZ needs processing (reads image headers only)
  cbz/            # Reader extracts CBZ contents, Writer creates new CBZ with atomic writes
  processor/      # Pipeline orchestrates the full flow, ImageProcessor handles resize/convert
  backup/         # Moves originals to backup dir before replacing
```

### Key Flow

1. **Analysis** (`analyzer/`): Quick scan reads only image headers to check dimensions and formats. Uses heuristic (MB/page threshold) to skip already-optimized files.

2. **Processing** (`processor/`):
   - Extract all images from CBZ
   - Resize images exceeding max dimension using Lanczos filter
   - Convert PNG/GIF/WebP to JPEG
   - Adaptive quality reduction if output is larger than input

3. **Atomic Writes** (`cbz/writer.go`): Creates temp file, writes compressed CBZ, then atomically renames to final path.

4. **Backup Safety** (`backup/`): Original files are moved to backup directory before replacement. Restore is attempted on failure.

### Important Design Decisions

- Images are sorted using natural sort ordering (page2 < page10)
- Non-image files (e.g., ComicInfo.xml) are preserved unchanged
- Hidden files and macOS resource forks (__MACOSX) are skipped
- If re-encoding produces a larger file than the original JPEG, the original is kept
- **Idempotent operation**: The backup directory is automatically excluded from directory scans, preventing accidental re-processing of backed-up originals
- **Parallel processing**: Directory processing uses a worker pool pattern for concurrent file processing. Progress output may appear out-of-order. Thread-safety is handled via `SafeReporter` (mutex-protected) and mutex-protected backup manager.

## Configuration

The `cbz-compress.yaml` file controls default values. It is **embedded at build time** using `go:embed`, so the binary contains its own defaults. A runtime config file in the current directory can override embedded values.

```yaml
# cbz-compress.yaml
max_dimension: 1800
jpeg_quality: 90
threshold_mb_per_page: 1.5
backup_dir: "originals_backup"
```

Precedence (lowest to highest):
1. Hardcoded fallbacks (safety net)
2. Embedded config (compiled into binary at build time)
3. Runtime config file (`./cbz-compress.yaml` in current directory)
4. CLI flags

**Build-time customization:** Edit `cbz-compress.yaml` before building to bake your preferred defaults into the binary. The file is required for building.

## Dependencies

- `github.com/disintegration/imaging` - Image processing (resize, format conversion)
- `golang.org/x/image/webp` - WebP format support
- `gopkg.in/yaml.v3` - YAML config file parsing

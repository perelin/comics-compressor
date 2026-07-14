# comics-compressor

A command-line tool to compress and optimize CBZ comic book archives for tablet reading. Reduces file sizes while maintaining visual quality.

## Features

- **Smart Compression**: Resizes images to a maximum dimension while preserving aspect ratio
- **Quality First**: Pages that don't need work are copied byte-identical (no re-encoding); originals are only replaced if compression saves a meaningful amount
- **Backup Support**: Automatically backs up original files before modification
- **Batch Processing**: Process individual files or entire directories
- **Parallel Processing**: Utilizes multiple CPU cores for faster batch operations
- **Dry Run Mode**: Preview changes without modifying files
- **Skip Heuristic**: Automatically skips files that appear already optimized
- **Customizable Quality**: Adjust JPEG quality and output dimensions

## Installation

### From Source

```bash
git clone https://github.com/perelin/comics-compressor
cd comics-compressor
go build -o cbz-compress .
```

### Download Binary

Download the latest release from the [GitHub releases page](https://github.com/perelin/comics-compressor/releases).

## Usage

### Basic Commands

```bash
# Compress a single CBZ file
cbz-compress -i comic.cbz

# Compress all CBZ files in a directory (recursive)
cbz-compress -i ./comics

# Preview changes without modifying files
cbz-compress -i ./comics -dry-run

# Compress with custom settings
cbz-compress -i ./comics -q 85 -max-dim 1600
```

### Command-Line Options

| Flag | Shorthand | Default | Description |
|------|-----------|---------|-------------|
| `-input` | `-i` | (required) | Path to CBZ file or directory |
| `-quality` | `-q` | 90 | JPEG quality (1-100) |
| `-max-dim` | | 4098 | Maximum dimension in pixels (long edge) |
| `-backup` | `-b` | `originals_backup` | Directory for original backups |
| `-recursive` | `-r` | true | Process directories recursively |
| `-workers` | `-w` | CPU count | Number of parallel workers |
| `-dry-run` | | false | Preview without modifying |
| `-force` | `-f` | false | Process even if file appears optimized; re-encodes all pages and replaces the file even if the savings guard would keep it |
| `-threshold` | `-t` | 3 | MB/page threshold for skip heuristic |
| `-min-savings` | | 5 | Minimum savings percent required to replace a file (0-100) |
| `-verbose` | `-v` | false | Show detailed progress |
| `-version` | | false | Show version information |

### Configuration File

Create a `cbz-compress.yaml` file to set default values:

```yaml
# Maximum dimension in pixels (width or height)
max_dimension: 4098

# JPEG quality (1-100)
jpeg_quality: 90

# MB per page threshold for skip heuristic
threshold_mb_per_page: 3

# Minimum savings (percent) required to replace a file
min_savings_pct: 5

# Directory to store original files
backup_dir: "originals_backup"

# Patterns to skip
skip_patterns:
  - "._*"      # macOS resource forks
  - ".DS_Store" # macOS folder metadata
  - "__MACOSX" # macOS archive artifacts
```

## How It Works

1. **Analysis**: Scans each page in the CBZ archive and measures average page size
2. **Skip Check**: Files below the threshold are assumed optimized and skipped
3. **Per-Page Processing**: Only pages that individually need work (oversized or non-JPEG) are resized/converted; all other pages are copied byte-identical. If the file exceeds the MB/page threshold (or `-force` is set), every page is re-encoded
4. **Savings Guard**: The original is only replaced if the result is at least `min_savings_pct` percent smaller — otherwise the file is kept untouched (`[KEPT]` in the output). `-force` bypasses the guard: it is an explicit request to re-encode and always replaces
5. **Backup**: Original files are saved to the backup directory before replacement

## Requirements

- Go 1.21+ (for building from source)
- CBZ/CBR archives (CBZ = ZIP-based comic archives)

## License

MIT License - see LICENSE file for details.
# Spec: Quality-First Compression

**Status:** Draft — agreed 2026-07-13
**Goal:** Keep comics at high quality. The tool's job is to *find and fix badly or
unnecessarily large compressed files* — not to re-encode everything it touches.

This spec captures an agreed analysis of the current pipeline and defines three
independent work packages (WP1–WP3), each implementable in its own session.

---

## Problem analysis (verified against current code)

| # | Finding | Location |
|---|---------|----------|
| P1 | Decision is per-**file**, processing is per-**page-all**: one oversized image or one PNG cover triggers re-encode of *every* page at q90. Any JPEG page whose re-encode is even 1 byte smaller is replaced → generation loss on pages that were never a problem. | `pipeline.go:157` (`ProcessFile` loops `processor.Process` over all images) |
| P2 | `ImageProcessor.ShouldProcess()` implements exactly the per-page gating we need — but is **never called**. Dead code. | `image.go:129` |
| P3 | Adaptive quality loop silently degrades to **q60** when the re-encode is larger than the original, and keeps the q60 result for converted/resized images *even if still larger*. | `image.go:84–101` |
| P4 | No file-level size check before replacing: a CBZ that ends up same-size or larger still replaces the original. Pure quality loss, zero gain. | `pipeline.go:217–231` (no `CompressedSize` vs `OriginalSize` comparison) |
| P5 | MB/page heuristic ignores resolution: 2.5 MB/page at 4000 px is *efficient*; 2.5 MB/page at 1600 px is *bloated*. The metric cannot distinguish "large because high-res" (keep!) from "large because badly encoded" (target!). | `analyzer.go:150–170` |
| P6 | The strongest available signal is unused: JPEG quantization tables allow estimating the encoding quality from the header. A q95–100 file is "unnecessarily large"; an already-q75 file that is big only because of resolution must not be touched (re-encode = generation loss, little gain). | not implemented |
| P7 | `HasNonJPEG` forces conversion unconditionally. For flat-color / line-art comics, PNG is often smaller *and* lossless; forced JPEG conversion makes them larger and worse (then hits P3 and lands at q60). | `analyzer.go:157`, `image.go:52–59` |
| P8 | Analyzer reads each image fully into memory (`io.ReadAll`) although only the header is needed. Slows scans of large libraries. | `analyzer.go:113` |

What stays as-is (explicitly good): backup-before-replace, atomic rename,
post-write verification, restore on failure, backup-dir exclusion, two-phase
cheap-scan → expensive-process design, worker pool.

---

## WP1 — Per-page gating + file-level savings guard

*Smallest change, biggest quality protection. No heuristic changes.*

### Changes

1. **Per-page gating** (fixes P1, P2): In `Pipeline.ProcessFile`, decide per
   image whether it needs work. Pages that need nothing are passed through
   **byte-identical** (original bytes, original path — no decode/re-encode).
   Wire up (and adapt) the existing `ImageProcessor.ShouldProcess`:
   - resize needed (dimension > max) → process
   - conversion decision (see WP3; until then: non-JPEG → process)
   - otherwise → pass through untouched
   - Note: per-page dimension info requires a header decode per image during
     processing (cheap vs. full decode) or reuse of analyzer results.
2. **File-level savings guard** (fixes P4): after writing the temp CBZ, replace
   the original only if `CompressedSize <= OriginalSize * (1 - min_savings)`.
   Otherwise: delete temp, report file as
   `[KEPT] no meaningful savings (-X%)`, count as skipped-after-processing.
   - New config key `min_savings_pct` (default: **5**), CLI flag `-min-savings`.
3. Reporting: `Result` gains pass-through counts so output distinguishes
   "pages untouched / resized / converted / re-encoded".

### Acceptance criteria

- A CBZ with 1 oversized page out of 200: exactly 1 page is modified; the other
  199 are byte-identical in the output (test asserts on raw bytes).
- A CBZ whose processing yields < min_savings: original file untouched
  (same inode/mtime), temp cleaned up, correct report line.
- Unit tests for the guard boundary (exactly at threshold) and pass-through.

---

## WP2 — Resolution-aware heuristic (bits/pixel) + JPEG quality estimation

*Makes "find the badly compressed files" precise. Analyzer-only; no change to
what processing does — only to what gets selected.*

### Changes

1. **Bits/pixel metric** (fixes P5): analyzer already reads per-image
   dimensions; additionally track per-image compressed size (available from
   the zip entry) and compute per-file
   `avg_bpp = total_image_bytes * 8 / total_pixels`.
   - New trigger: `avg_bpp > bpp_threshold` (config `bpp_threshold`,
     default: **3.0**, calibrate — see Verification).
   - `threshold_mb_per_page` is retired as a *trigger* but kept in the report
     output for context. Config key remains readable (deprecation note) so
     existing configs don't break.
2. **JPEG quality estimation via quantization tables** (fixes P6): parse DQT
   segments from the JPEG header (header-only, no full decode; small
   self-contained parser, no new dependency) and estimate libjpeg-equivalent
   quality per image.
   - Skip rule: if estimated quality ≤ `requality_threshold`
     (default: **92** with target q90), re-encoding gains little →
     do *not* trigger on bpp for that file; report
     `[SKIP] already efficiently encoded (~qXX, Y.Y bpp)`.
   - Trigger rule: estimated quality ≥ 95 *and* bpp above threshold →
     prime candidate ("unnecessarily large").
3. **Header-only scan** (fixes P8): replace `io.ReadAll` with a bounded read
   (e.g. `io.LimitReader`, 512 KiB — SOF/DQT can sit behind large EXIF blobs;
   fall back to full read if header parse fails within the limit).
4. Dry-run report gains columns: `bpp`, `~q` (estimated quality), so the
   selection is auditable before anything is touched.

### Acceptance criteria

- Synthetic fixtures: same image encoded at q70/q90/q100 → estimator within
  ±5 of the true setting; bpp computed correctly.
- A high-res but efficient file (q85, 4000 px, high MB/page) is **skipped**;
  a low-res bloated file (q100, 1600 px) is **selected**.
- Dry-run on a real library (see Verification) reviewed before defaults are
  finalized.

---

## WP3 — PNG policy + quality floor

*Stops the two remaining "quality destroyed for size" paths.*

### Changes

1. **Conditional PNG/GIF/WebP conversion** (fixes P7): convert to JPEG only if
   the JPEG at target quality is ≤ `convert_max_ratio` (default: **0.85**) of
   the original size. Otherwise keep the original format/bytes untouched.
   - Consequence for the analyzer: `HasNonJPEG` alone no longer forces
     `NeedsProcessing = true`; non-JPEG pages are *candidates*, decided
     per page at processing time.
   - A conversion that would *increase* size is never accepted (today it is,
     via P3's loop-end state).
2. **Quality floor** (fixes P3): adaptive loop stops at
   `min_jpeg_quality` (config, default: **80**, was hardcoded 60). If the
   result is still not smaller at the floor: keep the original bytes
   (for resized images: keep the smallest attempt ≥ floor — resizing must
   still happen, but never below the floor).
3. Both parameters in YAML config + CLI flags, documented in README and
   embedded `cbz-compress.yaml`.

### Acceptance criteria

- Line-art PNG whose JPEG version would be larger: stays PNG, byte-identical.
- Photographic PNG with clear JPEG win: converted.
- No output image is ever encoded below `min_jpeg_quality`; test asserts the
  loop floor.

---

## Cross-cutting

- **Tests are part of each WP**, not follow-up (repo currently has zero tests).
  Each WP adds unit tests for its logic; WP1 additionally adds a small
  end-to-end test (build tiny CBZs in `t.TempDir()` with generated images).
- **Docs**: README + CLAUDE.md updated per WP (flags, config keys, heuristic
  description).
- Config precedence rules stay unchanged (fallbacks → embedded → runtime YAML
  → flags).

## Verification (after WP2, before finalizing thresholds)

Run `-dry-run -verbose` with old vs. new heuristic against the real library
and compare selections:

- Files newly *skipped* (high-res, efficient): spot-check that they are indeed
  fine → confirms bpp/quality skip rules.
- Files newly *selected* (low-res, bloated / q95+): spot-check visually after
  processing → confirms the tool now finds the actual targets.
- Tune `bpp_threshold` / `requality_threshold` from this data; defaults above
  are informed starting points, not calibrated values.

## Open parameters (defaults chosen, calibrate in verification)

| Parameter | Default | WP |
|-----------|---------|----|
| `min_savings_pct` | 5 | WP1 |
| `bpp_threshold` | 3.0 | WP2 |
| `requality_threshold` | 92 | WP2 |
| `convert_max_ratio` | 0.85 | WP3 |
| `min_jpeg_quality` | 80 | WP3 |

## Suggested session order

1. **WP1** — immediate quality protection, independent of heuristics.
2. **WP2** — precise target selection; ends with the library dry-run calibration.
3. **WP3** — PNG policy + floor; benefits from WP1's per-page plumbing.

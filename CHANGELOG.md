# Changelog

## 0.2.0-beta.10 - 2026-09-23

### Changed

- Pairing output uses distinct colored sections for the title, expiry, security
  notice, copyable URL, QR area, and PNG path.
- URL content stays plain text for reliable copying; `NO_COLOR` and non-TTY
  output remain stable and searchable.

## 0.2.0-beta.9 - 2026-09-23

### Fixed

- Restore terminal QR output and the copyable authenticated pairing URL as the
  default for `pair`, `start`, and `restart`.
- Keep PNG generation available through `--terminal=false --output PATH`.

## 0.2.0-beta.8 - 2026-09-23

### Changed

- Replace the QR encoder with a ZXing-compatible high-redundancy matrix and an
  explicit four-module quiet zone.
- Add compact, pixel-stable PNG output for explicit image-based pairing.

## 0.2.0-beta.7 - 2026-09-23

### Fixed

- Align the terminal QR quiet zone with full-cell rows so finder-pattern
  borders remain joined and camera-readable.

## 0.2.0-beta.6 - 2026-09-22

### Fixed

- Replace oversized full-block terminal QR output with a smaller renderer based
  on pure ANSI background cells and square module geometry.

## 0.2.0-beta.5 - 2026-09-22

### Fixed

- Make the default terminal QR large enough for reliable iPhone camera scanning
  while retaining explicit compact renderer options.

## 0.2.0-beta.4 - 2026-09-22

### Fixed

- `codex-remote restart` now waits for the old Runtime LaunchAgent, its exact
  process-tree snapshot, and all persisted Runtime ports to exit before
  bootstrapping the replacement.
- Failed startup rollback now uses the same bounded shutdown barrier, so a
  subsequent `codex-remote start` cannot collide with children that are still
  exiting.
- Shutdown timeout errors identify the remaining LaunchAgent, process IDs, and
  listening ports instead of surfacing only a later PostgreSQL readiness
  timeout.

### Security and distribution

- Runtime source and new archives use Apache License 2.0 with a NOTICE file;
  the immutable Beta 1 through Beta 3 archives retain their historical license.
- Release CI includes Go vulnerability scanning and all public repositories use
  protected, code-owner-reviewed main branches.

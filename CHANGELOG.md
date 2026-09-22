# Changelog

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

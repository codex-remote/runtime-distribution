# Codex Remote Runtime Distribution

This independent repository owns the Apple Silicon Runtime CLI, release
assembly, compatibility manifest, installation tests, and Homebrew metadata
update flow. It consumes versioned outputs from the independent Relay Server,
Mac Agent, and Mobile Web repositories; it does not import their source.

## User installation

The third-party Tap installation is:

```bash
brew trust --formula codex-remote/tap/codex-remote
brew install codex-remote/tap/codex-remote
codex-remote setup --workspace-root ~/work
codex-remote pair
```

Homebrew 6 requires explicit trust before loading a non-official Formula. This
security decision cannot be embedded in the Formula itself. Homebrew versions
without Tap Trust ignore the extra command.

After the Tap has been registered, upgrades and reinstalls use the short name:

```bash
brew upgrade codex-remote
brew reinstall codex-remote
```

A bare `brew install codex-remote` on a clean Mac is available only after the
Formula is accepted into an official Homebrew repository. Before that point,
users can trust and tap `codex-remote/tap` once and then use the short name.

## Command responsibilities

`brew install` installs immutable Runtime files plus `postgresql@17`. The
Runtime archive contains a pinned, non-TLS Valkey executable used only on
loopback, avoiding Homebrew's `redis`/`valkey` binary conflict. Installation
does not inspect Codex, initialize databases, allocate ports,
write credentials, or start services.

`codex-remote setup` performs those per-user operations. It validates Codex
with a real App Server stdio handshake, creates isolated data directories,
stores random credentials in macOS Keychain, selects and persists free ports,
writes user LaunchAgents, runs the first database initialization, starts the
stack, checks health, and prints the LAN address.

Existing PostgreSQL, Redis, or Valkey instances are left untouched. Codex Remote uses
its own data directory and picks another port if a preferred port is occupied.
After setup, a saved Gateway port is never silently changed by repair or
upgrade; `doctor` reports the conflicting process instead.

## Release assembly

```bash
make test vet
make assemble VERSION=0.2.0
./scripts/update-formula.sh \
  0.2.0 \
  dist/codex-remote-runtime-0.2.0-darwin-arm64.tar.gz \
  ../homebrew-tap
```

The stable release gate additionally requires Developer ID signing, Apple
notarization, complete third-party license generation, immutable component
tags, a public Release URL, clean-Mac Homebrew install/upgrade/rollback tests,
and true-device pairing acceptance.

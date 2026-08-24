# Codex Remote Runtime Distribution

This independent repository owns the Apple Silicon Runtime CLI, release
assembly, compatibility manifest, installation tests, and Homebrew metadata
update flow. It consumes versioned outputs from the independent Relay Server,
Mac Agent, and Mobile Web repositories; it does not import their source.

## Current release status

Runtime `0.2.0-beta.1` is being prepared as an unsigned public Beta based on the
local `0.2.0` Apple Silicon Homebrew acceptance milestone, including
isolated PostgreSQL 17, bundled Valkey 9.1.1, occupied default ports, all five
LaunchAgents, `doctor --json`, Mac Agent connectivity, and QR generation. It is
It is not yet downloadable: immutable tags, the GitHub prerelease asset, and
clean-Mac upgrade/rollback acceptance are still required. The Beta binary
license and generated third-party notices are release inputs. Developer ID
signing and Apple notarization are explicitly deferred to a later stable release.

The local archive is release-candidate evidence only. Do not publish a Formula
that points to a local `file://` URL or describe `0.2.0-beta.1` as available
until every Beta release gate below has passed.

## User installation after publication

The third-party Tap installation will be:

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

A bare `brew install codex-remote` on a clean Mac is a later official Homebrew
Cask milestone. The Runtime is distributed as closed-source binaries, so it is
not eligible for `homebrew/core`, whose Formulae must be open source. Before an
official Cask is accepted, users must trust and install the third-party Tap
Formula once; short-name upgrades then work on that machine.

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
make notices
make assemble VERSION=0.2.0-beta.1 CHANNEL=beta
./scripts/update-formula.sh \
  0.2.0-beta.1 \
  dist/codex-remote-runtime-0.2.0-beta.1-darwin-arm64.tar.gz \
  ../homebrew-tap
```

The Beta gate requires generated third-party licenses, an explicit public Beta
binary license, immutable component tags, a GitHub prerelease, clean-Mac
Homebrew install/upgrade/rollback tests, and true-device pairing acceptance.
The stable gate additionally requires Developer ID signing and Apple
notarization.

# Codex Remote Runtime Distribution

This independent repository owns the Apple Silicon Runtime CLI, release
assembly, compatibility manifest, installation tests, and Homebrew metadata
update flow. It consumes versioned outputs from the independent Relay Server,
Mac Agent, and Mobile Web repositories; it does not import their source.

## Current release status

Runtime `0.2.0-beta.3` replaces five visible Login Items with one
`com.codex-remote.runtime` LaunchAgent. Its Supervisor manages isolated
PostgreSQL 17, bundled Valkey 9.1.1, Relay, Mac Agent, and Gateway with dynamic ports,
`doctor --json`, Mac Agent connectivity, and QR generation. Developer ID
signing and Apple notarization remain deferred to a later stable release.

The local archive is release-candidate evidence only. Do not publish a Formula
that points to a local `file://` URL or describe `0.2.0-beta.3` as available
until every Beta release gate below has passed.

## Development Supervisor

The additive `dev-supervisor` command reuses the release Supervisor process
ownership and readiness/failure behavior for a source-worktree stack. It is
not part of the installed Runtime setup and does not touch the release
`com.codex-remote.runtime` LaunchAgent or its state directory. `mobile-web`
builds this binary as `bin/codex-remote-dev-supervisor` and runs it under the
isolated `com.codexremote.runtime.dev` LaunchAgent for `devrun crweb`.

Development Supervisor currently manages the source-built Relay, Mac Agent,
and Mobile Web Gateway while using the existing isolated development
PostgreSQL/Valkey endpoints. Vite `test`, `codex`, and `poll` entries remain
direct frontend-development launchers and are not children of this Supervisor.

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
distribution milestone. Until then, users must trust and install the
third-party Tap Formula once; short-name upgrades then work on that machine.

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
make assemble VERSION=0.2.0-beta.3 CHANNEL=beta
./scripts/update-formula.sh \
  0.2.0-beta.3 \
  dist/codex-remote-runtime-0.2.0-beta.3-darwin-arm64.tar.gz \
  ../homebrew-tap
```

The Beta gate requires the Apache-2.0 source license and NOTICE, generated
third-party licenses, immutable component tags, a GitHub prerelease, clean-Mac
Homebrew install/upgrade/rollback tests, and true-device pairing acceptance.
The stable gate additionally requires Developer ID signing and Apple
notarization.

## Open source

Source code and future Runtime artifacts are licensed under the
[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution and project
name guidance. The already published `0.2.0-beta.1` through `0.2.0-beta.3`
archives retain the historical license embedded in those immutable artifacts.

Codex Remote is an independent open-source project and is not affiliated with
or endorsed by OpenAI. Codex and OpenAI are trademarks of their respective
owners.

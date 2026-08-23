# Installation and setup troubleshooting

## Codex is not found

Discovery order is `--codex-binary`, `CODEX_BINARY`, `PATH`, the ChatGPT app,
then the Codex app. Setup stops before writing state when no executable passes
`codex --version`, `codex app-server --help`, App-bundle companion validation,
and the real `initialize` / `initialized` stdio handshake.

Recovery:

```bash
codex-remote setup --codex-binary /absolute/path/to/codex \
  --workspace-root /absolute/path/to/work
```

## A default port is occupied

On first setup Codex Remote chooses the next available port and persists it.
It never terminates the existing listener. After setup, `setup --repair` does
not silently change ports because doing so would invalidate paired Gateway
origins. Inspect the owner with `codex-remote doctor --json` and either stop the
owner yourself or explicitly purge and create a new installation.

## PostgreSQL or Valkey already runs

This is supported. Homebrew supplies the PostgreSQL executable, while the
Runtime contains a pinned Valkey executable so installing Codex Remote never
requires unlinking an existing Redis or Valkey package. Codex Remote does not
use `brew services` or the user's clusters. Its data lives below
`~/Library/Application Support/CodexRemote/Data` with separate persisted ports.

## Setup was interrupted

Run:

```bash
codex-remote setup --repair
codex-remote doctor
```

The repair path reuses the saved Gateway origin, workspace roots, database,
and Keychain credentials. It regenerates LaunchAgents and validates the current
Runtime layout.

## Upgrade or uninstall

`brew upgrade` replaces only immutable files. `codex-remote uninstall` removes
LaunchAgents but retains user data. Permanent deletion requires both explicit
flags:

```bash
codex-remote uninstall --purge --yes
brew uninstall codex-remote
```

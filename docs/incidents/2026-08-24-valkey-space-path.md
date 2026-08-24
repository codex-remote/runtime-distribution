# Valkey startup failure with the default macOS state path

## Symptom

`codex-remote setup` initialized PostgreSQL and then failed with:

```text
error: Valkey: port 63800 did not become ready
```

The Valkey LaunchAgent remained loaded and retried every five seconds. Relay,
Mac Agent, and Gateway were not loaded.

## Scope

Affected `0.2.0-beta.1` installations using the default state directory:

```text
~/Library/Application Support/CodexRemote
```

## Root cause

The Runtime generated the Valkey `dir` directive without quoting. Valkey split
the path at the space and rejected it with `wrong number of arguments`. The
failure was not caused by a port collision, Gatekeeper, architecture, or a
dynamic-library dependency.

## Fast diagnosis

1. Run `codex-remote doctor --json`.
2. Confirm PostgreSQL is loaded while Valkey is loaded but not listening.
3. Read `Logs/valkey.stderr.log` and locate the rejected `dir` directive.

## Recovery

Upgrade to `0.2.0-beta.2` or later, then run `codex-remote setup --repair` from
an external Terminal. Preserve the existing state directory and Keychain
credentials.

## Prevention

- Quote and escape every Valkey configuration value.
- Reject CR, LF, and NUL before writing configuration text.
- Test paths containing spaces, quotes, and backslashes.
- Roll back only the LaunchAgents newly loaded by a failed start attempt.
- Exercise Setup with the real default macOS state path before publishing.

## Verification evidence

Focused unit tests cover quoting, control-character rejection, and reverse
rollback order. Release validation additionally starts the bundled Valkey from
a directory containing spaces and verifies the remote Homebrew upgrade.

# Runtime restart PostgreSQL handoff failure

## Symptom

`codex-remote start` reports the Runtime ready, but an immediate
`codex-remote restart` stops the stack and ends with:

```text
error: PostgreSQL did not become ready on port 54330
```

After the failure, `codex-remote status --json` reports the Runtime stopped and
`doctor --json` reports that the LaunchAgent, Run Server, and Gateway are
unavailable.

## Affected scope

Runtime `0.2.0-beta.3` with the single `com.codex-remote.runtime` Supervisor.
User data, PostgreSQL contents, the development `devrun crweb` stack, and
unrelated PostgreSQL or Valkey services are not inherently damaged.

## Verified root cause

The restart path called `launchctl bootout` and immediately proceeded to
`bootstrap`. It did not wait for the old Supervisor's managed PostgreSQL,
Valkey, Relay, Mac Agent, and Gateway processes or their persisted ports to
fully exit. The replacement could therefore lose the PostgreSQL startup race.
The top-level readiness loop hid that handoff failure behind a 20-second
`pg_isready` timeout, then unloaded the replacement job.

In the observed incident, PostgreSQL logged a smart shutdown and successful
shutdown checkpoint at `2026-09-22 11:48:24`. `pg_controldata` reported
`Database cluster state: shut down`; the configured port had no listener, and
`postmaster.pid` was absent. This ruled out corruption, crash recovery, stale
PID state, and a foreign listener.

## Fast diagnosis

1. Run `codex-remote status --json` and `codex-remote doctor --json`.
2. Inspect the newest PostgreSQL log block and its file modification time.
3. Check the configured PostgreSQL port and `postmaster.pid` without changing
   either.
4. Use `pg_controldata` to confirm whether the cluster is cleanly shut down.
5. Treat historical errors in append-only logs as historical unless their
   timestamps match the failed restart.

## Recovery

When the cluster is cleanly shut down, no Runtime process owns the configured
ports, and `postmaster.pid` is absent, run `codex-remote start`. Do not delete or
reinitialize the PostgreSQL data directory. Upgrade to `0.2.0-beta.4` before
depending on synchronous restart behavior.

## Prevention and verification

Beta 4 snapshots the exact LaunchAgent process tree before bootout and waits
for that job, every captured PID, and all five configured Runtime ports to
disappear. The same barrier runs during failed-start rollback. Unit tests cover
delayed process/port exit, cancellation, detailed timeouts, launchd PID parsing,
and descendant PID discovery. Release acceptance must exercise real
start/restart cycles with persisted data and verify `doctor --json` afterward.

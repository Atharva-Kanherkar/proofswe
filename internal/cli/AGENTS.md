# internal/cli — commands, hook entrypoints, kill-switch

[↑ Root AGENTS.md](../../AGENTS.md)

**Single responsibility:** implement the subcommands
(`enable`/`disable`/`off`/`on`/`status`/`stats`/`resolve`) and the **hook
entrypoints** Claude Code / Codex spawn. This is the orchestrator: wire reader +
adapters + sinks; own the snapshot/resolve lifecycle and the `proofswe stats` table.

**Dependency direction:** `cli → core`, `cli → reader`, `cli → adapter/*`. Called
only by `cmd/proofswe`.

## Owns

- Hook entrypoint handlers; the soft **kill-switch** check; the adapter registry
  wiring; the loud `SessionStart` notice.
- **Snapshot phase** (issue #8): pending record of agent-produced lines (salted
  hashes + metadata) at `SessionEnd`/`Stop`.
- **Resolve phase** (issue #9): keeprate + committed computation, triggered on the
  next `SessionStart`.
- **`stats`** (issue #10): the per-model keeprate/committed table.
- `%w`-wrapping of `core`/adapter errors with context; handled once near the top.

## Invariants that bite here (this is where they are enforced)

- **Kill-switch is the FIRST action of every hook entrypoint:**
  `~/.proofswe/config: enabled=false` OR `PROOFSWE_OFF=1` → **exit 0 in
  microseconds, zero capture.** Gate the disabled path under ~10 ms, overall
  < 50 ms p95 (hyperfine). Also honor per-repo `.proofswe-ignore` and `DO_NOT_TRACK=1`.
- **No network in the capture path** — none, anywhere in these handlers.
- **Loud by default:** `SessionStart` prints the one-line observe/disable notice
  before any capture.
- **Hashes-only, salted, local** by default. Do not store raw code content — the
  default consent tier is an OPEN decision; don't resolve it silently.
- Append-only event log (`O_APPEND`); crash-safe cursor/config writes (temp +
  `fsync` + rename). Logging via `log/slog` with structural redaction.

## Links

- [CAPTURE.md §3](../../docs/CAPTURE.md) (capture sequence), [§4](../../docs/CAPTURE.md) (snapshot/resolve), [§7](../../docs/CAPTURE.md) (kill-switch / ethics); [METHODOLOGY.md](../../docs/METHODOLOGY.md) (keeprate definitions)
- Issues: [#7 hooks + kill-switch](https://github.com/Atharva-Kanherkar/proofswe/issues/7), [#8 snapshot](https://github.com/Atharva-Kanherkar/proofswe/issues/8), [#9 resolve](https://github.com/Atharva-Kanherkar/proofswe/issues/9), [#10 stats](https://github.com/Atharva-Kanherkar/proofswe/issues/10)

# internal/adapter/codex — Codex adapter

[↑ Root AGENTS.md](../../../AGENTS.md)

**Single responsibility:** parse Codex rollouts at
`~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` (plus `session_index.jsonl`) into
`NormalizedEvent`s, and register / unregister hooks in `~/.codex/config.toml`.

**Dependency direction:** `codex → core` (+ `reader`). Adapters **never import each
other**; the core never imports this package.

## Owns

- Raw per-harness structs for the rollout format + `session_index.jsonl`, with the
  same two-pass `UnmarshalJSON` → `core.Unknown` catch-all pattern.
- A parse-don't-validate constructor into the shared `NormalizedEvent`.
- Idempotent, **sentinel-tagged** TOML hook registration. **Use a CST editor
  (`creachadair/tomledit`)** — struct `Marshal`/`Unmarshal` does NOT round-trip user
  TOML (drops comments, reorders). Wrap entries in a tagged block so `enable` is a
  no-op if present and uninstall deletes exactly that block.

## Invariants / specifics that bite here

- **Codex has no `SessionEnd`.** It exposes `SessionStart` + per-turn `Stop`. So
  **resolution fires on the NEXT `SessionStart`** (see the snapshot→pending→resolved
  lifecycle), never on a reliable end signal.
- **Register hooks at user level only** — Codex forbids machine-local telemetry
  hooks in *project* config.
- Parse-don't-validate; lenient fields; local-only; hashes-only by default.
- Kill-switch check lives in the cli hook entrypoint, ahead of this adapter's work.

## Links

- [CAPTURE.md §5](../../../docs/CAPTURE.md) (harness landscape), [§4](../../../docs/CAPTURE.md) (snapshot/resolve lifecycle), [§7](../../../docs/CAPTURE.md) (enable/disable)
- Issue: [#6 adapter/codex: parse ~/.codex rollouts](https://github.com/Atharva-Kanherkar/proofswe/issues/6)

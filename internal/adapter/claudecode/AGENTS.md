# internal/adapter/claudecode — Claude Code adapter

[↑ Root AGENTS.md](../../../AGENTS.md)

**Single responsibility:** parse Claude Code transcripts at
`~/.claude/projects/<slug>/<session-id>.jsonl` into `NormalizedEvent`s, and register
/ unregister the two lifecycle hooks in `~/.claude/settings.json`.

**Dependency direction:** `claudecode → core` (+ `reader`). Adapters **never import
each other** and the core never imports this package.

## Owns

- Raw per-harness structs matching Claude Code's JSONL quirks, with a two-pass
  `UnmarshalJSON` (`{"type"}` probe → concrete variant; unknown → `core.Unknown`).
- A constructor that **parses** raw records into the shared `NormalizedEvent` —
  parse-don't-validate, exactly once at the boundary.
- Idempotent, **sentinel-tagged** hook registration in `~/.claude/settings.json`
  (surgical key insert/delete via stdlib `encoding/json`; `disable` removes only
  proofswe's tagged entries). `Marshal` reorders keys, so do surgical edits.
- Hooks registered: `SessionStart` + `SessionEnd`/`Stop` only — **never per-tool hooks**.

## Invariants that bite here

- **Parse, don't validate;** keep raw types separate from `NormalizedEvent`.
- **Be lenient:** stdlib `encoding/json` ignores unknown keys — never
  `DisallowUnknownFields()`. Route unknown discriminators to `core.Unknown`.
- The `SessionStart` hook is where the **loud notice** prints; the
  **kill-switch check belongs in `internal/cli`'s hook entrypoint**, before any
  capture work here runs.
- Local-only, hashes-only: this adapter emits metadata + salted line-hashes, never
  raw code, by default.

## Links

- [CAPTURE.md §5](../../../docs/CAPTURE.md) (harness landscape), [§7](../../../docs/CAPTURE.md) (enable/disable), [§6.3](../../../docs/CAPTURE.md) (modeling)
- Issue: [#5 adapter/claudecode: parse ~/.claude transcripts](https://github.com/Atharva-Kanherkar/proofswe/issues/5)

@AGENTS.md

# Claude Code — notes specific to this harness

The file above is the **single source of truth**, shared with Codex and other
agents. Everything you need is there; the notes below are Claude-Code-only.

- **Read order:** start at the imported [AGENTS.md](AGENTS.md), then the nested
  `AGENTS.md` in the package you are editing (see the package map table), then the
  cited [`docs/CAPTURE.md`](docs/CAPTURE.md) / [`docs/METHODOLOGY.md`](docs/METHODOLOGY.md)
  section. Prefer just-in-time retrieval over loading everything.
- **Nested files load lazily.** Subdirectory `AGENTS.md`/`CLAUDE.md` files are read
  only when you open a file in that directory — not at launch. After `/compact`,
  only this **project-root** file is re-injected; anything that must always be in
  context belongs here, not in a nested file.
- **CLAUDE.md is context, not enforcement.** Rules that must run deterministically
  (e.g. before every commit) belong in a hook, not here.
- **The five Invariants are hard rules**, not preferences. Before touching a hook
  path, the reader, or the capture/storage code, re-read the Invariants section and
  confirm your change preserves kill-switch-first, local-only, hashes-only,
  no-mmap, and the < 50 ms budget.
- **Do not silently resolve OPEN decisions** ([CAPTURE.md §10](docs/CAPTURE.md)):
  proxy tier, default consent tier, faster-JSON default, `os/exec` git vs `go-git`,
  scope metric. Surface them; don't pick one quietly.

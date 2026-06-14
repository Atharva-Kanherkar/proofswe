# snapshot-phase - Test Contract

Implements [CAPTURE.md §4](../docs/CAPTURE.md) phase 1 (issue #7→#8): on session end,
write a privacy-safe **pending record** of what the agent produced.

## Functional Behavior
- `SessionEnd` (Claude Code) / `Stop` (Codex) hook entrypoints read the harness JSON
  on stdin (`session_id`, `transcript_path`, `cwd`) and snapshot the session.
- The kill-switch (`PROOFSWE_OFF`/`DO_NOT_TRACK`/`enabled=false`/`.proofswe-ignore`)
  is checked before any snapshot work, exactly as for `SessionStart`.
- Produced lines = working-tree additions vs `HEAD` (tracked) plus full contents of
  untracked, non-ignored files, via `os/exec` `git` (the documented default). Each
  added line is stored as `Hasher.StringHash(repo_relative_path)` +
  `Hasher.StringHash(trimmed_line)` using the shared per-install salted HMAC
  (`internal/hashing`) — the same hashing the resolve phase (#9) will use. Blank
  (whitespace-only) lines are skipped.
- Session metadata (model, turn count = user prompts, tool-call count, duration) is
  pulled from the transcript via the matching adapter's `ParseFile`; the Codex
  hook's `model` field is the fallback when no transcript path is supplied.
- The record (`schema_version`, harness, session id, repo path, captured-at, model,
  counts, line hashes) is written to `~/.proofswe/pending/<session_id>.json`
  atomically and is idempotent (one file per session, overwritten).
- Non-git cwd → no record, exit 0. Snapshot is best-effort: any failure is logged to
  stderr and never disrupts the session.

## Unit Tests
- `TestSnapshotHashesMatchKnownDiffAndStoreNoRawCode` — known diff (tracked edit +
  untracked file) yields exactly the expected salted hashes; no raw line content
  appears in the serialized record.
- `TestSnapshotNonGitCwdWritesNothing` — non-git cwd writes no record, no error.
- `TestSnapshotMetadataFromAdapter` — model/turns/tool-calls populated from both a
  Claude Code and a Codex transcript fixture.
- `TestSnapshotIsIdempotent` — re-running for one session id overwrites in place.
- `TestHookStopTriggersSnapshotFromStdin` — the `hook codex Stop` entrypoint reads
  stdin and writes a record.

## Integration / Smoke
- `go test ./...` exercises real `git` against temporary repositories.

## Out of scope (deferred)
- Resolve/keeprate computation is #9 (reads these pending records on next
  `SessionStart`).
- Committed-during-session changes need a session-start baseline (scope-metric
  open decision); v0 captures working-tree + untracked additions.
- Migrating the adapters' duplicate salted-hasher onto `internal/hashing`.

# codex/resolve-phase-issue-9 - Test Contract

## Functional Behavior

Implements issue #9: the resolve phase matures pending snapshot records into
schema-versioned datapoints.

- `proofswe resolve` scans `~/.proofswe/pending/*.json` and resolves records older
  than the maturity window. The default maturity window is 24h.
- A configurable maturity window is available for tests and command-line use.
- Hook `SessionStart` prints the local notice after the kill-switch check, then
  runs the same best-effort resolve path.
- A pending record younger than the maturity window is left untouched and emits
  no datapoint.
- A matured pending record re-reads only the original repo-relative file paths
  represented by the stored `path_hash` values. A line moved to a different file
  is conservatively counted as not survived.
- Survival counts a stored salted `line_hash` as survived when the same
  normalized line is present in the working tree file at the same path or in
  `HEAD` at the same path.
- `committed` is true when any pending line hash is present in `HEAD` at the same
  repo-relative path.
- Resolved datapoints append to `~/.proofswe/data.jsonl` with
  `schema_version`, `event_type`, `ts`, `model`, `harness`, `repo_hash`, `turns`,
  `tool_calls`, `lines_added`, `lines_survived`, `keeprate`, `committed`, and
  `resolved_after_h`.
- `keeprate` is exact `lines_survived / lines_added`; zero-line records emit
  `keeprate == 0` without division by zero.
- A successfully resolved pending record is removed. Failed records are left in
  place so a later run can retry.
- No raw code content is written to pending records or resolved datapoints.

## Unit Tests

- `TestResolveKeeprateAndCommittedFromWorkingTreeAndHEAD` - a fixture with known
  pending hashes resolves to exact `K/N`, correct `committed`, and appends one
  datapoint.
- `TestResolveRespectsMaturityWindow` - an injectable clock leaves immature
  records untouched and emits nothing.
- `TestResolveRemovesPendingAfterDatapoint` - successful resolve removes the
  pending record after appending the datapoint.
- `TestResolveRenamedLineDoesNotSurvive` - moving the same raw line to a new path
  is not counted as survived.
- `TestResolveZeroLinePendingRecord` - zero-line records resolve without divide by
  zero and report zero counts.
- `TestResolveSkipsInvalidPendingRecord` - malformed or unreadable pending JSON
  does not remove the record and returns a useful error.
- `TestResolveCommandUsesSamePath` - `proofswe resolve` calls the same resolver
  and honors the maturity flag.

## Integration / Functional Tests

- `go test ./internal/cli` passes with git-backed fixtures.
- `go test ./...` passes.

## Smoke Tests

- `go run ./cmd/proofswe -- resolve --maturity=0s` exits successfully in an
  isolated `HOME` with no pending records.
- `go run ./cmd/proofswe -- help` includes `resolve`.

## E2E Tests

N/A - issue #9 is a local CLI lifecycle phase. The hook-path unit test covers the
user-visible entrypoint without requiring Claude Code or Codex to be installed.

## Manual / cURL Tests

N/A - no HTTP surface. Manual reviewer can inspect `~/.proofswe/data.jsonl` after
running `proofswe resolve --maturity=0s` against a temporary pending fixture.

# codex/expanded-transcript-signals - Test Contract

## Functional Behavior
- `proofswe score --json <transcript>` includes a versioned extracted-signal block with evidence offsets for deterministic signals.
- Verification, landing, termination, human correction, acceptance, rework, scope, and verified-after-edit signals are computed from the transcript only.
- Landing quality distinguishes no landing command, attempted-but-failed, succeeded, and PR-link evidence.
- Rework/thrash counts repeated edits to the same file when the transcript exposes paths.
- Scope/ambition includes files touched, test files touched, edit count, and diff hunk count when visible in tool outputs.
- Existing score axes remain backward-compatible: deterministic success scoring still works, and pending success remains excluded.
- Consent/rating guidance documents richer transcript tiers and tiny end-of-session ratings without changing default capture privacy.
- Verification and landing commands only match real execution tools (`Bash`, shell, `exec_command`, `run_command`) and only the executed command field, never prose/file content inside non-exec tools.
- Missing tool results only imply abandoned termination when the final meaningful event is the unanswered tool call; older orphaned calls leave termination unknown or let later evidence decide.

## Unit Tests
- `TestExtractTranscriptSignals_VerifiedAfterEditAndScope` - edits followed by passing tests produce verified-after-edit and scope signals.
- `TestExtractTranscriptSignals_NonExecToolInputDoesNotVerify` - `Write`/planning tool inputs mentioning tests or git commands do not create verification/landing evidence.
- `TestSuccessFacts_EarlierPendingToolCallDoesNotAbandonPRLink` - a stale unmatched tool call before PR evidence does not mark the session abandoned.
- `TestExtractTranscriptSignals_HumanCorrectionAndAcceptance` - user turns classify correction pressure and acceptance.
- `TestExtractTranscriptSignals_ReworkAndLandingQuality` - failed and successful landing attempts are distinguished, repeated file edits count as rework.
- Existing `successFactsFromTranscript` tests continue to pass through the new extractor path.
- `internal/score` tests continue to prove pending and deterministic success behavior.

## Integration / Functional Tests
- `go test ./internal/cli ./internal/score` passes.
- `go test ./...` passes.
- `go run ./cmd/proofswe -- score --json internal/cli/testdata/score/verified.jsonl` shows extracted signals in JSON.

## Smoke Tests
- `go vet ./...` passes.
- `git diff --check` passes.
- `gofumpt -l .` prints no files when available.

## E2E Tests
- N/A - this is a CLI scoring change with package and command-level coverage.

## Manual / cURL Tests
- Inspect the generated score JSON for `signals.extracted.version`, `signals.extracted.evidence`, `signals.extracted.landing_quality`, and `signals.extracted.scope`.

# codex/session-utility-scoring - Test Contract

## Functional Behavior

- `score.Score` should report transcript utility, not a plain average of execution axes.
- The headline score should be a sigmoid-calibrated probability-style utility score in `[0,100]`.
- Deterministic transcript signals should dominate the utility score:
  - verification passed/failed
  - landed commit/PR action
  - clean or abandoned termination
  - tool error rate
  - human turn burden
  - observed edit/scope signal
- Judge output should be treated as a bounded supplement, not the scoring engine.
  - Judge success may only nudge deterministic utility within a fixed cap.
  - Judge labels should remain visible as evidence.
- A transcript with explicit failure/friction should score below a superficially clean transcript even when both have activity.
- Existing execution axes should remain available as evidence/profile axes.
- JSON output should expose the new headline score and utility details without breaking access to existing axes/signals.
- Text and HTML output should label the headline as session utility rather than execution score.

## Unit Tests

- `internal/score.TestScore_ExecutionAxesRemainAsEvidence` - existing efficiency/autonomy/friction axes still compute as before.
- `internal/score.TestScore_UtilityRewardsVerifiedAcceptedWork` - passed verification, landed action, clean end, and edits produce high utility.
- `internal/score.TestScore_UtilityPenalizesFailureAndFriction` - failed verification, abandonment, tool errors, and many turns produce low utility.
- `internal/score.TestScore_JudgeIsBoundedNudge` - judge success cannot move utility beyond the configured nudge cap.
- `internal/score.TestScore_UtilityUsesSigmoidShape` - stronger evidence monotonically increases the sigmoid utility score.
- `internal/cli.TestScoreCommand_Fixture` - JSON output contains the new `utility` block and valid score.
- `internal/cli.TestScoreCommand_TextOutput` - text output includes `session utility`.
- `internal/cli.TestScoreCommand_Judge` - judge affects the utility score through the bounded nudge path.

## Integration / Functional Tests

- `go test ./internal/score ./internal/cli`
- `go test ./...`

## Smoke Tests

- `go test ./...` passes.

## E2E Tests

N/A - this change affects scoring math and CLI rendering only.

## Manual / cURL Tests

- Run `go run ./cmd/proofswe score internal/cli/testdata/score/session.jsonl`.
- Run `go run ./cmd/proofswe score --json internal/cli/testdata/score/session.jsonl`.

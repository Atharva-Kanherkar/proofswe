# codex/public-corpus-leaderboard — Test Contract

## Functional Behavior
- `GET /v1/leaderboard` returns JSON for the static website to render public ProofSWE corpus activity.
- The response includes recent published submissions with task ID, submission ID, harness, model, contributor, scorecard summary, GitHub corpus path, GitHub PR URL, GitHub commit SHA, and timestamps.
- The response includes model leaderboard rows grouped by harness and model with submission count, average score, best score, latest score, and latest published timestamp.
- Only published submissions with scorecards and GitHub corpus mappings are included by default.
- Query parameters support `limit`, `harness`, and `model`; invalid limits fail with a 400.
- The endpoint never returns raw prompts, assistant transcript text, tool outputs, code patches, or raw task JSON.

## Unit Tests
- `TestMemorySubmissionStore_ListsPublishedCorpusFeedAndLeaderboard` — published mapped submissions become recent rows and model aggregate rows.
- `TestSubmissionHandler_LeaderboardEndpoint` — HTTP endpoint returns the public response shape and supports filters.
- `TestSubmissionHandler_LeaderboardRejectsInvalidLimit` — invalid `limit` returns 400.

## Integration / Functional Tests
- `go test ./internal/cli -run 'Test.*Leaderboard|TestMemorySubmissionStore_ListsPublishedCorpusFeedAndLeaderboard'` must pass.
- Existing serve and submission tests must continue passing.

## Smoke Tests
- `go test ./internal/cli` must pass.
- `go test ./...` must pass.

## E2E Tests
N/A — this PR exposes the backend JSON contract for the static site; browser rendering can be added in the web app PR.

## Manual / cURL Tests
- `curl http://localhost:8080/v1/leaderboard`
- `curl 'http://localhost:8080/v1/leaderboard?harness=codex&limit=10'`

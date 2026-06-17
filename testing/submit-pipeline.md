# Submit Pipeline — Test Contract

## Functional Behavior
- `proofswe submit <transcript>` builds the same scrubbed reproducible task as `contribute`.
- `submit` sends JSON to a server endpoint with no contributor-side judge API key.
- `proofswe serve` exposes the server endpoint that runs the judge with server-side credentials.
- The hosted endpoint works by default and is configurable with `--endpoint` or `PROOFSWE_API_URL`.
- The request includes the task payload and enough deterministic context for the server to judge.
- The response prints an official scorecard in text by default and JSON with `--json`.
- Non-reproducible tasks are refused unless `--force` is set.
- Network errors and non-2xx responses return actionable errors.

## Unit Tests
- `TestSubmitCommand_PostsTaskAndPrintsScorecard` — posts a corpus task and prints the returned score.
- `TestSubmitCommand_JSON` — emits the server response JSON unchanged enough for automation.
- `TestSubmitCommand_UsesEnvEndpointAndToken` — accepts staging endpoint/auth overrides from env.
- `TestSubmitCommand_RejectsNonReproducibleWithoutForce` — keeps corpus quality gate.
- `TestSubmissionHandler_RunsServerJudgeAndScores` — server endpoint runs judge and returns scorecard.

## Integration / Functional Tests
- `go test ./internal/cli ./internal/corpus ./internal/judge`
- `go test ./...`

## Smoke Tests
- `go build ./cmd/proofswe`
- `node npm/proofswe/bin/proofswe.js version` works when pointed at a local built binary package.

## E2E Tests
- N/A for this PR. A hosted API environment is required to run a real server judge.

## Manual / cURL Tests
- Start `proofswe serve --judge-provider=openai` with server-side `OPENAI_API_KEY`.
- Run `proofswe submit --endpoint=http://127.0.0.1:<port>/v1/submissions --force <transcript>`.
- Confirm the CLI prints server score status and scorecard.

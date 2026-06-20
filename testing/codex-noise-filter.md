# codex/noise-filter — Test Contract

## Functional Behavior

- The hosted LLM judge classifies each submitted transcript as either a software-engineering task or noise in the same blinded call used to assess outcome.
- Software-engineering work includes implementation and non-code work that directly advances a concrete software project, such as debugging, architecture, review, testing, CI, deployment, and requirements work.
- Pure general Q&A, open-ended ideation such as “what should I build?”, and conversations that do not produce or advance a concrete software artifact are classified as noise.
- A noise verdict is terminal: the submission receives no scorecard, is not published to the public corpus, and cannot appear in leaderboard rows or model aggregates.
- A software-engineering verdict follows the existing score, publish, and leaderboard path unchanged.
- Legacy judge responses that omit the classification field remain scorable, preventing rollout failures while older/in-flight responses exist.

## Unit Tests

- `TestParseVerdict_ParsesTaskClassification` — accepts and normalizes SWE/noise classifications and rejects unknown values.
- `TestParseVerdict_LegacyResponseDefaultsToSWE` — keeps an omitted classification backward-compatible.
- `TestBuildPrompt_AsksForNoiseClassification` — captures the intended boundary, including code-less but concrete SWE work.
- `TestSubmissionWorker_FiltersNoise` — records a terminal filtered submission with no scorecard and does not invoke the publisher.
- Existing judge, scoring, worker, store, and leaderboard tests continue to pass.

## Integration / Functional Tests

- The in-memory submission pipeline processes an SWE verdict through scoring/publication as before.
- The in-memory submission pipeline processes a noise verdict without scoring or publication.
- The PostgreSQL store can persist the filtered terminal state using the existing status/error metadata columns.

## Smoke Tests

- `go test ./internal/judge ./internal/cli`
- `go test ./...`

## E2E Tests

N/A — hosted provider calls and production corpus publication are intentionally not exercised by the offline suite.

## Manual / cURL Tests

N/A — the worker integration tests exercise the server lifecycle without external credentials or corpus mutations.

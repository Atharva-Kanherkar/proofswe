# codex/issue-53-async-judge-db — Test Contract

## Functional Behavior
- `POST /v1/submissions` validates the submitted `corpus.Task`, stores or reuses its `task_id`, creates a unique submission, enqueues exactly one judge job, and returns `202` with `status: queued` quickly.
- `GET /v1/submissions/{submission_id}` returns the stored submission status and includes the scorecard once judging completes.
- `/health` remains public.
- Submit token enforcement is opt-in via `PROOFSWE_REQUIRE_SUBMIT_TOKEN=true`; when false or unset, public donation submissions are accepted without a token.
- A worker claims queued jobs durably, marks them judging, runs the configured server judge, writes a judge run, stores the scorecard, marks the submission judged, and retries transient failures without losing jobs.
- Duplicate task submissions create one task row/key while preserving separate submission records.
- PostgreSQL migrations define `tasks`, `submissions`, `judge_jobs`, and `judge_runs` with the columns listed in issue #53.

## Unit Tests
- Handler tests cover queued submit, polling before/after worker completion, optional token enforcement, invalid task rejection, duplicate task dedupe, and no inline judge call during POST.
- Store tests cover enqueue, claim, success completion, failure retry, permanent failure, and task dedupe.

## Integration / Functional Tests
- In-memory server tests exercise the HTTP handler and worker together without external network or real judge APIs.
- PostgreSQL SQL/migration code must compile; live Postgres execution is not required in default CI.

## Smoke Tests
- `go test ./...` passes.
- `go build ./cmd/proofswe` passes.

## E2E Tests
- N/A — this PR changes the server-side queue boundary; production DB provisioning/deploy verification happens after merge.

## Manual / cURL Tests
- With the server running and `DATABASE_URL` set, `POST /v1/submissions` should return `202` and a `submission_id`.
- `GET /v1/submissions/{submission_id}` should move from `queued`/`judging` to `judged` with a scorecard after a worker processes the job.

# codex/issue-54-github-corpus-publish — Test Contract

## Functional Behavior
- After a submission is judged, the server can publish the corpus-safe task JSON to `Atharva-Kanherkar/proofswe-corpus`.
- Corpus path is deterministic: `tasks/sha256/<first2>/<full_sha256>.json`.
- Publisher branch is `proofswe/task/<short_task_id>` and PR title is `Add proofswe task <short_task_id>`.
- `tasks` stores `github_path`, `github_pr_url`, and `github_commit_sha`; `GET /v1/submissions/{id}` returns mapping fields when available.
- Duplicate `task_id` submissions reuse existing GitHub mapping instead of creating another file/PR.
- Publishing failure does not erase the saved scorecard, and transient failures can retry without re-running the judge.

## Unit Tests
- Path/branch/title helpers produce canonical values.
- Fake publisher tests cover create, duplicate mapping reuse, conflict/already-published behavior, and retry after publish failure.
- Handler polling includes GitHub mapping fields when stored.

## Integration / Functional Tests
- In-memory store + fake publisher exercise judge-to-publish flow without network.
- Postgres migration/query code compiles; live GitHub/Postgres integration is not required in default CI.

## Smoke Tests
- `go test ./...` passes.
- `go build ./cmd/proofswe` passes.

## E2E Tests
- N/A — live GitHub corpus publish should be smoke-tested after merge with a staging token or test corpus repo.

## Manual / cURL Tests
- Submit a reproducible task, poll `GET /v1/submissions/{id}`, and verify the response eventually includes `github_path` and `github_pr_url` when publishing is configured.

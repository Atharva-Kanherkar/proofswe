# codex/issue-54-github-corpus-publish — Test Contract

## Functional Behavior
- Submissions must use a server-verifiable task ID derived from repo URL, base commit, and first prompt.
- Publishing must be recoverable after a crash between judged and published.
- Existing corpus mappings may be reused only when the stored task body exactly matches the submitted task.
- GitHub 403 publish failures are transient, not permanent.
- GitHub publishing must use the corpus repository default branch unless explicitly configured.
- CLI polling must continue through `publishing` but must not hang forever on a judged-only server.

## Unit Tests
- Memory store rejects mutated task content under a fixed task ID.
- Memory store claims judged jobs for publish recovery.
- Publish failure classification treats 403 as retryable.
- GitHub publisher test covers real HTTP request flow and default-branch selection.

## Integration / Functional Tests
- Submission worker publishes after judging and reuses mappings for identical tasks.
- Publish retry preserves the scorecard and does not re-run judging.
- CLI submit prints a judged scorecard without waiting forever when no publish phase is present.

## Smoke Tests
- `go test ./internal/cli`
- `go test ./...`
- `go build ./cmd/proofswe`

## E2E Tests
N/A — this change is queue/publisher backend hardening with unit and functional coverage.

## Manual / cURL Tests
N/A — GitHub API behavior is exercised with a fake HTTP client in tests.

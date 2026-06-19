# codex/bulk-upload-repo-menu — Test Contract

## Functional Behavior
- `proofswe upload` discovers supported Claude Code and Codex transcripts across the user's transcript roots and groups them by detected git repository.
- In an interactive terminal, `proofswe upload` shows a numbered multiple-choice repo menu with transcript counts and lets the user select one or more repos.
- After repo selection, the user can open each selected repo's transcript menu and deselect individual transcripts before upload.
- Non-interactive usage is explicit: `--repo <path>` and/or `--all` select uploads without prompting; otherwise the command fails with usage guidance.
- Uploads are batched with a configurable `--batch-size`; each transcript is converted through the same task-building, reproducibility, agreement, endpoint, token, wait/no-wait, and force behavior as `proofswe submit`.
- The command reports submitted, skipped, and failed transcript counts without aborting the whole batch on one transcript failure.
- Existing `proofswe submit` behavior remains unchanged for single-session submission.

## Unit Tests
- `TestUploadDiscoveryGroupsTranscriptsByRepo` — discovers Claude/Codex transcripts and groups only those with repo provenance.
- `TestUploadSelectionParsesRepoAndTranscriptMenus` — parses repo selections and transcript deselections from interactive input.
- `TestUploadCommandNonInteractiveRequiresSelection` — refuses non-interactive bulk upload without `--repo` or `--all`.
- `TestUploadCommandSubmitsSelectedTranscriptsInBatches` — posts selected transcripts in batch order and reports counts.

## Integration / Functional Tests
- `go test ./internal/cli -run TestUpload` must pass.
- Existing submit tests must pass to confirm no single-submit regression.

## Smoke Tests
- `go test ./...` must pass.
- `go build ./...` must pass.

## E2E Tests
N/A — this is CLI behavior covered by Go tests with fake transcript homes and HTTP server.

## Manual / cURL Tests
- In a real shell, `proofswe upload --dry-run` should show grouped repos and transcript counts without posting.
- `proofswe upload --repo <repo> --dry-run` should select only transcripts detected for that repo.

# codex/issue-55-npm-submit-ux — Test Contract

## Functional Behavior
- `proofswe submit` with no transcript path auto-detects the latest supported Claude Code or Codex transcript.
- `proofswe submit <path>` remains supported.
- `--no-wait` disables polling; `--json` stays machine-readable.
- The npm root wrapper resolves the matching native optional package for every supported platform.
- GitHub tag releases build all native npm package binaries, publish native packages first, then publish the root package with provenance.
- The repo includes installable agent assets so a coding agent can invoke proofswe benchmarking from chat.

## Unit Tests
- Submit command posts a task when no path is provided and a latest transcript can be discovered.
- Submit command rejects too many transcript paths.
- Wrapper smoke tests cover all supported platform package layouts.

## Integration / Functional Tests
- `go test ./internal/cli`
- `npm test`

## Smoke Tests
- `go test ./...`
- `go build ./cmd/proofswe`
- `node npm/bin/proofswe.js version` with a locally built binary.

## E2E Tests
N/A — live npm publish requires npm trusted publishing / auth and will run from tag CI.

## Manual / cURL Tests
After npm auth/trusted publishing is configured, tag a release and verify `npx proofswe version` on macOS/Linux/Windows.

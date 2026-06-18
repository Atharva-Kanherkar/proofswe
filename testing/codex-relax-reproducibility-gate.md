# codex/relax-reproducibility-gate — Test Contract

## Functional Behavior
- Patch-backed tasks still use the current repo `HEAD` as `repo.base_commit` and include captured working-tree code.
- Patchless historical tasks infer `repo.base_commit` from the transcript start time, so the base commit is the pre-work commit rather than the final clean-tree commit.
- Patchless tasks without a verifiable historical base remain non-reproducible.
- Public no-license / non-allowlisted repos are still allowed by product policy when the contributor explicitly accepts the corpus code-publishing agreement.
- Server-side submission validation rejects patchless tasks unless they carry enough provenance to show the base came from historical inference.
- Existing private-repo and missing-base protections remain intact.

## Unit Tests
- `TestContributeAllowsReproducibleMetadataWithoutPatch` — emits a patchless task with inferred historical base commit.
- `TestContributeRequiresAgreementForRawCodePublication` — refuses public raw-code publishing until agreement is accepted.
- `TestValidateSubmittedTaskRejectsPatchlessWithoutHistoricalBase` — rejects forged patchless server submissions with only remote/base/prompt.
- Existing `repoAllowsRawCode` and `ReproducibilityProblems` tests continue to pass.

## Integration / Functional Tests
- `go test ./...` must pass.
- `go build ./...` must pass.

## Smoke Tests
- `proofswe contribute` on a public repo with a live working-tree diff still emits `code.patch`.
- `proofswe contribute` on a clean, already-committed historical session emits an empty patch only when the base commit is inferred from transcript time.

## E2E Tests
N/A — this is CLI/server validation behavior covered by Go tests.

## Manual / cURL Tests
N/A — no live hosted API changes are exercised from this workspace.

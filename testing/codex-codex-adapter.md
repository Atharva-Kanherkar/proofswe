# codex/codex-adapter — Test Contract

## Functional Behavior
- `internal/adapter/codex` discovers Codex rollout transcripts under `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`, sorted by path.
- `session_index.jsonl` enumeration returns redacted session metadata (`id`, `thread_name`, `updated_at`) from a fixture directory without requiring rollout files to exist.
- `session_meta` records map to `core.SessionStart` with harness `codex`, session id, cwd, git branch, timestamp, and source path.
- `turn_context` records update per-turn context used by later records in the same rollout, especially turn id, cwd, and model.
- `event_msg.task_started` maps to `core.SessionStart`; `event_msg.task_complete` and other event messages do not synthesize `SessionEnd` because Codex has no reliable session end.
- `response_item.message` with role `user` maps to `core.UserPrompt` with salted hashes only.
- `response_item.message` with role `assistant` maps to `core.AssistantMessage` with salted hashes only.
- `response_item.function_call`, `custom_tool_call`, `web_search_call`, and `tool_search_call` map to `core.ToolCall` with sanitized argument metadata.
- `response_item.function_call_output`, `custom_tool_call_output`, `web_search_output`, and `tool_search_output` map to `core.ToolResult` with sanitized result metadata.
- Unknown rollout line types or response item kinds map to sanitized `core.Unknown` values rather than failing or leaking raw content.
- Capture uses `internal/reader.ReadNewLines` with byte-offset cursors and resumes without re-emitting old records.
- The adapter implements `core.SourceAdapter` and is registered in the default CLI adapter list.

## Unit Tests
- `TestGoldenFixtureSnapshot` — fixture rollout normalizes to the checked-in golden JSON.
- `TestNormalizedOutputRedactsFixtureContent` — normalized output does not contain fixture prompt, assistant, tool argument, or tool output plaintext.
- `TestSessionIndexEnumeration` — fixture `session_index.jsonl` returns expected sorted sessions.
- `TestDiscoverRollouts` — recursive rollout discovery finds only `rollout-*.jsonl` under date directories and sorts paths.
- `TestTaskCompleteDoesNotMapToSessionEnd` — Codex completion records remain unknown/no-op instead of `SessionEnd`.
- `TestResponseItemKindsMapToEventsOrUnknown` — observed response item kinds map to modeled events or sanitized unknown.
- `TestCaptureResumesFromCursor` — capture emits only newly appended rollout lines after the first pass.
- `TestMalformedLineIsSkipped` — one bad JSONL line does not stop later lines.
- `TestSanitizationDropsSensitiveContent` — in-memory sensitive markers do not appear in normalized output.

## Integration / Functional Tests
- `go test ./internal/adapter/codex ./internal/cli` verifies the adapter package and CLI registration compile together.
- `go test ./...` verifies the new adapter does not regress core, reader, Claude Code adapter, or CLI behavior.
- Constant-memory behavior is covered by reusing `internal/reader.ReadNewLines`; no new whole-file scanner or mmap path is introduced.

## Smoke Tests
- `go test ./internal/adapter/codex` passes.
- `go test ./...` passes.
- `go vet ./...` passes.

## E2E Tests
N/A — v0 CLI capture entrypoints and hook installation are downstream issues; this PR adds the parser/discovery adapter and registration only.

## Manual / cURL Tests
- `go test -run TestGoldenFixtureSnapshot ./internal/adapter/codex`
- `go test -run TestSessionIndexEnumeration ./internal/adapter/codex`
- `go test ./...`

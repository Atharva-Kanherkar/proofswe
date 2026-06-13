# Contract: issue #3 core normalized event waist

## Functional expectations

- `internal/core` defines the sealed `NormalizedEvent` interface with an unexported marker method and the variants `SessionStart`, `UserPrompt`, `AssistantMessage`, `ToolCall`, `ToolResult`, `SessionEnd`, and `Unknown`.
- Each concrete modeled variant carries schema version, source, session, model, event metadata, and metrics fields through a shared core metadata shape.
- Public identifiers use defined types rather than bare strings for `SessionId`, `ToolCallId`, `HarnessName`, and `ModelId`.
- Event JSON uses a stable discriminator field named `type`, has `schema_version = 1`, and can be decoded through a two-pass dispatcher.
- Unknown event discriminators decode without error into `Unknown{Type, Raw}` and preserve the raw payload for forward compatibility.
- `SourceAdapter` is an open interface with `Detect`, `Enable`, `Disable`, and `Capture(trigger)` returning a sequence of `NormalizedEvent`.
- `ProofsweError` is opaque to callers, exposes `Kind() ErrorKind`, supports `errors.As`, and wraps public underlying errors with `%w`.
- `go generate ./...` emits `schema/normalized-event.v1.json`; a test fails when the checked-in schema drifts from the generated schema.

## Tests and checks

- Add unit tests for event discriminator dispatch, unknown forward compatibility, source adapter shape by compile-time use, and error kind/wrapping behavior.
- Add a property test with `pgregory.net/rapid` that marshals and unmarshals every variant and checks identity with `go-cmp`, including raw-payload preservation for `Unknown`.
- Add a golden schema drift test comparing the generated schema bytes with `schema/normalized-event.v1.json`.
- Run `go generate ./...`.
- Run `go test ./...`.
- Run `go vet ./...`.
- Run `gofmt` on changed Go files.

## Manual verification

- Inspect exported API for no bare-string identifier aliases for the required identifier types.
- Inspect `internal/core` imports to ensure runtime core stays pure and does not import adapter, reader, CLI, network, or filesystem packages.
- Inspect `git diff` before staging so only issue #3 work is included.

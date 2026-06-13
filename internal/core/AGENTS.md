# internal/core — the narrow waist

[↑ Root AGENTS.md](../../AGENTS.md)

**Single responsibility:** define the one stable contract every adapter and sink
shares — `NormalizedEvent`, `SourceAdapter`, `ProofsweError`, and the defined-type
identifiers. This is the **API boundary**.

**Dependency direction:** `adapter/* → core ← cli`. `core` imports **nothing** from
adapters, the reader, or the cli. It stays pure — **no IO, no serialization side
effects, no network.**

## Owns

- `NormalizedEvent` — a **sealed interface**: an interface with an *unexported
  marker method* so only this package can add variants (Go's stand-in for a Rust
  enum; closed at compile time).
- The `Unknown{Type string; Raw json.RawMessage}` catch-all variant — load-bearing
  forward compat: old binaries survive new event types instead of erroring.
- `SourceAdapter` — an **open** interface, invoked from a `[]SourceAdapter`
  registry. The core never names a harness.
- `ProofsweError` — opaque error type exposing a `Kind() ErrorKind` accessor
  (assert behavior, not concrete type); plus sentinel `var ErrX = errors.New(...)`.
- Defined-type ids: `type SessionId string`, `type ToolCallId string`,
  `type HarnessName string`, `type ModelId string` (zero-cost mis-wire protection).
- `schema_version` constant for the waist (v1).

## Invariants / conventions that bite here

- **Adding an event variant is expensive** (every sink's `switch` must update);
  adding a sink or adapter is cheap. Keep the waist small and stable.
- Sink exhaustiveness is **lint-enforced** with `go-check-sumtype`; mark the sealed
  interface accordingly. Iota enums use `exhaustive`.
- Errors use `errors.Is`/`errors.As`/`errors.Join` (Go 1.20+). Wrap with `%w` only
  when the wrapped error is intentionally public.
- No `Decoder.DisallowUnknownFields()` semantics leak in here — lenient by design.

## Links

- [CAPTURE.md §6.3](../../docs/CAPTURE.md) (modeling events), [§6.4](../../docs/CAPTURE.md) (closed events / open adapters), [§6.5](../../docs/CAPTURE.md) (errors)
- Issue: [#3 internal/core: NormalizedEvent schema, SourceAdapter interface, error type](https://github.com/Atharva-Kanherkar/proofswe/issues/3)

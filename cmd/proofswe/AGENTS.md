# cmd/proofswe — entrypoint

[↑ Root AGENTS.md](../../AGENTS.md)

**Single responsibility:** the thin `main` shim. Parse args, build the root
context, dispatch into `internal/cli`, translate the returned `error` into an exit
code. No business logic lives here.

**Dependency direction:** `cmd/proofswe → internal/cli` only. Never import
`internal/core` or adapters directly; never let `internal/*` import this package.

## Owns

- `main.go` — `func main()` shim + `func run(ctx, args, getenv, stdin, stdout, stderr) error`.
- Version variables stamped via `-ldflags "-X main.version=$TAG"` (authoritative),
  with `runtime/debug.ReadBuildInfo()` as supplementary VCS metadata.

## Conventions that bite here

- **`os.Exit` lives ONLY in `main`.** `main()` calls `run(...) error`; everything
  else returns errors up. `os.Exit` deep in the program skips deferred cleanup,
  flushes, and the signal `stop()`.
- **Root context** from `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`;
  `defer stop()`. Thread `ctx` as the first arg downward. Use `context.Cause(ctx)`
  to identify the signal.
- **Testable `run` signature:** `run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) error` so tests inject all IO. Define/parse flags here, pass concrete config down.
- Arg parser is stdlib `flag` / `ffcli` / `kong` — **not cobra**.

## Invariant that bites here

For the hook entrypoints dispatched from here, the **kill-switch is checked first**
in `internal/cli` — but keep `main`'s own pre-dispatch work near-zero so the
< 50 ms p95 cold-start budget holds. No heavy package-level `init()`.

## Links

- [CAPTURE.md §6.6](../../docs/CAPTURE.md) (package structure), [§6.1](../../docs/CAPTURE.md) (cold-start)
- Issue: [#2 Module scaffold, CI, and release build](https://github.com/Atharva-Kanherkar/proofswe/issues/2)

# proofswe — Agent Context (AGENTS.md)

> **Single source of truth** for any coding agent (Claude Code, Codex, Cursor, …)
> working in this repo. Keep it lean and high-signal; link out for detail rather
> than inlining it. The root [`CLAUDE.md`](CLAUDE.md) imports this file verbatim.

## Mission

**proofswe** is an open benchmark for coding agents built from **real developer
sessions**, not synthetic tasks. Where SWE-bench / Aider / Terminal-Bench measure
whether a model can close tasks that already *have* an oracle, proofswe measures
whether an agent's work **survives** — kept, committed, built upon, or merged — in
ambiguous tasks that have no pre-written oracle. v0 is a Go CLI that installs as a
hook on Claude Code and Codex, captures sessions **locally** (privacy-safe
line-hashes + metadata, **on-by-default with an instant kill-switch**), and prints
`proofswe stats` (a per-model keeprate / committed table). See the
[README](README.md), [`docs/METHODOLOGY.md`](docs/METHODOLOGY.md), and
[`docs/CAPTURE.md`](docs/CAPTURE.md).

**Rust → Go pivot (2026-06):** proofswe was originally scoped in Rust. We pivoted
to **Go** for product velocity over peak performance — this is an I/O-bound
per-spawn hook CLI (it reads files and shells out; it barely computes), and Go's
`CGO_ENABLED=0` static binaries + trivial `GOOS`/`GOARCH` cross-compilation drop
the entire musl/zig cross-build machinery. The honest trade: Go pays a small fixed
runtime-init tax per spawn that Rust avoids — judged irrelevant next to OS `exec`
and the file read, and **gated empirically** (see Invariants). See
[CAPTURE.md §1](docs/CAPTURE.md) (the spine).

## Navigation

- [Architecture: the narrow waist](#architecture-the-narrow-waist)
- [Planned layout (package map)](#planned-layout-package-map)
- [Invariants (non-negotiable)](#invariants-non-negotiable)
- [Commands](#commands)
- [Conventions](#conventions)
- [Telemetry & privacy ethics](#telemetry--privacy-ethics)
- [Build order (issues #1–#11)](#build-order-issues-111)
- [References](#references)

## Architecture: the narrow waist

The whole thesis is an **hourglass**: many messy harness inputs fan in, **one
stable middle** (`NormalizedEvent`, schema v1), many possible sinks fan out. See
[CAPTURE.md §2](docs/CAPTURE.md) (architecture overview) and the locked spine in
[CAPTURE.md §1](docs/CAPTURE.md).

- **`NormalizedEvent` is a sealed interface** — an interface with an *unexported
  marker method*, so only `internal/core` can add variants. This is Go's stand-in
  for a Rust enum; the variant set is closed at compile time. Sink exhaustiveness
  is lint-enforced with [`go-check-sumtype`](https://github.com/alecthomas/go-check-sumtype).
  See [CAPTURE.md §6.3](docs/CAPTURE.md) (modeling) and [§6.4](docs/CAPTURE.md)
  (closed events, open adapters).
- **`SourceAdapter` is an open interface** invoked from a `[]SourceAdapter`
  registry — adapters/harnesses are an open set that grows without touching the
  core. The core never names a harness.
- **Parse, don't validate.** Each adapter turns loose, harness-specific JSON into
  the precise `NormalizedEvent` exactly once, at the boundary (a two-pass
  `UnmarshalJSON` dispatcher → defined-type constructor); nothing downstream
  re-parses. Unknown `"type"` discriminators route to `Unknown{Type, Raw json.RawMessage}`
  so old binaries survive new event types (forward compat). Never call
  `Decoder.DisallowUnknownFields()` on event structs.
- **Dependency direction is one-way:** `adapter/* → core ← cli`. Adapters never
  import each other; `core` stays pure (no IO/serialization). `internal/` is the
  only compiler-enforced boundary in Go. See [CAPTURE.md §6.6](docs/CAPTURE.md).

## Planned layout (package map)

A single Go module: `github.com/Atharva-Kanherkar/proofswe`. Each package has its
own nested `AGENTS.md`. Build order is strictly dependency-ordered — do not start a
downstream package before its dependencies land.

| Package | Responsibility | Nested doc | Issue | CAPTURE.md |
|---|---|---|---|---|
| `cmd/proofswe/` | Thin `main` shim: arg dispatch → `internal/cli`; `os.Exit` lives only here | [cmd/proofswe/AGENTS.md](cmd/proofswe/AGENTS.md) | [#2](https://github.com/Atharva-Kanherkar/proofswe/issues/2) | [§6.6](docs/CAPTURE.md) |
| `internal/core/` | The waist: `NormalizedEvent` (sealed iface) + `SourceAdapter` + `ProofsweError`, defined-type ids | [internal/core/AGENTS.md](internal/core/AGENTS.md) | [#3](https://github.com/Atharva-Kanherkar/proofswe/issues/3) | [§6.3](docs/CAPTURE.md), [§6.4](docs/CAPTURE.md), [§6.5](docs/CAPTURE.md) |
| `internal/reader/` | Streaming JSONL reader + byte-offset resume cursor; constant memory | [internal/reader/AGENTS.md](internal/reader/AGENTS.md) | [#4](https://github.com/Atharva-Kanherkar/proofswe/issues/4) | [§6.2](docs/CAPTURE.md) |
| `internal/adapter/claudecode/` | Parse `~/.claude/projects/*.jsonl` + hook registration in `~/.claude/settings.json` | [internal/adapter/claudecode/AGENTS.md](internal/adapter/claudecode/AGENTS.md) | [#5](https://github.com/Atharva-Kanherkar/proofswe/issues/5) | [§5](docs/CAPTURE.md), [§7](docs/CAPTURE.md) |
| `internal/adapter/codex/` | Parse `~/.codex/sessions/**/rollout-*.jsonl` + `session_index.jsonl` + user-level hook registration | [internal/adapter/codex/AGENTS.md](internal/adapter/codex/AGENTS.md) | [#6](https://github.com/Atharva-Kanherkar/proofswe/issues/6) | [§5](docs/CAPTURE.md), [§7](docs/CAPTURE.md) |
| `internal/cli/` | `enable`/`disable`/`off`/`on`/`status`/`stats`/`resolve` + hook entrypoints + kill-switch | [internal/cli/AGENTS.md](internal/cli/AGENTS.md) | [#7](https://github.com/Atharva-Kanherkar/proofswe/issues/7), [#8](https://github.com/Atharva-Kanherkar/proofswe/issues/8), [#9](https://github.com/Atharva-Kanherkar/proofswe/issues/9), [#10](https://github.com/Atharva-Kanherkar/proofswe/issues/10) | [§3](docs/CAPTURE.md), [§4](docs/CAPTURE.md), [§7](docs/CAPTURE.md) |

## Invariants (non-negotiable)

> **These five invariants are load-bearing. The "on-by-default" posture is *only*
> defensible because of them — weakening any one materially changes the privacy
> claim. Treat any change here as a design decision, not a refactor.** See
> [CAPTURE.md §7](docs/CAPTURE.md).

1. **Instant total disable (kill-switch first).** The soft kill-switch
   (`~/.proofswe/config: enabled=false` **OR** `PROOFSWE_OFF=1`) is the **first
   action of every hook**. Disabled = exit 0 in microseconds with **zero capture**.
   Gate the disabled path under ~10 ms; gate overall hook cold-start **< 50 ms p95**
   with [`hyperfine`](https://github.com/sharkdp/hyperfine).
2. **Local-only.** **No network anywhere in the capture path.** Upload is a
   separate, explicit, later consent moment — not v0.
3. **Metadata + line-hashes only by default.** Store `SHA256(path)` +
   `SHA256(normalized_line)` + adapter metadata. **No raw code content** by default.
   Hash *normalized* lines for survival matching, and **salt with a per-install
   secret salt** that never leaves the machine — unsalted hashes of low-entropy
   code lines are reversible against a public corpus.
4. **Never `mmap` a live transcript.** A session JSONL grows while you read it;
   mmap of an appended/truncated file risks **SIGBUS** (unrecoverable abort). Use
   constant-memory streaming via `bufio.Reader.ReadBytes('\n')` — **never**
   `bufio.Scanner` (its 64 KB `MaxScanTokenSize` cap aborts on a fat record).
5. **Cold-start budget < 50 ms p95**, tracked as a gated metric in CI with
   `hyperfine --shell=none`.

## Commands

Run from the module root unless noted. (Source tree not yet scaffolded — these are
the agreed commands per [issue #2](https://github.com/Atharva-Kanherkar/proofswe/issues/2).)

```sh
# Build / run
go build ./...
go run ./cmd/proofswe -- <args>

# Test
go test ./...                       # full suite
go test -short ./...                # skip the >=1GB streaming test (testing.Short)
go test -run TestX -update ./...    # regenerate golden files under testdata/
go test -fuzz=FuzzParse ./internal/adapter/...   # native fuzzing (bound -fuzztime in CI)

# Lint / format / vuln (each its own gate)
golangci-lint run                   # v2; staticcheck + go vet + exhaustive + go-check-sumtype
gofumpt -l .                        # stricter gofmt; -w to fix
go vet ./...
govulncheck ./...                   # separate CI step from golangci-lint

# Cold-start gate (Invariant 5)
hyperfine --shell=none './proofswe status'   # assert < 50ms p95

# Release (tag-triggered)
goreleaser release --clean          # build/archive/checksum/brew/scoop/nfpm
npm publish --provenance            # esbuild-style optionalDependencies wrapper, separate step
```

CI also greps the tree to assert **zero `mmap` calls** (Invariant 4).

## Conventions

Decided Go SOTA choices — encode these; do not relitigate silently.

- **Testable `main`.** `main()` is a tiny shim calling `run(ctx, args, getenv, stdin, stdout, stderr) error`;
  `os.Exit` confined to `main`. Root context from `signal.NotifyContext(…, os.Interrupt, syscall.SIGTERM)`,
  threaded as the first arg through blocking/exec calls.
- **Arg parsing.** Stdlib [`flag`](https://pkg.go.dev/flag) + manual dispatch, or
  [`peterbourgon/ff`/`ffcli`](https://github.com/peterbourgon/ff), or
  [`alecthomas/kong`](https://github.com/alecthomas/kong). **Not cobra.** Config
  precedence: flags > env > file > defaults; every setting reachable as a flag.
- **Errors.** Typed/sentinel errors + `errors.Is`/`errors.As`/`errors.Join` in
  `core`/adapters (the "thiserror in libraries" half); `fmt.Errorf("…: %w", err)`
  context-wrapping in `cli`, handled once near `main` (the "anyhow in the binary"
  half). Wrap with `%w` only when the wrapped error is intentionally public; use
  `%v` to keep it opaque. Model the core error opaque with a `Kind()` accessor
  (assert behavior, not concrete type). See [CAPTURE.md §6.5](docs/CAPTURE.md).
- **Modeling.** Sealed interface + two-pass `UnmarshalJSON` + `Unknown{Raw}`
  catch-all; lenient fields; defined-type ids (`type SessionId string`, etc.);
  raw-per-harness types then *parse* into the waist.
- **JSON.** Stdlib `encoding/json` is the always-available default. Faster decoders
  ([`goccy/go-json`](https://github.com/goccy/go-json) drop-in, or
  [`bytedance/sonic`](https://github.com/bytedance/sonic), amd64 + arm64) stay
  **opt-in behind a build tag** — never the default. (Sonic on arm64 needs go1.20+;
  state this precisely.)
- **Logging.** Single spine = stdlib [`log/slog`](https://pkg.go.dev/log/slog):
  `JSONHandler` for machine output, `TextHandler` for the terminal. Do redaction
  structurally inside a custom handler / `ReplaceAttr`, not per-call.
- **Testing.** Table-driven + `t.Run` subtests; diff with
  [`google/go-cmp`](https://pkg.go.dev/github.com/google/go-cmp/cmp) `cmp.Diff`
  (supply `cmpopts.IgnoreUnexported` — it panics on unexported fields otherwise);
  golden files under `testdata/` behind a `-update` flag; native `go test -fuzz`
  for parsers; [`pgregory.net/rapid`](https://pkg.go.dev/pgregory.net/rapid) for
  round-trip/idempotency invariants; guard the ≥1 GB streaming test with
  `testing.Short()`.
- **Build/dist.** `CGO_ENABLED=0` static, `-trimpath -ldflags="-s -w"` (no UPX);
  pinned `toolchain` directive in `go.mod` for reproducibility; version via
  `-ldflags "-X main.version=$TAG"` (authoritative) with `runtime/debug.ReadBuildInfo`
  as supplement. Git via `os/exec` git or [`go-git`](https://github.com/go-git/go-git)
  — **never** [`git2go`](https://github.com/libgit2/git2go) (CGO breaks `CGO_ENABLED=0`).
- **Layout.** Flat root + aggressive `internal/` + `cmd/<binary>/`. **No `pkg/`** —
  there is no public library to export. Single module; no `go.work`.

## Telemetry & privacy ethics

On-by-default is only ethical if it is **loud**. Notice must precede the first
captured byte, and the kill-switch must be honored deterministically.

- **Loud by default.** `SessionStart` prints a one-line notice ("observing locally
  · disable: `proofswe off`"); on-by-default *and silent* is the malware shape.
  Model: [Homebrew Analytics](https://docs.brew.sh/Analytics) (notice before
  transmission). Cautionary tale: the GitHub CLI v2.91
  [opt-out rollout](https://github.blog/changelog/2026-04-22-github-cli-opt-out-usage-telemetry/)
  shipped with only a changelog entry — adopt its opt-out *controls*, not its lack
  of consent. See [CAPTURE.md §7](docs/CAPTURE.md).
- **Dual disable + per-repo opt-out.** `PROOFSWE_OFF=1` env var + `proofswe off`
  command + a per-repo `.proofswe-ignore` marker. Honor
  [`DO_NOT_TRACK=1`](https://donottrack.sh/).
- **Consent tiers, just-in-time.** Default tier = metadata + salted line-hashes
  only. Upgrades (redacted content; OSS full-transcript) are explicit, symmetric,
  reversible (downgrade purges higher-tier local data), and never auto-re-prompted
  after decline. Default consent tier is an
  [**OPEN** decision](docs/CAPTURE.md) (CAPTURE.md §10.2) — **do not silently start
  storing code content.**
- **Redaction by allowlist, not denylist.** Never write raw code, full paths,
  commit messages, prompt/response bodies, usernames, emails, tokens, or env
  values — capture *that* an event happened and its coarse shape, never *what* was
  in it. Compare the [.NET CLI non-collection guarantees](https://learn.microsoft.com/en-us/dotnet/core/tools/telemetry).
- **Hashing discipline.** Salt every hash with a per-install secret salt; if a
  value's domain is small/predictable, **drop it** rather than hash it (unsalted
  hashes of low-entropy values are trivially reversible).
- **Store hygiene.** Event log = append-only (`O_APPEND`, whole-line-then-newline;
  recover by truncating at last newline). Cursor/config = temp-file + `fsync` +
  `os.Rename` (same filesystem). Put `schema_version`, `event_type`, `ts` on every
  record; be additive-only; consumers are tolerant readers (ignore unknown fields).

## Build order (issues #1–#11)

Strict dependency order — confirmed against live issues. Do not start a downstream
issue before its dependencies land.

1. [#1 — \[epic\] v0 the data-capture pipeline (Claude Code + Codex)](https://github.com/Atharva-Kanherkar/proofswe/issues/1)
2. [#2 — Module scaffold, CI, and release build](https://github.com/Atharva-Kanherkar/proofswe/issues/2)
3. [#3 — internal/core: NormalizedEvent schema, SourceAdapter interface, error type](https://github.com/Atharva-Kanherkar/proofswe/issues/3)
4. [#4 — Streaming JSONL reader with byte-offset resume cursor](https://github.com/Atharva-Kanherkar/proofswe/issues/4)
5. [#5 — adapter/claudecode: parse ~/.claude transcripts](https://github.com/Atharva-Kanherkar/proofswe/issues/5)
6. [#6 — adapter/codex: parse ~/.codex rollouts](https://github.com/Atharva-Kanherkar/proofswe/issues/6)
7. [#7 — Hook install/enable/disable + instant kill-switch](https://github.com/Atharva-Kanherkar/proofswe/issues/7)
8. [#8 — Snapshot phase: pending record of agent-produced lines](https://github.com/Atharva-Kanherkar/proofswe/issues/8)
9. [#9 — Resolve phase: keeprate + committed computation](https://github.com/Atharva-Kanherkar/proofswe/issues/9)
10. [#10 — proofswe stats: per-model keeprate/committed table](https://github.com/Atharva-Kanherkar/proofswe/issues/10)
11. [#11 — Release & npm distribution (npx proofswe)](https://github.com/Atharva-Kanherkar/proofswe/issues/11)

## References

Only verified sources, grouped by topic.

**Go CLI engineering**
- [Organizing a Go module (go.dev)](https://go.dev/doc/modules/layout) ·
  [project-layout critique #117](https://github.com/golang-standards/project-layout/issues/117) ·
  [No-nonsense Go package layout](https://laurentsv.com/blog/2024/10/19/no-nonsense-go-package-layout.html)
- [How I write HTTP services after 13 years (Mat Ryer)](https://grafana.com/blog/how-i-write-http-services-in-go-after-13-years/) ·
  [Go best practices, six years in (Bourgon)](https://peter.bourgon.org/go-best-practices-2016/) ·
  [Go for Industrial Programming](https://peter.bourgon.org/go-for-industrial-programming/) ·
  [Practical Go (Cheney)](https://dave.cheney.net/practical-go)
- [os/signal NotifyContext](https://pkg.go.dev/os/signal#NotifyContext) ·
  [Cancellation with NotifyContext (henvic)](https://henvic.dev/posts/signal-notify-context/)
- [peterbourgon/ff](https://github.com/peterbourgon/ff) ·
  [ffcli](https://pkg.go.dev/github.com/peterbourgon/ff/v3/ffcli) ·
  [alecthomas/kong](https://github.com/alecthomas/kong)
- [Minimal Version Selection (Russ Cox)](https://research.swtch.com/vgo-mvs) ·
  [Go Modules Reference](https://go.dev/ref/mod) ·
  [Using Go Modules](https://go.dev/blog/using-go-modules)

**Errors & modeling**
- [Working with Errors in Go 1.13](https://go.dev/blog/go1.13-errors) ·
  [errors.Join / multi-%w #53435](https://github.com/golang/go/issues/53435) ·
  [io/fs PathError](https://pkg.go.dev/io/fs#PathError) ·
  [Handle errors gracefully (Cheney)](https://dave.cheney.net/2016/04/27/dont-just-check-errors-handle-them-gracefully)
- [go-check-sumtype](https://github.com/alecthomas/go-check-sumtype) ·
  [exhaustive](https://pkg.go.dev/github.com/nishanths/exhaustive)

**Testing, release & supply chain**
- [google/go-cmp](https://pkg.go.dev/github.com/google/go-cmp/cmp) ·
  [pgregory.net/rapid](https://pkg.go.dev/pgregory.net/rapid) ·
  [Go Fuzzing](https://go.dev/doc/security/fuzz/)
- [golangci-lint v2 config](https://golangci-lint.run/docs/configuration/file/) ·
  [staticcheck](https://staticcheck.dev/docs/) ·
  [govulncheck](https://go.dev/doc/security/vuln/)
- [GoReleaser Go builds](https://goreleaser.com/customization/builds/go/) ·
  [nfpm](https://goreleaser.com/customization/nfpm/) ·
  [Reproducible Go toolchains](https://go.dev/blog/rebuild)
- [cosign signing blobs](https://docs.sigstore.dev/cosign/signing/signing_with_blobs/) ·
  [actions/attest-build-provenance](https://github.com/actions/attest-build-provenance) ·
  [SLSA](https://slsa.dev/)
- [esbuild optionalDependencies PR #1621](https://github.com/evanw/esbuild/pull/1621) ·
  [npm provenance](https://docs.npmjs.com/generating-provenance-statements/) ·
  [runtime/debug.ReadBuildInfo](https://pkg.go.dev/runtime/debug#ReadBuildInfo)

**Telemetry, privacy & ethics**
- [log/slog](https://pkg.go.dev/log/slog) ·
  [Structured Logging with slog](https://go.dev/blog/slog) ·
  [Logging in Go (Better Stack)](https://betterstack.com/community/guides/logging/logging-in-go/)
- [Homebrew Analytics](https://docs.brew.sh/Analytics) ·
  [VS Code Telemetry](https://code.visualstudio.com/docs/configure/telemetry) ·
  [.NET CLI telemetry](https://learn.microsoft.com/en-us/dotnet/core/tools/telemetry) ·
  [DO_NOT_TRACK](https://donottrack.sh/) ·
  [GitHub CLI telemetry](https://docs.github.com/en/github-cli/github-cli/github-cli-telemetry) ·
  [gh v2.91 changelog](https://github.blog/changelog/2026-04-22-github-cli-opt-out-usage-telemetry/)
- [NIST SP 800-63B (salt ≥32 bits + KDF)](https://pages.nist.gov/800-63-3/sp800-63b.html) ·
  [Rainbow table attack](https://www.netwrix.com/en/cybersecurity-glossary/cyber-security-attacks/rainbow-table-attack) ·
  [Scrubbing PII (dash0)](https://www.dash0.com/guides/scrubbing-sensitive-data-with-opentelemetry) ·
  [Redaction allowlist (dash0)](https://www.dash0.com/guides/opentelemetry-redaction-processor)
- [JSON Lines](https://jsonlines.org/) ·
  [Crash-safe JSON](https://dev.to/constanta/crash-safe-json-at-scale-atomic-writes-recovery-without-a-db-3aic) ·
  [Tolerant Reader (Fowler)](https://martinfowler.com/bliki/TolerantReader.html) ·
  [Event Sourcing (Azure)](https://learn.microsoft.com/en-us/azure/architecture/patterns/event-sourcing)
- [creachadair/tomledit](https://github.com/creachadair/tomledit) ·
  [encoding/json](https://pkg.go.dev/encoding/json)

**Benchmark integrity (methodology)**
- [Adversarial leaderboard manipulation (2501.07493)](https://arxiv.org/abs/2501.07493) ·
  [The Leaderboard Illusion (2504.20879)](https://arxiv.org/abs/2504.20879) ·
  [LMArena response](https://arena.ai/blog/our-response/)

**Tooling referenced in CAPTURE.md**
- [hyperfine](https://github.com/sharkdp/hyperfine) ·
  [goccy/go-json](https://github.com/goccy/go-json) ·
  [bytedance/sonic](https://github.com/bytedance/sonic) ·
  [go-git](https://github.com/go-git/go-git) ·
  [git2go (avoid)](https://github.com/libgit2/git2go) ·
  [gocloc](https://github.com/hhatto/gocloc) ·
  [go-tree-sitter](https://github.com/tree-sitter/go-tree-sitter) ·
  [fsnotify](https://github.com/fsnotify/fsnotify) ·
  [invopop/jsonschema](https://github.com/invopop/jsonschema)

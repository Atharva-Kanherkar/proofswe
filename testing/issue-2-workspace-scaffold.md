# issue-2-workspace-scaffold — Test Contract

## Functional Behavior
- The repository is a buildable Cargo workspace with a virtual root manifest and
  exactly these crates under `crates/`: `core`, `adapter-claude-code`,
  `adapter-codex`, `cli`, and `xtask`.
- Each crate is named exactly like its folder. Internal crates use
  `version = "0.0.0"`.
- The `cli` crate exposes one binary named `proofswe`.
- `cargo run -p cli -- version` prints a semver string and exits 0.
- `cargo run -p cli -- help` prints usage text including `proofswe version` and
  exits 0.
- Unknown commands and unexpected arguments fail with a non-zero exit code and
  include a concise error plus usage guidance.
- Argument parsing uses `lexopt` or `pico-args`, never `clap`.
- The workspace defines `[profile.release-lto]` with `inherits = "release"`,
  `lto = "fat"`, `codegen-units = 1`, `panic = "abort"`,
  `strip = "symbols"`, and `opt-level = "s"`.
- No capture, adapter, hook, transcript, or sink logic is implemented in this
  issue.

## Unit Tests
- `cli::parse_command` maps no command and `help` to help behavior.
- `cli::parse_command` maps `version` to version behavior.
- `cli::parse_command` rejects unknown subcommands.
- `cli::parse_command` rejects extra trailing arguments for `help` and `version`.
- `core` has at least one unit test proving the crate is linked and testable
  without capture logic.
- `adapter-claude-code` and `adapter-codex` have smoke unit tests proving they
  compile against `core` while remaining logic-free placeholders.

## Integration / Functional Tests
- `cargo build --workspace` succeeds.
- `cargo test --workspace` succeeds.
- `cargo clippy --workspace -- -D warnings` succeeds.
- `cargo fmt --check` succeeds.
- `cargo tree` output contains no `clap`.
- `cargo build --profile release-lto` succeeds and produces the `proofswe`
  binary through the `cli` package.

## Smoke Tests
- `cargo run -p cli -- version` exits 0 and prints a semver string.
- `cargo run -p cli -- help` exits 0 and prints usage text.
- `cargo run -p cli -- nope` exits non-zero and prints an unknown-command error.

## E2E Tests
- N/A — this issue only creates the workspace foundation and command stubs. There
  is no capture workflow yet.

## Manual / cURL Tests
- N/A — no HTTP service or remote API is introduced.
- Manual reviewer commands:
  - `cargo build --workspace`
  - `cargo test --workspace`
  - `cargo clippy --workspace -- -D warnings`
  - `cargo fmt --check`
  - `cargo run -p cli -- version`
  - `cargo run -p cli -- help`
  - `cargo build --profile release-lto`
  - `cargo tree | rg clap` should return no matches.

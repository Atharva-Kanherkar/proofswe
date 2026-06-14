# hook-lifecycle - Test Contract

## Functional Behavior
- `proofswe enable` creates tagged proofswe hook entries in the Claude Code user settings file and the Codex user config file, creating parent directories and files when needed.
- `proofswe enable` is idempotent: running it twice leaves one proofswe-owned hook block per expected hook and does not duplicate entries.
- `proofswe disable --hooks` removes only proofswe-owned hook entries and leaves unrelated Claude Code and Codex config content intact.
- `proofswe disable` without `--hooks` returns usage guidance and does not edit config files.
- Codex hook registration targets the user config path only, never a project-local config.
- `proofswe off` writes `enabled=false` to the proofswe user config. `proofswe on` writes `enabled=true`.
- Hook entrypoints check `PROOFSWE_OFF`, `DO_NOT_TRACK`, the user config enabled flag, and `.proofswe-ignore` before doing any capture work.
- Disabled or ignored hook entrypoints exit successfully, write nothing, and avoid creating capture output.
- `SessionStart` hook entrypoints print `proofswe observing locally; disable: proofswe off` to stderr only when capture is enabled, leaving stdout empty so the notice is not injected into model context.
- `.proofswe-ignore` suppresses hooks from the current working directory or any parent directory up to the filesystem root.
- `proofswe off` and `proofswe on` preserve unrelated lines in `~/.proofswe/config` while updating or inserting the `enabled=` setting.
- Claude Code settings edits preserve existing non-hook JSON bytes and key order outside the top-level `hooks` property.
- Registered hook commands are shell-quoted for the target platform, including Windows paths.
- `proofswe status` reports whether proofswe is enabled and whether Claude Code and Codex user hooks are wired.

## Unit Tests
- `TestRunEnableCreatesAndRemovesTaggedHooks` - enable writes tagged Claude Code and Codex hooks; disable removes only proofswe hooks.
- `TestRunEnableIsIdempotent` - two enable runs produce a single tagged hook set.
- `TestRunDisableRequiresHooksFlag` - `disable` without `--hooks` returns `ErrUsage`.
- `TestRunOffOnStatus` - off/on update config, preserve unrelated config lines, and status reflects enabled state.
- `TestHookEntrypointHonorsDisabledConfigBeforeOutput` - disabled config exits 0 with no output.
- `TestHookEntrypointHonorsEnvKillSwitchBeforeOutput` - `PROOFSWE_OFF=1` exits 0 with no output.
- `TestHookEntrypointHonorsDoNotTrackBeforeOutput` - `DO_NOT_TRACK=1` exits 0 with no output.
- `TestHookEntrypointHonorsRepoIgnoreBeforeOutput` - `.proofswe-ignore` exits 0 with no output.
- `TestHookSessionStartPrintsNoticeWhenEnabled` - enabled `SessionStart` emits the loud local notice on stderr and leaves stdout empty.
- `TestCodexHooksUseUserConfigPath` - hook registration writes only the injected Codex user config path.
- `TestClaudeHookEditsPreserveTopLevelSettingsOrder` - enabling and disabling hooks does not reorder unrelated Claude Code settings keys.
- `TestHookCommandQuotesWindowsExecutables` - Windows hook command strings are quoted for Windows shell parsing.

## Integration / Functional Tests
- `go test ./internal/cli ./cmd/proofswe` verifies command dispatch and config file round trips.
- `go test ./...` verifies the lifecycle code does not regress core, reader, or adapter packages.

## Smoke Tests
- `go run ./cmd/proofswe -- status` prints enabled state and hook wiring status without requiring existing user config files.
- `go run ./cmd/proofswe -- help` lists lifecycle commands.

## E2E Tests
- N/A - issue #7 explicitly allows hook entrypoints to be stubs; snapshot and resolve behavior lands in #8 and #9.

## Manual / cURL Tests
- In a temporary home directory, run `proofswe enable`, inspect `.claude/settings.json` and `.codex/config.toml`, run `proofswe enable` again, and confirm no duplicate proofswe hook entries.
- In the same temporary home directory, run `proofswe disable --hooks` and confirm unrelated config content remains.
- Run `proofswe off`, then invoke a hook command and confirm it exits 0 with no output.

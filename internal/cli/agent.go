package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func runAgentCommand(cfg Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: agent requires a subcommand: install", ErrUsage)
	}
	switch args[0] {
	case "install":
		return runAgentInstallCommand(cfg, args[1:])
	default:
		return fmt.Errorf("%w: unknown agent subcommand %q", ErrUsage, args[0])
	}
}

func runAgentInstallCommand(cfg Config, args []string) error {
	flags := flag.NewFlagSet("agent install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var codexHome, claudeHome string
	flags.StringVar(&codexHome, "codex-home", "", "Codex home directory (default: CODEX_HOME or ~/.codex)")
	flags.StringVar(&claudeHome, "claude-home", "", "Claude Code home directory (default: ~/.claude)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%w: agent install does not accept positional arguments", ErrUsage)
	}
	cfg = cfg.withDefaults()
	if codexHome == "" {
		codexHome = firstNonEmpty(getenvOrEmpty(cfg, "CODEX_HOME"), filepath.Join(cfg.HomeDir, ".codex"))
	}
	if claudeHome == "" {
		claudeHome = firstNonEmpty(getenvOrEmpty(cfg, "PROOFSWE_CLAUDE_HOME"), filepath.Join(cfg.HomeDir, ".claude"))
	}
	codexPromptPath := filepath.Join(codexHome, "prompts", "benchmark.md")
	codexSkillPath := filepath.Join(codexHome, "skills", "proofswe-benchmark", "SKILL.md")
	claudeSkillPath := filepath.Join(claudeHome, "skills", "proofswe-benchmark", "SKILL.md")
	if err := writeAgentAsset(codexPromptPath, codexBenchmarkPrompt); err != nil {
		return err
	}
	if err := writeAgentAsset(codexSkillPath, proofsweBenchmarkSkill); err != nil {
		return err
	}
	if err := writeAgentAsset(claudeSkillPath, claudeBenchmarkSkill); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cfg.Stdout, "installed Codex prompt:     %s\n", codexPromptPath)
	_, _ = fmt.Fprintf(cfg.Stdout, "installed Codex skill:      %s\n", codexSkillPath)
	_, _ = fmt.Fprintf(cfg.Stdout, "installed Claude Code skill: %s\n", claudeSkillPath)
	_, _ = fmt.Fprintln(cfg.Stdout, "\nUse /prompts:benchmark or mention $proofswe-benchmark inside Codex; mention $proofswe-benchmark inside Claude Code.")
	return nil
}

func writeAgentAsset(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

const codexBenchmarkPrompt = `Run the proofswe benchmark submission for the current coding-agent session.

Steps:
1. From the repo root, run ` + "`proofswe submit`" + ` and wait for the official scorecard.
2. If ` + "`proofswe`" + ` is not on PATH, run ` + "`npx -y proofswe submit`" + `.
3. Do not ask the user for an OpenAI or Anthropic key; server submission uses the proofswe hosted judge.
4. If submission fails because the task is not reproducible, report the exact reason and do not force-submit unless the user explicitly asks.
5. Return the score, status, submission URL, and corpus PR/path if present.
`

const proofsweBenchmarkSkill = `---
name: proofswe-benchmark
description: Run proofswe benchmarking/submission from inside a coding-agent chat. Use when the user asks to benchmark the current agent session, donate the transcript, submit to proofswe, get an official scorecard, run /benchmark, or avoid leaving the chat to run the CLI.
---

# Proofswe Benchmark

Run the benchmark from the current repository without asking the user to leave the agent chat.

1. Prefer ` + "`proofswe submit`" + ` from the repo root. It auto-detects the latest Claude Code or Codex transcript and waits for the official hosted scorecard.
2. If ` + "`proofswe`" + ` is unavailable, use ` + "`npx -y proofswe submit`" + `.
3. Use ` + "`proofswe submit --json`" + ` when structured output is easier to summarize.
4. Use ` + "`--no-wait`" + ` only when the user wants to queue the submission and continue immediately.
5. Never ask for a local judge API key for ` + "`submit`" + `; hosted submission does the official judging. Local keys are only for ` + "`proofswe score --local-judge`" + ` previews.
6. If reproducibility checks fail, show the exact blocker. Do not use ` + "`--force`" + ` unless the user explicitly asks.
7. Report the official score, status, submission URL, and corpus PR/path when present.
`

const claudeBenchmarkSkill = `---
name: proofswe-benchmark
description: Run proofswe benchmarking/submission from inside Claude Code. Use when the user asks to benchmark the current Claude Code session, donate the transcript, submit to proofswe, get an official scorecard, run proofswe benchmark, or avoid leaving Claude Code to run the CLI.
---

# Proofswe Benchmark

Run the benchmark from the current repository without asking the user to leave Claude Code.

1. Run ` + "`proofswe submit --harness=claudecode`" + ` from the repo root. It auto-detects the latest Claude Code transcript and waits for the official hosted scorecard.
2. If ` + "`proofswe`" + ` is unavailable, use ` + "`npx -y proofswe submit --harness=claudecode`" + `.
3. Use ` + "`proofswe submit --harness=claudecode --json`" + ` when structured output is easier to summarize.
4. Use ` + "`--no-wait`" + ` only when the user wants to queue the submission and continue immediately.
5. Never ask for a local judge API key for ` + "`submit`" + `; hosted submission does the official judging. Local keys are only for ` + "`proofswe score --local-judge`" + ` previews.
6. If reproducibility checks fail, show the exact blocker. Do not use ` + "`--force`" + ` unless the user explicitly asks.
7. Report the official score, status, submission URL, and corpus PR/path when present.
`

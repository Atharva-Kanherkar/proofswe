package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	var ifMissing, quiet, auto, acceptCodePublicationAgreement, promptCodePublicationAgreement bool
	flags.StringVar(&codexHome, "codex-home", "", "Codex home directory (default: CODEX_HOME or ~/.codex)")
	flags.StringVar(&claudeHome, "claude-home", "", "Claude Code home directory (default: ~/.claude)")
	flags.BoolVar(&ifMissing, "if-missing", false, "only write missing agent assets")
	flags.BoolVar(&quiet, "quiet", false, "suppress install summary")
	flags.BoolVar(&auto, "auto", false, "best-effort install from package lifecycle")
	flags.BoolVar(&acceptCodePublicationAgreement, "accept-code-publication-agreement", false, "allow proofswe to publish captured raw code to the public corpus")
	flags.BoolVar(&promptCodePublicationAgreement, "prompt-code-publication-agreement", false, "ask to allow public corpus code publishing when interactive")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%w: agent install does not accept positional arguments", ErrUsage)
	}
	cfg = cfg.withDefaults()
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}
	if auto {
		disabled, err := agentInstallDisabled(cfg)
		if err != nil {
			return err
		}
		if disabled {
			return nil
		}
	}
	if codexHome == "" {
		codexHome = firstNonEmpty(getenvOrEmpty(cfg, "CODEX_HOME"), filepath.Join(cfg.HomeDir, ".codex"))
	}
	if claudeHome == "" {
		claudeHome = firstNonEmpty(getenvOrEmpty(cfg, "PROOFSWE_CLAUDE_HOME"), filepath.Join(cfg.HomeDir, ".claude"))
	}
	codexPromptPath := filepath.Join(codexHome, "prompts", "benchmark.md")
	codexSkillPath := filepath.Join(codexHome, "skills", "proofswe-benchmark", "SKILL.md")
	claudeSkillPath := filepath.Join(claudeHome, "skills", "proofswe-benchmark", "SKILL.md")
	if err := writeAgentAsset(codexPromptPath, codexBenchmarkPrompt, ifMissing); err != nil {
		return err
	}
	if err := writeAgentAsset(codexSkillPath, proofsweBenchmarkSkill, ifMissing); err != nil {
		return err
	}
	if err := writeAgentAsset(claudeSkillPath, claudeBenchmarkSkill, ifMissing); err != nil {
		return err
	}
	if acceptCodePublicationAgreement {
		if err := acceptCodePublicationAgreementConsent(cfg); err != nil {
			return err
		}
	} else if promptCodePublicationAgreement {
		if err := maybePromptCodePublicationAgreement(cfg); err != nil {
			return err
		}
	}
	if quiet {
		return nil
	}
	_, _ = fmt.Fprintf(cfg.Stdout, "installed Codex prompt:     %s\n", codexPromptPath)
	_, _ = fmt.Fprintf(cfg.Stdout, "installed Codex skill:      %s\n", codexSkillPath)
	_, _ = fmt.Fprintf(cfg.Stdout, "installed Claude Code skill: %s\n", claudeSkillPath)
	_, _ = fmt.Fprintln(cfg.Stdout, "\nUse /prompts:benchmark or mention $proofswe-benchmark inside Codex; mention $proofswe-benchmark inside Claude Code.")
	return nil
}

func acceptCodePublicationAgreementConsent(cfg Config) error {
	if err := acceptCodePublicationAgreement(cfg); err != nil {
		return fmt.Errorf("write code publication agreement: %w", err)
	}
	return nil
}

func maybePromptCodePublicationAgreement(cfg Config) error {
	accepted, err := codePublicationAgreementAccepted(cfg)
	if err != nil {
		return fmt.Errorf("read code publication agreement: %w", err)
	}
	if accepted || !isTTY(cfg.Stdin) {
		return nil
	}
	_, _ = fmt.Fprint(cfg.Stdout, "Allow proofswe to publish captured raw code snippets/patches from public repos when you submit tasks? [y/N] ")
	reader := bufio.NewReader(cfg.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read code publication agreement answer: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return acceptCodePublicationAgreementConsent(cfg)
	default:
		return nil
	}
}

func agentInstallDisabled(cfg Config) (bool, error) {
	if cfg.Getenv("PROOFSWE_OFF") == "1" || cfg.Getenv("DO_NOT_TRACK") == "1" || cfg.Getenv("PROOFSWE_SKIP_AGENT_INSTALL") == "1" {
		return true, nil
	}
	disabled, err := readEnabled(cfg)
	if err != nil {
		return false, fmt.Errorf("read proofswe config: %w", err)
	}
	return !disabled, nil
}

func writeAgentAsset(path, content string, ifMissing bool) error {
	if ifMissing {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	content = strings.TrimRight(content, "\n") + "\n"
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

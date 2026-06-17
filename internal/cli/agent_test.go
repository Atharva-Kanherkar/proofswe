package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentInstallWritesCodexPromptAndSkill(t *testing.T) {
	var stdout bytes.Buffer
	home := t.TempDir()
	codexHome := filepath.Join(home, "codex-home")
	claudeHome := filepath.Join(home, "claude-home")
	cfg := Config{
		HomeDir: home,
		Stdout:  &stdout,
		Stderr:  &bytes.Buffer{},
		Getenv:  func(string) string { return "" },
	}
	if err := runAgentCommand(cfg, []string{"install", "--codex-home", codexHome, "--claude-home", claudeHome}); err != nil {
		t.Fatalf("agent install: %v", err)
	}

	prompt := mustReadString(t, filepath.Join(codexHome, "prompts", "benchmark.md"))
	skill := mustReadString(t, filepath.Join(codexHome, "skills", "proofswe-benchmark", "SKILL.md"))
	claudeSkill := mustReadString(t, filepath.Join(claudeHome, "skills", "proofswe-benchmark", "SKILL.md"))
	for _, got := range []string{prompt, skill, claudeSkill} {
		if !strings.Contains(got, "proofswe submit") {
			t.Fatalf("installed asset missing submit command:\n%s", got)
		}
		if strings.Contains(got, "OPENAI_API_KEY") || strings.Contains(got, "ANTHROPIC_API_KEY") {
			t.Fatalf("installed asset should not ask for local judge keys:\n%s", got)
		}
	}
	if !strings.Contains(stdout.String(), "/prompts:benchmark") || !strings.Contains(stdout.String(), "$proofswe-benchmark") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "$proofswe-benchmark") || !strings.Contains(claudeSkill, "name: proofswe-benchmark") {
		t.Fatalf("Claude skill not installed as proofswe-benchmark:\nstdout=%s\nskill=%s", stdout.String(), claudeSkill)
	}
	if _, err := os.Stat(filepath.Join(claudeHome, "skills", "benchmark", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("generic Claude benchmark skill should not be written; stat err = %v", err)
	}
}

func TestAgentInstallAutoQuietIfMissingPreservesExistingAssets(t *testing.T) {
	var stdout bytes.Buffer
	home := t.TempDir()
	codexHome := filepath.Join(home, "codex-home")
	claudeHome := filepath.Join(home, "claude-home")
	existingSkill := filepath.Join(codexHome, "skills", "proofswe-benchmark", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(existingSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingSkill, []byte("custom skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		HomeDir: home,
		Stdout:  &stdout,
		Stderr:  &bytes.Buffer{},
		Getenv:  func(string) string { return "" },
	}
	err := runAgentCommand(cfg, []string{
		"install",
		"--auto",
		"--if-missing",
		"--quiet",
		"--codex-home", codexHome,
		"--claude-home", claudeHome,
	})
	if err != nil {
		t.Fatalf("agent install: %v", err)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want quiet", stdout.String())
	}
	if got := mustReadString(t, existingSkill); got != "custom skill\n" {
		t.Fatalf("existing skill overwritten: %q", got)
	}
	if got := mustReadString(t, filepath.Join(codexHome, "prompts", "benchmark.md")); !strings.Contains(got, "proofswe submit") {
		t.Fatalf("missing codex prompt: %q", got)
	}
	if got := mustReadString(t, filepath.Join(claudeHome, "skills", "proofswe-benchmark", "SKILL.md")); !strings.Contains(got, "proofswe submit") {
		t.Fatalf("missing claude skill: %q", got)
	}
}

func TestAgentInstallAutoHonorsKillSwitches(t *testing.T) {
	for _, tc := range []struct {
		name       string
		env        map[string]string
		configText string
	}{
		{name: "proofswe-off", env: map[string]string{"PROOFSWE_OFF": "1"}},
		{name: "do-not-track", env: map[string]string{"DO_NOT_TRACK": "1"}},
		{name: "skip-agent-install", env: map[string]string{"PROOFSWE_SKIP_AGENT_INSTALL": "1"}},
		{name: "config-disabled", configText: "enabled=false\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			codexHome := filepath.Join(home, "codex-home")
			claudeHome := filepath.Join(home, "claude-home")
			cfg := Config{
				HomeDir: home,
				Stdout:  &bytes.Buffer{},
				Stderr:  &bytes.Buffer{},
				Getenv: func(key string) string {
					return tc.env[key]
				},
			}
			if tc.configText != "" {
				if err := os.MkdirAll(filepath.Dir(proofsweConfigPath(cfg)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(proofsweConfigPath(cfg), []byte(tc.configText), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := runAgentCommand(cfg, []string{
				"install",
				"--auto",
				"--if-missing",
				"--quiet",
				"--codex-home", codexHome,
				"--claude-home", claudeHome,
			})
			if err != nil {
				t.Fatalf("agent install: %v", err)
			}
			if _, err := os.Stat(filepath.Join(codexHome, "prompts", "benchmark.md")); !os.IsNotExist(err) {
				t.Fatalf("codex prompt should not be written; stat err = %v", err)
			}
			if _, err := os.Stat(filepath.Join(claudeHome, "skills", "proofswe-benchmark", "SKILL.md")); !os.IsNotExist(err) {
				t.Fatalf("claude skill should not be written; stat err = %v", err)
			}
		})
	}
}

func mustReadString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

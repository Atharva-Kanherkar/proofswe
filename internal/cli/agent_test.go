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
	cfg := Config{
		HomeDir: home,
		Stdout:  &stdout,
		Stderr:  &bytes.Buffer{},
		Getenv:  func(string) string { return "" },
	}
	if err := runAgentCommand(cfg, []string{"install", "--codex-home", codexHome}); err != nil {
		t.Fatalf("agent install: %v", err)
	}

	prompt := mustReadString(t, filepath.Join(codexHome, "prompts", "benchmark.md"))
	skill := mustReadString(t, filepath.Join(codexHome, "skills", "proofswe-benchmark", "SKILL.md"))
	for _, got := range []string{prompt, skill} {
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
}

func mustReadString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

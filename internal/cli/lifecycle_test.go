package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunEnableCreatesAndRemovesTaggedHooks(t *testing.T) {
	cfg, stdout := testConfig(t)
	claudePath := filepath.Join(cfg.HomeDir, ".claude", "settings.json")
	codexPath := filepath.Join(cfg.HomeDir, ".codex", "config.toml")
	mustWriteFile(t, claudePath, []byte(`{"theme":"dark","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo keep"}]}]}}`))
	mustWriteFile(t, codexPath, []byte("model = \"gpt-5\"\n"))

	cfg.Args = []string{"enable"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("enable error = %v", err)
	}
	if stdout.String() == "" {
		t.Fatal("enable should print status")
	}

	claudeData := mustReadFile(t, claudePath)
	if !bytes.Contains(claudeData, []byte(proofsweTag)) {
		t.Fatalf("claude settings missing proofswe tag:\n%s", claudeData)
	}
	if !bytes.Contains(claudeData, []byte("echo keep")) {
		t.Fatalf("claude settings lost existing hook:\n%s", claudeData)
	}

	codexData := mustReadFile(t, codexPath)
	if !bytes.Contains(codexData, []byte(codexBlockStart)) || !bytes.Contains(codexData, []byte("[[hooks.SessionStart]]")) {
		t.Fatalf("codex config missing tagged hook block:\n%s", codexData)
	}
	if !bytes.Contains(codexData, []byte("model = \"gpt-5\"")) {
		t.Fatalf("codex config lost existing content:\n%s", codexData)
	}

	stdout.Reset()
	cfg.Args = []string{"disable", "--hooks"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("disable --hooks error = %v", err)
	}

	claudeData = mustReadFile(t, claudePath)
	if bytes.Contains(claudeData, []byte(proofsweTag)) {
		t.Fatalf("claude settings kept proofswe hook:\n%s", claudeData)
	}
	if !bytes.Contains(claudeData, []byte("echo keep")) {
		t.Fatalf("claude settings removed unrelated hook:\n%s", claudeData)
	}

	codexData = mustReadFile(t, codexPath)
	if bytes.Contains(codexData, []byte(codexBlockStart)) {
		t.Fatalf("codex config kept proofswe hook block:\n%s", codexData)
	}
	if string(codexData) != "model = \"gpt-5\"\n" {
		t.Fatalf("codex config = %q, want original content", codexData)
	}
}

func TestRunEnableIsIdempotent(t *testing.T) {
	cfg, _ := testConfig(t)

	cfg.Args = []string{"enable"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("first enable error = %v", err)
	}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("second enable error = %v", err)
	}

	claudeData := mustReadFile(t, filepath.Join(cfg.HomeDir, ".claude", "settings.json"))
	if got := bytes.Count(claudeData, []byte(proofsweTag)); got != 3 {
		t.Fatalf("proofswe tag count in claude settings = %d, want 3:\n%s", got, claudeData)
	}

	codexData := mustReadFile(t, filepath.Join(cfg.HomeDir, ".codex", "config.toml"))
	if got := bytes.Count(codexData, []byte(codexBlockStart)); got != 1 {
		t.Fatalf("codex proofswe block count = %d, want 1:\n%s", got, codexData)
	}
}

func TestRunDisableRequiresHooksFlag(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.Args = []string{"disable"}

	err := Run(context.Background(), cfg)
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("disable error = %v, want ErrUsage", err)
	}
}

func TestRunOffOnStatus(t *testing.T) {
	cfg, stdout := testConfig(t)

	cfg.Args = []string{"off"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("off error = %v", err)
	}
	stdout.Reset()
	cfg.Args = []string{"status"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("status error = %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "enabled: false") {
		t.Fatalf("status = %q, want disabled", got)
	}

	stdout.Reset()
	cfg.Args = []string{"on"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("on error = %v", err)
	}
	stdout.Reset()
	cfg.Args = []string{"status"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("status error = %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "enabled: true") {
		t.Fatalf("status = %q, want enabled", got)
	}
}

func TestHookEntrypointHonorsDisabledConfigBeforeOutput(t *testing.T) {
	cfg, stdout := testConfig(t)
	mustWriteFile(t, filepath.Join(cfg.HomeDir, ".proofswe", "config"), []byte("enabled=false\n"))
	cfg.Args = []string{"hook", "codex", "SessionStart"}

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("hook error = %v", err)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestHookEntrypointHonorsEnvKillSwitchBeforeOutput(t *testing.T) {
	cfg, stdout := testConfig(t)
	cfg.Getenv = func(key string) string {
		if key == "PROOFSWE_OFF" {
			return "1"
		}
		return ""
	}
	cfg.Args = []string{"hook", "claudecode", "SessionStart"}

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("hook error = %v", err)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestHookEntrypointHonorsDoNotTrackBeforeOutput(t *testing.T) {
	cfg, stdout := testConfig(t)
	cfg.Getenv = func(key string) string {
		if key == "DO_NOT_TRACK" {
			return "1"
		}
		return ""
	}
	cfg.Args = []string{"hook", "codex", "SessionStart"}

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("hook error = %v", err)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestHookEntrypointHonorsRepoIgnoreBeforeOutput(t *testing.T) {
	cfg, stdout := testConfig(t)
	mustWriteFile(t, filepath.Join(cfg.WorkDir, ".proofswe-ignore"), nil)
	cfg.Args = []string{"hook", "codex", "SessionStart"}

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("hook error = %v", err)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestHookSessionStartPrintsNoticeWhenEnabled(t *testing.T) {
	cfg, stdout := testConfig(t)
	cfg.Args = []string{"hook", "codex", "SessionStart"}

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("hook error = %v", err)
	}
	if got, want := stdout.String(), noticeLine+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestCodexHooksUseUserConfigPath(t *testing.T) {
	cfg, _ := testConfig(t)
	projectConfig := filepath.Join(cfg.WorkDir, ".codex", "config.toml")
	mustWriteFile(t, projectConfig, []byte("project = true\n"))

	cfg.Args = []string{"enable"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("enable error = %v", err)
	}

	if got := string(mustReadFile(t, projectConfig)); got != "project = true\n" {
		t.Fatalf("project codex config changed: %q", got)
	}
	userConfig := filepath.Join(cfg.HomeDir, ".codex", "config.toml")
	if _, err := os.Stat(userConfig); err != nil {
		t.Fatalf("user codex config not written: %v", err)
	}
}

func TestClaudeSettingsRemainValidJSON(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.Args = []string{"enable"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("enable error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(mustReadFile(t, filepath.Join(cfg.HomeDir, ".claude", "settings.json")), &decoded); err != nil {
		t.Fatalf("claude settings invalid JSON: %v", err)
	}
}

func testConfig(t *testing.T) (Config, *bytes.Buffer) {
	t.Helper()

	var stdout bytes.Buffer
	tmp := t.TempDir()
	cfg := Config{
		Args:    []string{"status"},
		Stdout:  &stdout,
		Version: "test",
		HomeDir: filepath.Join(tmp, "home"),
		WorkDir: filepath.Join(tmp, "repo"),
		ExePath: filepath.Join(tmp, "bin", "proofswe"),
		Getenv:  func(string) string { return "" },
	}
	if err := os.MkdirAll(filepath.Dir(cfg.ExePath), 0o755); err != nil {
		t.Fatalf("MkdirAll exe dir: %v", err)
	}
	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		t.Fatalf("MkdirAll work dir: %v", err)
	}

	return cfg, &stdout
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return data
}

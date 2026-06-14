package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestRunDisableHooksDoesNotCreateMissingConfigFiles(t *testing.T) {
	cfg, _ := testConfig(t)
	claudePath := filepath.Join(cfg.HomeDir, ".claude", "settings.json")
	codexPath := filepath.Join(cfg.HomeDir, ".codex", "config.toml")
	cfg.Args = []string{"disable", "--hooks"}

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("disable --hooks error = %v", err)
	}

	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Fatalf("claude settings stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(codexPath); !os.IsNotExist(err) {
		t.Fatalf("codex config stat error = %v, want not exist", err)
	}
}

func TestRunOffOnStatus(t *testing.T) {
	cfg, stdout := testConfig(t)
	configPath := filepath.Join(cfg.HomeDir, ".proofswe", "config")
	mustWriteFile(t, configPath, []byte("# keep this\nconsent_tier=metadata\n"))

	cfg.Args = []string{"off"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("off error = %v", err)
	}
	if got := string(mustReadFile(t, configPath)); !strings.Contains(got, "consent_tier=metadata") || !strings.Contains(got, "enabled=false") {
		t.Fatalf("off config = %q, want preserved consent tier and enabled=false", got)
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
	if got := string(mustReadFile(t, configPath)); !strings.Contains(got, "consent_tier=metadata") || !strings.Contains(got, "enabled=true") {
		t.Fatalf("on config = %q, want preserved consent tier and enabled=true", got)
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
	subdir := filepath.Join(cfg.WorkDir, "a", "b")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll subdir: %v", err)
	}
	cfg.WorkDir = subdir
	mustWriteFile(t, filepath.Join(cfg.WorkDir, ".proofswe-ignore"), nil)
	cfg.Args = []string{"hook", "codex", "SessionStart"}

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("hook error = %v", err)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestHookEntrypointHonorsRepoIgnoreInParentDirectory(t *testing.T) {
	cfg, stdout := testConfig(t)
	subdir := filepath.Join(cfg.WorkDir, "a", "b")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll subdir: %v", err)
	}
	mustWriteFile(t, filepath.Join(cfg.WorkDir, ".proofswe-ignore"), nil)
	cfg.WorkDir = subdir
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
	var stderr bytes.Buffer
	cfg.Stderr = &stderr
	cfg.Args = []string{"hook", "codex", "SessionStart"}

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("hook error = %v", err)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
	if got, want := stderr.String(), noticeLine+"\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
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

func TestClaudeHookEditsPreserveTopLevelSettingsOrder(t *testing.T) {
	cfg, _ := testConfig(t)
	claudePath := filepath.Join(cfg.HomeDir, ".claude", "settings.json")
	original := "{\n  \"model\": \"opus\",\n  \"theme\": \"dark\",\n  \"enabledPlugins\": [\"x\"],\n  \"statusLine\": {\"type\":\"command\",\"command\":\"echo ok\"}\n}\n"
	mustWriteFile(t, claudePath, []byte(original))

	cfg.Args = []string{"enable"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("enable error = %v", err)
	}

	enabled := string(mustReadFile(t, claudePath))
	assertBefore(t, enabled, "\"model\"", "\"theme\"")
	assertBefore(t, enabled, "\"theme\"", "\"enabledPlugins\"")
	assertBefore(t, enabled, "\"enabledPlugins\"", "\"statusLine\"")
	if !strings.Contains(enabled, "\"hooks\"") {
		t.Fatalf("enabled settings missing hooks:\n%s", enabled)
	}

	cfg.Args = []string{"disable", "--hooks"}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("disable --hooks error = %v", err)
	}
	if got := string(mustReadFile(t, claudePath)); got != original {
		t.Fatalf("settings after disable =\n%s\nwant original =\n%s", got, original)
	}
}

func TestHookCommandQuotesWindowsExecutables(t *testing.T) {
	got := hookCommandForOS(`C:\Program Files\proofswe\proofswe.exe`, "codex", "SessionStart", "windows")
	want := `"C:\Program Files\proofswe\proofswe.exe" hook codex SessionStart`
	if got != want {
		t.Fatalf("windows hook command = %q, want %q", got, want)
	}

	if runtime.GOOS != "windows" {
		got = hookCommandForOS("/tmp/proofswe bin/proofswe", "codex", "SessionStart", "linux")
		want = "'/tmp/proofswe bin/proofswe' hook codex SessionStart"
		if got != want {
			t.Fatalf("posix hook command = %q, want %q", got, want)
		}
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

func assertBefore(t *testing.T, text, before, after string) {
	t.Helper()

	beforeIndex := strings.Index(text, before)
	afterIndex := strings.Index(text, after)
	if beforeIndex < 0 || afterIndex < 0 || beforeIndex > afterIndex {
		t.Fatalf("expected %q before %q in:\n%s", before, after, text)
	}
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

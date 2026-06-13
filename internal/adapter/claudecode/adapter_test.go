package claudecode

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/google/go-cmp/cmp"
)

var update = flag.Bool("update", false, "update golden files")

func TestGoldenFixtureSnapshot(t *testing.T) {
	path := filepath.Join("testdata", "fixtures", "session.jsonl")

	events, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	data, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	data = append(data, '\n')

	golden := filepath.Join("testdata", "fixtures", "session.normalized.golden.json")
	if *update {
		if err := os.WriteFile(golden, data, 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if diff := cmp.Diff(string(want), string(data)); diff != "" {
		t.Fatalf("normalized snapshot mismatch (-want +got):\n%s", diff)
	}
}

func TestAssistantMetricsFromFixture(t *testing.T) {
	events, err := ParseFile(filepath.Join("testdata", "fixtures", "session.jsonl"))
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	var assistant *core.AssistantMessage
	for _, event := range events {
		if candidate, ok := event.(*core.AssistantMessage); ok {
			assistant = candidate
			break
		}
	}
	if assistant == nil {
		t.Fatal("fixture did not produce AssistantMessage")
	}
	if assistant.Model.ID != "claude-opus-4-7" {
		t.Fatalf("Model.ID = %q, want claude-opus-4-7", assistant.Model.ID)
	}
	if assistant.Metrics.InputTokens != 11 {
		t.Fatalf("InputTokens = %d, want 11", assistant.Metrics.InputTokens)
	}
	if assistant.Metrics.OutputTokens != 22 {
		t.Fatalf("OutputTokens = %d, want 22", assistant.Metrics.OutputTokens)
	}
	if assistant.Metrics.CacheCreationInputTokens != 33 {
		t.Fatalf("CacheCreationInputTokens = %d, want 33", assistant.Metrics.CacheCreationInputTokens)
	}
	if assistant.Metrics.CacheReadInputTokens != 44 {
		t.Fatalf("CacheReadInputTokens = %d, want 44", assistant.Metrics.CacheReadInputTokens)
	}
	if !assistant.Event.IsSubagent {
		t.Fatal("Event.IsSubagent = false, want true")
	}
	if assistant.Event.TurnIndex != 3 {
		t.Fatalf("Event.TurnIndex = %d, want 3", assistant.Event.TurnIndex)
	}
	if assistant.Source.GitBranch != "main" {
		t.Fatalf("Source.GitBranch = %q, want main", assistant.Source.GitBranch)
	}
}

func TestUnknownTypeMapsToCoreUnknown(t *testing.T) {
	events, err := ParseRaw([]byte(`{"type":"future-event","payload_hash":"sha256:0000000000000000000000000000000000000000000000000000000000000005"}`), "future.jsonl", 0)
	if err != nil {
		t.Fatalf("ParseRaw() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	unknown, ok := events[0].(core.Unknown)
	if !ok {
		t.Fatalf("event = %T, want core.Unknown", events[0])
	}
	if unknown.Type != "future-event" {
		t.Fatalf("Unknown.Type = %q, want future-event", unknown.Type)
	}
	if got := string(unknown.Raw); got != `{"type":"future-event"}` {
		t.Fatalf("Unknown.Raw = %s, want sanitized type-only raw", got)
	}
}

func TestParseRawNormalizesSourcePath(t *testing.T) {
	events, err := ParseRaw([]byte(`{"type":"started","sessionId":"session-a","timestamp":"2026-01-02T03:04:05Z"}`), `testdata\fixtures\session.jsonl`, 0)
	if err != nil {
		t.Fatalf("ParseRaw() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	start, ok := events[0].(*core.SessionStart)
	if !ok {
		t.Fatalf("event = %T, want *core.SessionStart", events[0])
	}
	if start.Source.Path != "testdata/fixtures/session.jsonl" {
		t.Fatalf("Source.Path = %q, want testdata/fixtures/session.jsonl", start.Source.Path)
	}
}

func TestDiscoverTranscripts(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-home-atharva-promptfoo-lab")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(projectDir, "session-a.jsonl")
	if err := os.WriteFile(path, []byte(`{"cwd":"/home/atharva/promptfoo-lab"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(Discover()) = %d, want 1", len(got))
	}
	if got[0].Path != path {
		t.Fatalf("Path = %q, want %q", got[0].Path, path)
	}
	if got[0].ProjectSlug != "-home-atharva-promptfoo-lab" {
		t.Fatalf("ProjectSlug = %q, want -home-atharva-promptfoo-lab", got[0].ProjectSlug)
	}
	if got[0].RepoPath != filepath.Join(string(filepath.Separator), "home", "atharva", "promptfoo-lab") {
		t.Fatalf("RepoPath = %q, want /home/atharva/promptfoo-lab", got[0].RepoPath)
	}
	if got[0].SessionID != "session-a" {
		t.Fatalf("SessionID = %q, want session-a", got[0].SessionID)
	}
}

func TestFixtureContainsOnlyRedactedContent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "fixtures", "session.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	for _, forbidden := range []string{
		"/home/",
		"@",
		"AKIA",
		"BEGIN PRIVATE KEY",
		"package ",
		"func ",
		"const ",
		"var ",
		"SELECT ",
		"curl ",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("fixture contains forbidden raw/PII marker %q", forbidden)
		}
	}

	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("fixture line is not JSON: %v", err)
		}
		lines = append(lines, value)
	}
	for _, line := range lines {
		assertRedactedStrings(t, "", line)
	}
}

var hashPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func assertRedactedStrings(t *testing.T, key string, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for childKey, childValue := range typed {
			assertRedactedStrings(t, childKey, childValue)
		}
	case []any:
		for _, childValue := range typed {
			assertRedactedStrings(t, key, childValue)
		}
	case string:
		if isContentKey(key) && !hashPattern.MatchString(typed) {
			t.Fatalf("fixture key %q contains unredacted string %q", key, typed)
		}
	}
}

func isContentKey(key string) bool {
	switch key {
	case "content", "text", "command", "prompt", "result":
		return true
	default:
		return strings.HasSuffix(key, "_hash")
	}
}

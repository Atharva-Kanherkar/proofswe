package claudecode

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/google/go-cmp/cmp"
)

var update = flag.Bool("update", false, "update golden files")

// Fixed salt so golden hashes are deterministic. Production loads a per-install
// secret via loadOrCreateSalt; tests never touch that file.
var testSalt = []byte("proofswe-fixture-salt-v1")

func fixturePath() string {
	return filepath.Join("testdata", "fixtures", "session.jsonl")
}

func TestGoldenFixtureSnapshot(t *testing.T) {
	events, err := ParseFile(testSalt, fixturePath())
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

// The privacy guarantee: raw content goes in, only hashes come out. Assert the
// plaintext values from the fixture never survive into the normalized output.
func TestNormalizedOutputRedactsFixtureContent(t *testing.T) {
	events, err := ParseFile(testSalt, fixturePath())
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(data)
	for _, plaintext := range []string{
		"summarize the open issues",
		"I'll list the issues",
		"gh issue list",
		"issue #5 is open",
		"now open a pull request",
	} {
		if strings.Contains(text, plaintext) {
			t.Fatalf("normalized output leaked raw content %q", plaintext)
		}
	}
}

// Sensitive markers fed through every content-bearing path must never appear in
// the normalized output. Kept in-memory so the repo never stores real secrets.
func TestSanitizationDropsSensitiveContent(t *testing.T) {
	secrets := []string{
		"AKIAIOSFODNN7EXAMPLE",
		"-----BEGIN PRIVATE KEY-----",
		"/Users/victim/secret-project",
		"hunter2-password",
		"sk-proj-supersecrettoken",
	}
	lines := []string{
		`{"type":"user","uuid":"u1","sessionId":"s","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"my key is AKIAIOSFODNN7EXAMPLE"}}`,
		`{"type":"user","uuid":"u2","sessionId":"s","timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":[{"type":"text","text":"path is /Users/victim/secret-project"}]}}`,
		`{"type":"assistant","uuid":"u3","sessionId":"s","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","model":"m","content":[{"type":"text","text":"-----BEGIN PRIVATE KEY-----"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"export TOKEN=sk-proj-supersecrettoken"}}]}}`,
		`{"type":"user","uuid":"u4","sessionId":"s","timestamp":"2026-01-01T00:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"login failed for hunter2-password"}]}}`,
	}
	for i, line := range lines {
		events, err := ParseRaw(testSalt, []byte(line), "probe.jsonl", i)
		if err != nil {
			t.Fatalf("ParseRaw(line %d) error = %v", i, err)
		}
		data, err := json.Marshal(events)
		if err != nil {
			t.Fatalf("Marshal(line %d) error = %v", i, err)
		}
		text := string(data)
		for _, secret := range secrets {
			if strings.Contains(text, secret) {
				t.Fatalf("line %d leaked secret %q in %s", i, secret, text)
			}
		}
	}
}

// Regression: system records are turn-duration / api-error / away-summary
// metadata, not session boundaries. They must route to core.Unknown, never
// session_start.
func TestSystemRecordMapsToUnknownNotSessionStart(t *testing.T) {
	line := `{"type":"system","subtype":"turn_duration","uuid":"sys1","sessionId":"s","timestamp":"2026-01-01T00:00:00Z","durationMs":1500,"messageCount":5}`
	events, err := ParseRaw(testSalt, []byte(line), "probe.jsonl", 0)
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
	if unknown.Type != "system" {
		t.Fatalf("Unknown.Type = %q, want system", unknown.Type)
	}
	if got := string(unknown.Raw); got != `{"type":"system"}` {
		t.Fatalf("Unknown.Raw = %s, want sanitized type-only raw", got)
	}
}

// Regression: a user prompt sent as a content array (the dominant real shape)
// must still produce a user_prompt, not be silently dropped.
func TestArrayTextUserPromptCaptured(t *testing.T) {
	line := `{"type":"user","uuid":"u1","sessionId":"s","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":[{"type":"text","text":"refactor the auth module"}]}}`
	events, err := ParseRaw(testSalt, []byte(line), "probe.jsonl", 0)
	if err != nil {
		t.Fatalf("ParseRaw() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	prompt, ok := events[0].(*core.UserPrompt)
	if !ok {
		t.Fatalf("event = %T, want *core.UserPrompt", events[0])
	}
	if !strings.HasPrefix(prompt.PromptHash, "sha256:") {
		t.Fatalf("PromptHash = %q, want sha256: prefix", prompt.PromptHash)
	}
}

func TestStartedAndResultMapToBoundaries(t *testing.T) {
	start, err := ParseRaw(testSalt, []byte(`{"type":"started","uuid":"s1","sessionId":"s","timestamp":"2026-01-01T00:00:00Z"}`), "probe.jsonl", 0)
	if err != nil {
		t.Fatalf("ParseRaw(started) error = %v", err)
	}
	if len(start) != 1 {
		t.Fatalf("len(started events) = %d, want 1", len(start))
	}
	if _, ok := start[0].(*core.SessionStart); !ok {
		t.Fatalf("started event = %T, want *core.SessionStart", start[0])
	}

	end, err := ParseRaw(testSalt, []byte(`{"type":"result","uuid":"r1","sessionId":"s","timestamp":"2026-01-01T00:00:00Z","subtype":"success"}`), "probe.jsonl", 0)
	if err != nil {
		t.Fatalf("ParseRaw(result) error = %v", err)
	}
	if len(end) != 1 {
		t.Fatalf("len(result events) = %d, want 1", len(end))
	}
	sessionEnd, ok := end[0].(*core.SessionEnd)
	if !ok {
		t.Fatalf("result event = %T, want *core.SessionEnd", end[0])
	}
	if sessionEnd.Reason != "success" {
		t.Fatalf("SessionEnd.Reason = %q, want success", sessionEnd.Reason)
	}
}

func TestAssistantMetricsFromFixture(t *testing.T) {
	events, err := ParseFile(testSalt, fixturePath())
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
	if assistant.Event.TurnIndex != 2 {
		t.Fatalf("Event.TurnIndex = %d, want 2", assistant.Event.TurnIndex)
	}
	if assistant.Source.GitBranch != "main" {
		t.Fatalf("Source.GitBranch = %q, want main", assistant.Source.GitBranch)
	}
}

// Regression: token usage is reported once per assistant turn, so it must live on
// the assistant_message and be zeroed on the tool_call to avoid double-counting.
func TestAssistantMetricsNotDuplicatedOntoToolCall(t *testing.T) {
	events, err := ParseFile(testSalt, fixturePath())
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	var call *core.ToolCall
	for _, event := range events {
		if candidate, ok := event.(*core.ToolCall); ok {
			call = candidate
			break
		}
	}
	if call == nil {
		t.Fatal("fixture did not produce ToolCall")
	}
	if call.Metrics != (core.Metrics{}) {
		t.Fatalf("ToolCall.Metrics = %+v, want zero", call.Metrics)
	}
	if call.Model.ID != "claude-opus-4-7" {
		t.Fatalf("ToolCall.Model.ID = %q, want claude-opus-4-7", call.Model.ID)
	}
}

// Regression: is_error is a tool-error flag, not a process exit code, so we must
// not synthesize an exit_code from it. It is surfaced inside the result payload.
func TestToolResultHasNoSyntheticExitCode(t *testing.T) {
	events, err := ParseFile(testSalt, fixturePath())
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	var result *core.ToolResult
	for _, event := range events {
		if candidate, ok := event.(*core.ToolResult); ok {
			result = candidate
			break
		}
	}
	if result == nil {
		t.Fatal("fixture did not produce ToolResult")
	}
	if result.ExitCode != nil {
		t.Fatalf("ToolResult.ExitCode = %v, want nil (no synthetic exit code)", *result.ExitCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Result, &payload); err != nil {
		t.Fatalf("Unmarshal(result) error = %v", err)
	}
	if payload["is_error"] != false {
		t.Fatalf("result.is_error = %v, want false", payload["is_error"])
	}
}

func TestUnknownTypeMapsToCoreUnknown(t *testing.T) {
	events, err := ParseRaw(testSalt, []byte(`{"type":"future-event","payload_hash":"redacted"}`), "future.jsonl", 0)
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
	events, err := ParseRaw(testSalt, []byte(`{"type":"started","sessionId":"session-a","timestamp":"2026-01-02T03:04:05Z"}`), `testdata\fixtures\session.jsonl`, 0)
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

// Regression: a single malformed line must not abandon the rest of the transcript.
func TestMalformedLineIsSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := strings.Join([]string{
		`{"type":"user","uuid":"u1","sessionId":"s","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"first"}}`,
		`{not valid json`,
		`{"type":"user","uuid":"u2","sessionId":"s","timestamp":"2026-01-01T00:00:02Z","message":{"role":"user","content":"second"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	events, err := ParseFile(testSalt, path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2 (malformed line skipped, others kept)", len(events))
	}
	for _, event := range events {
		if _, ok := event.(*core.UserPrompt); !ok {
			t.Fatalf("event = %T, want *core.UserPrompt", event)
		}
	}
}

// Regression: discovery must reach subagent / workflow transcripts nested under
// <session>/subagents/…, not just the top-level main transcript.
func TestDiscoverRecursesIntoSubagents(t *testing.T) {
	root := t.TempDir()
	slug := "-home-atharva-promptfoolab"
	projectDir := filepath.Join(root, "projects", slug)
	mainPath := filepath.Join(projectDir, "session-a.jsonl")
	subagentPath := filepath.Join(projectDir, "session-a", "subagents", "agent-b.jsonl")
	if err := os.MkdirAll(filepath.Dir(subagentPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for _, p := range []string{mainPath, subagentPath} {
		if err := os.WriteFile(p, []byte(`{"type":"started","sessionId":"s","timestamp":"2026-01-01T00:00:00Z"}`+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", p, err)
		}
	}

	got, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(Discover()) = %d, want 2 (main + subagent)", len(got))
	}

	paths := map[string]bool{}
	for _, transcript := range got {
		paths[transcript.Path] = true
		if transcript.ProjectSlug != slug {
			t.Fatalf("ProjectSlug = %q, want %q", transcript.ProjectSlug, slug)
		}
		if transcript.RepoPath != ProjectSlugToRepoPath(slug) {
			t.Fatalf("RepoPath = %q, want %q", transcript.RepoPath, ProjectSlugToRepoPath(slug))
		}
	}
	if !paths[mainPath] || !paths[subagentPath] {
		t.Fatalf("Discover() = %v, want both %q and %q", paths, mainPath, subagentPath)
	}
}

// Regression: capture must resume from the byte-offset cursor instead of
// re-reading and re-emitting the whole transcript on every trigger.
func TestCaptureResumesFromCursor(t *testing.T) {
	root := t.TempDir()
	state := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-w-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(projectDir, "session.jsonl")
	initial := `{"type":"user","uuid":"u1","sessionId":"s","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"first"}}` + "\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	adapter := Adapter{Root: root, StateDir: state}

	first := collectEvents(adapter.Capture(core.CaptureTriggerStop))
	if len(first) != 1 {
		t.Fatalf("first capture = %d events, want 1", len(first))
	}

	second := collectEvents(adapter.Capture(core.CaptureTriggerStop))
	if len(second) != 0 {
		t.Fatalf("second capture = %d events, want 0 (cursor resumed)", len(second))
	}

	appended := `{"type":"user","uuid":"u2","sessionId":"s","timestamp":"2026-01-01T00:00:02Z","message":{"role":"user","content":"second"}}` + "\n"
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := file.WriteString(appended); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	third := collectEvents(adapter.Capture(core.CaptureTriggerStop))
	if len(third) != 1 {
		t.Fatalf("third capture = %d events, want 1 (only the appended record)", len(third))
	}
	if _, ok := third[0].(*core.UserPrompt); !ok {
		t.Fatalf("third capture event = %T, want *core.UserPrompt", third[0])
	}
}

func TestLoadOrCreateSaltIsStableAndPrivate(t *testing.T) {
	dir := t.TempDir()
	first, err := loadOrCreateSalt(dir)
	if err != nil {
		t.Fatalf("loadOrCreateSalt() error = %v", err)
	}
	if len(first) < minSaltBytes {
		t.Fatalf("salt length = %d, want >= %d", len(first), minSaltBytes)
	}

	second, err := loadOrCreateSalt(dir)
	if err != nil {
		t.Fatalf("loadOrCreateSalt() second error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("salt is not stable across loads")
	}

	info, err := os.Stat(filepath.Join(dir, saltFileName))
	if err != nil {
		t.Fatalf("Stat(salt) error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Fatalf("salt file perm = %o, want 600", perm)
	}
}

// Different salts must produce different hashes for the same content, proving the
// salt actually participates in the digest.
func TestSaltChangesHash(t *testing.T) {
	line := []byte(`{"type":"user","uuid":"u1","sessionId":"s","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"same content"}}`)
	a, err := ParseRaw([]byte("salt-a"), line, "p.jsonl", 0)
	if err != nil {
		t.Fatalf("ParseRaw(a) error = %v", err)
	}
	b, err := ParseRaw([]byte("salt-b"), line, "p.jsonl", 0)
	if err != nil {
		t.Fatalf("ParseRaw(b) error = %v", err)
	}
	ha := a[0].(*core.UserPrompt).PromptHash
	hb := b[0].(*core.UserPrompt).PromptHash
	if ha == "" || ha == hb {
		t.Fatalf("expected distinct salted hashes, got %q and %q", ha, hb)
	}
}

func collectEvents(seq func(func(core.NormalizedEvent) bool)) []core.NormalizedEvent {
	var out []core.NormalizedEvent
	seq(func(event core.NormalizedEvent) bool {
		out = append(out, event)
		return true
	})
	return out
}

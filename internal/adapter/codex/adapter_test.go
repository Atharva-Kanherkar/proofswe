package codex

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/google/go-cmp/cmp"
)

var update = flag.Bool("update", false, "update golden files")

var testSalt = []byte("proofswe-fixture-salt-v1")

func fixturePath() string {
	return filepath.Join("testdata", "fixtures", "rollout-2026-06-01T00-00-00-session-fixture.jsonl")
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
		"do not keep this",
	} {
		if strings.Contains(text, plaintext) {
			t.Fatalf("normalized output leaked raw content %q", plaintext)
		}
	}
}

func TestSessionIndexEnumeration(t *testing.T) {
	sessions, err := EnumerateSessionIndex(filepath.Join("testdata", "fixtures"))
	if err != nil {
		t.Fatalf("EnumerateSessionIndex() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(sessions))
	}
	if sessions[0].ID != "session-a" || sessions[0].ThreadName != "First redacted task" {
		t.Fatalf("sessions[0] = %+v, want session-a", sessions[0])
	}
	if sessions[1].ID != "session-b" || sessions[1].ThreadName != "Second redacted task" {
		t.Fatalf("sessions[1] = %+v, want session-b", sessions[1])
	}
}

func TestDiscoverRollouts(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "sessions", "2026", "06", "01", "rollout-2026-06-01T00-00-00-session-a.jsonl")
	second := filepath.Join(root, "sessions", "2026", "06", "02", "rollout-2026-06-02T00-00-00-session-b.jsonl")
	ignored := filepath.Join(root, "sessions", "2026", "06", "02", "notes.jsonl")
	for _, path := range []string{first, second, ignored} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	got, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(Discover()) = %d, want 2", len(got))
	}
	if got[0].Path != first || got[1].Path != second {
		t.Fatalf("Discover paths = %#v, want sorted rollout paths", got)
	}
	if got[0].SessionID != "session-a" || got[1].SessionID != "session-b" {
		t.Fatalf("Discover session IDs = %q, %q; want session-a/session-b", got[0].SessionID, got[1].SessionID)
	}
}

func TestTaskCompleteDoesNotMapToSessionEnd(t *testing.T) {
	events, err := ParseRaw(testSalt, []byte(`{"timestamp":"2026-06-01T00:00:00Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1","duration_ms":10}}`), "probe.jsonl", 0)
	if err != nil {
		t.Fatalf("ParseRaw() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if _, ok := events[0].(*core.SessionEnd); ok {
		t.Fatalf("task_complete event = %T, must not be *core.SessionEnd", events[0])
	}
	unknown, ok := events[0].(core.Unknown)
	if !ok {
		t.Fatalf("task_complete event = %T, want core.Unknown", events[0])
	}
	if unknown.Type != "event_msg.task_complete" {
		t.Fatalf("Unknown.Type = %q, want event_msg.task_complete", unknown.Type)
	}
}

func TestResponseItemKindsMapToEventsOrUnknown(t *testing.T) {
	cases := []struct {
		name string
		line string
		want any
	}{
		{
			name: "user message",
			line: `{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`,
			want: &core.UserPrompt{},
		},
		{
			name: "assistant message",
			line: `{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}`,
			want: &core.AssistantMessage{},
		},
		{
			name: "function call",
			line: `{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"c1","arguments":"{\"cmd\":\"date\"}"}}`,
			want: &core.ToolCall{},
		},
		{
			name: "custom tool call",
			line: `{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","call_id":"c1","arguments":{"patch":"secret"}}}`,
			want: &core.ToolCall{},
		},
		{
			name: "function output",
			line: `{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"secret output"}}`,
			want: &core.ToolResult{},
		},
		{
			name: "reasoning",
			line: `{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"reasoning","summary":["secret thought"]}}`,
			want: core.Unknown{},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			events, err := ParseRaw(testSalt, []byte(tt.line), "probe.jsonl", 0)
			if err != nil {
				t.Fatalf("ParseRaw() error = %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("len(events) = %d, want 1", len(events))
			}
			switch tt.want.(type) {
			case *core.UserPrompt:
				if _, ok := events[0].(*core.UserPrompt); !ok {
					t.Fatalf("event = %T, want *core.UserPrompt", events[0])
				}
			case *core.AssistantMessage:
				if _, ok := events[0].(*core.AssistantMessage); !ok {
					t.Fatalf("event = %T, want *core.AssistantMessage", events[0])
				}
			case *core.ToolCall:
				if _, ok := events[0].(*core.ToolCall); !ok {
					t.Fatalf("event = %T, want *core.ToolCall", events[0])
				}
			case *core.ToolResult:
				if _, ok := events[0].(*core.ToolResult); !ok {
					t.Fatalf("event = %T, want *core.ToolResult", events[0])
				}
			case core.Unknown:
				if _, ok := events[0].(core.Unknown); !ok {
					t.Fatalf("event = %T, want core.Unknown", events[0])
				}
			}
		})
	}
}

func TestCaptureResumesFromCursor(t *testing.T) {
	root := t.TempDir()
	state := t.TempDir()
	sessionDir := filepath.Join(root, "sessions", "2026", "06", "01")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(sessionDir, "rollout-2026-06-01T00-00-00-session-a.jsonl")
	initial := `{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]}}` + "\n"
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
		t.Fatalf("second capture = %d events, want 0", len(second))
	}

	appended := `{"timestamp":"2026-06-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"second"}]}}` + "\n"
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
		t.Fatalf("third capture = %d events, want 1", len(third))
	}
	if _, ok := third[0].(*core.UserPrompt); !ok {
		t.Fatalf("third event = %T, want *core.UserPrompt", third[0])
	}
}

func TestMalformedLineIsSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-2026-06-01T00-00-00-session-a.jsonl")
	content := strings.Join([]string{
		`{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]}}`,
		`{not valid json`,
		`{"timestamp":"2026-06-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"second"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	events, err := ParseFile(testSalt, path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
}

func TestSanitizationDropsSensitiveContent(t *testing.T) {
	secrets := []string{
		"AKIAIOSFODNN7EXAMPLE",
		"-----BEGIN PRIVATE KEY-----",
		"/Users/victim/secret-project",
		"hunter2-password",
		"sk-proj-supersecrettoken",
	}
	lines := []string{
		`{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"my key is AKIAIOSFODNN7EXAMPLE"}]}}`,
		`{"timestamp":"2026-06-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"-----BEGIN PRIVATE KEY-----"}]}}`,
		`{"timestamp":"2026-06-01T00:00:02Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"c1","arguments":"{\"cmd\":\"export TOKEN=sk-proj-supersecrettoken\"}"}}`,
		`{"timestamp":"2026-06-01T00:00:03Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"login failed for hunter2-password in /Users/victim/secret-project"}}`,
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

func collectEvents(seq func(func(core.NormalizedEvent) bool)) []core.NormalizedEvent {
	var out []core.NormalizedEvent
	seq(func(event core.NormalizedEvent) bool {
		out = append(out, event)
		return true
	})
	return out
}

package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"pgregory.net/rapid"
)

func TestUnmarshalNormalizedEventDispatchesModeledVariants(t *testing.T) {
	tests := []struct {
		eventType EventType
		want      NormalizedEvent
	}{
		{EventTypeSessionStart, &SessionStart{}},
		{EventTypeUserPrompt, &UserPrompt{}},
		{EventTypeAssistantMessage, &AssistantMessage{}},
		{EventTypeToolCall, &ToolCall{}},
		{EventTypeToolResult, &ToolResult{}},
		{EventTypeSessionEnd, &SessionEnd{}},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			got, err := UnmarshalNormalizedEvent([]byte(fmt.Sprintf(`{"type":%q}`, tt.eventType)))
			if err != nil {
				t.Fatalf("UnmarshalNormalizedEvent() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.IgnoreFields(Envelope{}, "SchemaVersion", "Type")); diff != "" {
				t.Fatalf("decoded variant mismatch (-want +got):\n%s", diff)
			}
			if got.EventType() != tt.eventType {
				t.Fatalf("EventType() = %q, want %q", got.EventType(), tt.eventType)
			}
		})
	}
}

func TestUnmodeledInputDecodesToUnknownWithoutError(t *testing.T) {
	raw := []byte(`{"schema_version":1,"type":"future_event","payload":{"kept":true}}`)

	got, err := UnmarshalNormalizedEvent(raw)
	if err != nil {
		t.Fatalf("UnmarshalNormalizedEvent() error = %v", err)
	}

	unknown, ok := got.(Unknown)
	if !ok {
		t.Fatalf("UnmarshalNormalizedEvent() = %T, want Unknown", got)
	}
	if unknown.Type != "future_event" {
		t.Fatalf("Unknown.Type = %q, want future_event", unknown.Type)
	}
	if diff := cmp.Diff(json.RawMessage(raw), unknown.Raw); diff != "" {
		t.Fatalf("Unknown.Raw mismatch (-want +got):\n%s", diff)
	}
}

func TestZeroValueEnvelopeOmitsOptionalMetadata(t *testing.T) {
	data, err := MarshalNormalizedEvent(&SessionStart{Envelope: Envelope{
		Source:  SourceMeta{Harness: "codex"},
		Session: SessionMeta{ID: "session"},
	}})
	if err != nil {
		t.Fatalf("MarshalNormalizedEvent() error = %v", err)
	}

	got := string(data)
	for _, field := range []string{`"model"`, `"event"`, `"metrics"`, `"ts"`} {
		if strings.Contains(got, field) {
			t.Fatalf("marshaled zero-value envelope contains %s: %s", field, got)
		}
	}
}

func TestProofsweErrorKindAndWrapping(t *testing.T) {
	cause := errors.New("bad discriminator")
	err := NewError(ErrorKindInvalidEvent, "decode event", cause)

	var proofsweErr *ProofsweError
	if !errors.As(err, &proofsweErr) {
		t.Fatalf("errors.As did not find ProofsweError")
	}
	if proofsweErr.Kind() != ErrorKindInvalidEvent {
		t.Fatalf("Kind() = %q, want %q", proofsweErr.Kind(), ErrorKindInvalidEvent)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is did not match wrapped cause")
	}
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("errors.Is did not match sentinel")
	}
}

func TestSourceAdapterShape(t *testing.T) {
	var adapter SourceAdapter = fakeAdapter{}

	if err := adapter.Detect(); err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if err := adapter.Enable(); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if err := adapter.Disable(); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}

	var count int
	for event := range adapter.Capture(CaptureTriggerStop) {
		if event.EventType() != EventTypeSessionStart {
			t.Fatalf("Capture yielded %q, want %q", event.EventType(), EventTypeSessionStart)
		}
		count++
	}
	if count != 1 {
		t.Fatalf("Capture yielded %d events, want 1", count)
	}
}

func TestNormalizedEventRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		event := rapid.OneOf(
			rapid.Custom(func(t *rapid.T) NormalizedEvent {
				return &SessionStart{Envelope: testEnvelope(EventTypeSessionStart, t)}
			}),
			rapid.Custom(func(t *rapid.T) NormalizedEvent {
				return &UserPrompt{
					Envelope:   testEnvelope(EventTypeUserPrompt, t),
					PromptHash: rapid.StringMatching(`[a-f0-9]{16}`).Draw(t, "prompt_hash"),
				}
			}),
			rapid.Custom(func(t *rapid.T) NormalizedEvent {
				return &AssistantMessage{
					Envelope:    testEnvelope(EventTypeAssistantMessage, t),
					MessageHash: rapid.StringMatching(`[a-f0-9]{16}`).Draw(t, "message_hash"),
				}
			}),
			rapid.Custom(func(t *rapid.T) NormalizedEvent {
				return &ToolCall{
					Envelope:   testEnvelope(EventTypeToolCall, t),
					ToolCallID: ToolCallId(rapid.StringMatching(`tc_[a-z0-9]{4}`).Draw(t, "tool_call_id")),
					Name:       rapid.SampledFrom([]string{"bash", "apply_patch", "read_file"}).Draw(t, "tool_name"),
					Arguments: map[string]any{
						"command": rapid.SampledFrom([]string{"status", "test", "build"}).Draw(t, "command"),
					},
				}
			}),
			rapid.Custom(func(t *rapid.T) NormalizedEvent {
				exitCode := rapid.IntRange(0, 127).Draw(t, "exit_code")
				return &ToolResult{
					Envelope:   testEnvelope(EventTypeToolResult, t),
					ToolCallID: ToolCallId(rapid.StringMatching(`tc_[a-z0-9]{4}`).Draw(t, "tool_call_id")),
					ExitCode:   &exitCode,
					Result:     json.RawMessage(`{"ok":true}`),
				}
			}),
			rapid.Custom(func(t *rapid.T) NormalizedEvent {
				return &SessionEnd{
					Envelope: testEnvelope(EventTypeSessionEnd, t),
					Reason:   rapid.SampledFrom([]string{"stop", "session_end", "cancelled"}).Draw(t, "reason"),
				}
			}),
			rapid.Custom(func(t *rapid.T) NormalizedEvent {
				eventType := EventType(rapid.StringMatching(`future_[a-z]{4}`).Draw(t, "future_type"))
				raw := json.RawMessage(fmt.Sprintf(`{"schema_version":1,"type":%q,"payload":{"n":1}}`, eventType))
				return Unknown{Type: eventType, Raw: raw}
			}),
		).Draw(t, "event")

		data, err := MarshalNormalizedEvent(event)
		if err != nil {
			t.Fatalf("MarshalNormalizedEvent() error = %v", err)
		}
		got, err := UnmarshalNormalizedEvent(data)
		if err != nil {
			t.Fatalf("UnmarshalNormalizedEvent() error = %v", err)
		}
		if diff := cmp.Diff(event, got); diff != "" {
			t.Fatalf("round trip mismatch (-want +got):\n%s\njson: %s", diff, data)
		}
	})
}

type fakeAdapter struct{}

func (fakeAdapter) Detect() error  { return nil }
func (fakeAdapter) Enable() error  { return nil }
func (fakeAdapter) Disable() error { return nil }

func (fakeAdapter) Capture(CaptureTrigger) iter.Seq[NormalizedEvent] {
	return func(yield func(NormalizedEvent) bool) {
		yield(&SessionStart{Envelope: Envelope{
			SchemaVersion: SchemaVersion,
			Type:          EventTypeSessionStart,
			Source:        SourceMeta{Harness: "test"},
			Session:       SessionMeta{ID: "session"},
		}})
	}
}

func testEnvelope(eventType EventType, t *rapid.T) Envelope {
	return Envelope{
		SchemaVersion: SchemaVersion,
		Type:          eventType,
		Source: SourceMeta{
			Harness: HarnessName(rapid.SampledFrom([]string{"codex", "claudecode"}).Draw(t, "harness")),
			Path:    rapid.StringMatching(`/tmp/session-[a-z]{3}\.jsonl`).Draw(t, "path"),
		},
		Session: SessionMeta{
			ID:  SessionId(rapid.StringMatching(`s_[a-z0-9]{6}`).Draw(t, "session_id")),
			CWD: rapid.StringMatching(`/repo/[a-z]{4}`).Draw(t, "cwd"),
		},
		Model: ModelMeta{
			ID: ModelId(rapid.SampledFrom([]string{"gpt-5", "claude-opus-4.1", "codex-mini"}).Draw(t, "model_id")),
		},
		Event: EventMeta{
			ID:        rapid.StringMatching(`e_[a-z0-9]{6}`).Draw(t, "event_id"),
			Timestamp: time.Unix(rapid.Int64Range(0, 1_999_999_999).Draw(t, "unix_ts"), 0).UTC(),
		},
		Metrics: Metrics{
			InputTokens:  rapid.Int64Range(0, 100_000).Draw(t, "input_tokens"),
			OutputTokens: rapid.Int64Range(0, 100_000).Draw(t, "output_tokens"),
			DurationMS:   rapid.Int64Range(0, 600_000).Draw(t, "duration_ms"),
		},
	}
}

package core

import (
	"encoding/json"
	"fmt"
	"time"
)

const SchemaVersion = 1

type EventType string

const (
	EventTypeSessionStart     EventType = "session_start"
	EventTypeUserPrompt       EventType = "user_prompt"
	EventTypeAssistantMessage EventType = "assistant_message"
	EventTypeToolCall         EventType = "tool_call"
	EventTypeToolResult       EventType = "tool_result"
	EventTypeSessionEnd       EventType = "session_end"
)

//sumtype:decl
type NormalizedEvent interface {
	normalizedEvent()
	EventType() EventType
}

type SourceMeta struct {
	Harness HarnessName `json:"harness" jsonschema:"title=Harness"`
	Path    string      `json:"path,omitempty" jsonschema:"title=Transcript path"`
}

type SessionMeta struct {
	ID  SessionId `json:"id" jsonschema:"title=Session ID"`
	CWD string    `json:"cwd,omitempty" jsonschema:"title=Working directory"`
}

type ModelMeta struct {
	ID ModelId `json:"id,omitempty" jsonschema:"title=Model ID"`
}

type EventMeta struct {
	ID        string    `json:"id,omitempty" jsonschema:"title=Event ID"`
	Timestamp time.Time `json:"ts,omitzero" jsonschema:"title=Timestamp"`
}

type Metrics struct {
	InputTokens  int64 `json:"input_tokens,omitempty" jsonschema:"minimum=0"`
	OutputTokens int64 `json:"output_tokens,omitempty" jsonschema:"minimum=0"`
	DurationMS   int64 `json:"duration_ms,omitempty" jsonschema:"minimum=0"`
}

type Envelope struct {
	SchemaVersion int         `json:"schema_version" jsonschema:"enum=1"`
	Type          EventType   `json:"type"`
	Source        SourceMeta  `json:"source"`
	Session       SessionMeta `json:"session"`
	Model         ModelMeta   `json:"model,omitzero"`
	Event         EventMeta   `json:"event,omitzero"`
	Metrics       Metrics     `json:"metrics,omitzero"`
}

type SessionStart struct {
	Envelope
}

type UserPrompt struct {
	Envelope
	PromptHash string `json:"prompt_hash,omitempty" jsonschema:"title=Prompt hash"`
}

type AssistantMessage struct {
	Envelope
	MessageHash string `json:"message_hash,omitempty" jsonschema:"title=Message hash"`
}

type ToolCall struct {
	Envelope
	ToolCallID ToolCallId     `json:"tool_call_id" jsonschema:"title=Tool call ID"`
	Name       string         `json:"name,omitempty" jsonschema:"title=Tool name"`
	Arguments  map[string]any `json:"arguments,omitempty" jsonschema:"title=Tool arguments"`
}

type ToolResult struct {
	Envelope
	ToolCallID ToolCallId      `json:"tool_call_id" jsonschema:"title=Tool call ID"`
	ExitCode   *int            `json:"exit_code,omitempty" jsonschema:"title=Exit code"`
	Result     json.RawMessage `json:"result,omitempty" jsonschema:"title=Tool result"`
}

type SessionEnd struct {
	Envelope
	Reason string `json:"reason,omitempty" jsonschema:"title=End reason"`
}

type Unknown struct {
	Type EventType       `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

func (SessionStart) normalizedEvent()     {}
func (UserPrompt) normalizedEvent()       {}
func (AssistantMessage) normalizedEvent() {}
func (ToolCall) normalizedEvent()         {}
func (ToolResult) normalizedEvent()       {}
func (SessionEnd) normalizedEvent()       {}
func (Unknown) normalizedEvent()          {}

func (SessionStart) EventType() EventType     { return EventTypeSessionStart }
func (UserPrompt) EventType() EventType       { return EventTypeUserPrompt }
func (AssistantMessage) EventType() EventType { return EventTypeAssistantMessage }
func (ToolCall) EventType() EventType         { return EventTypeToolCall }
func (ToolResult) EventType() EventType       { return EventTypeToolResult }
func (SessionEnd) EventType() EventType       { return EventTypeSessionEnd }
func (e Unknown) EventType() EventType        { return e.Type }

func MarshalNormalizedEvent(event NormalizedEvent) ([]byte, error) {
	if event == nil {
		return nil, fmt.Errorf("%w: nil normalized event", ErrInvalidEvent)
	}

	return json.Marshal(event)
}

func UnmarshalNormalizedEvent(data []byte) (NormalizedEvent, error) {
	return UnmarshalNormalizedEventWith(data, json.Unmarshal)
}

func UnmarshalNormalizedEventWith(data []byte, unmarshal func([]byte, any) error) (NormalizedEvent, error) {
	if unmarshal == nil {
		return nil, NewError(ErrorKindInvalidEvent, "nil normalized event decoder", nil)
	}

	var probe struct {
		Type EventType `json:"type"`
	}
	if err := unmarshal(data, &probe); err != nil {
		return nil, NewError(ErrorKindInvalidEvent, "decode event discriminator", err)
	}
	if probe.Type == "" {
		return nil, NewError(ErrorKindInvalidEvent, "missing event type", nil)
	}

	switch probe.Type {
	case EventTypeSessionStart:
		return unmarshalModeled[*SessionStart](data, EventTypeSessionStart, unmarshal)
	case EventTypeUserPrompt:
		return unmarshalModeled[*UserPrompt](data, EventTypeUserPrompt, unmarshal)
	case EventTypeAssistantMessage:
		return unmarshalModeled[*AssistantMessage](data, EventTypeAssistantMessage, unmarshal)
	case EventTypeToolCall:
		return unmarshalModeled[*ToolCall](data, EventTypeToolCall, unmarshal)
	case EventTypeToolResult:
		return unmarshalModeled[*ToolResult](data, EventTypeToolResult, unmarshal)
	case EventTypeSessionEnd:
		return unmarshalModeled[*SessionEnd](data, EventTypeSessionEnd, unmarshal)
	default:
		raw := append(json.RawMessage(nil), data...)
		return Unknown{Type: probe.Type, Raw: raw}, nil
	}
}

func unmarshalModeled[T interface {
	NormalizedEvent
	ensureEnvelope(EventType)
	*SessionStart | *UserPrompt | *AssistantMessage | *ToolCall | *ToolResult | *SessionEnd
}](data []byte, eventType EventType, unmarshal func([]byte, any) error) (NormalizedEvent, error) {
	event := newModeled[T]()
	if err := unmarshal(data, event); err != nil {
		return nil, NewError(ErrorKindInvalidEvent, "decode normalized event", err)
	}
	event.ensureEnvelope(eventType)
	return event, nil
}

func newModeled[T interface {
	*SessionStart | *UserPrompt | *AssistantMessage | *ToolCall | *ToolResult | *SessionEnd
}]() T {
	var event T
	switch any(event).(type) {
	case *SessionStart:
		return any(&SessionStart{}).(T)
	case *UserPrompt:
		return any(&UserPrompt{}).(T)
	case *AssistantMessage:
		return any(&AssistantMessage{}).(T)
	case *ToolCall:
		return any(&ToolCall{}).(T)
	case *ToolResult:
		return any(&ToolResult{}).(T)
	case *SessionEnd:
		return any(&SessionEnd{}).(T)
	default:
		panic("unreachable modeled event type")
	}
}

func (e *SessionStart) ensureEnvelope(t EventType)     { e.ensure(t) }
func (e *UserPrompt) ensureEnvelope(t EventType)       { e.ensure(t) }
func (e *AssistantMessage) ensureEnvelope(t EventType) { e.ensure(t) }
func (e *ToolCall) ensureEnvelope(t EventType)         { e.ensure(t) }
func (e *ToolResult) ensureEnvelope(t EventType)       { e.ensure(t) }
func (e *SessionEnd) ensureEnvelope(t EventType)       { e.ensure(t) }

func (e *Envelope) ensure(t EventType) {
	e.SchemaVersion = SchemaVersion
	e.Type = t
}

func (e SessionStart) MarshalJSON() ([]byte, error) {
	e.ensure(EventTypeSessionStart)
	type alias SessionStart
	return json.Marshal(alias(e))
}

func (e UserPrompt) MarshalJSON() ([]byte, error) {
	e.ensure(EventTypeUserPrompt)
	type alias UserPrompt
	return json.Marshal(alias(e))
}

func (e AssistantMessage) MarshalJSON() ([]byte, error) {
	e.ensure(EventTypeAssistantMessage)
	type alias AssistantMessage
	return json.Marshal(alias(e))
}

func (e ToolCall) MarshalJSON() ([]byte, error) {
	e.ensure(EventTypeToolCall)
	type alias ToolCall
	return json.Marshal(alias(e))
}

func (e ToolResult) MarshalJSON() ([]byte, error) {
	e.ensure(EventTypeToolResult)
	type alias ToolResult
	return json.Marshal(alias(e))
}

func (e SessionEnd) MarshalJSON() ([]byte, error) {
	e.ensure(EventTypeSessionEnd)
	type alias SessionEnd
	return json.Marshal(alias(e))
}

func (e Unknown) MarshalJSON() ([]byte, error) {
	if len(e.Raw) == 0 {
		return json.Marshal(struct {
			Type EventType `json:"type"`
		}{Type: e.Type})
	}
	return append([]byte(nil), e.Raw...), nil
}

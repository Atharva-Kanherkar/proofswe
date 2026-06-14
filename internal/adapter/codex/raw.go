package codex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
)

type parser struct {
	h     hasher
	path  string
	state rolloutState
}

type rolloutState struct {
	sessionID   core.SessionId
	cwd         string
	gitBranch   string
	model       core.ModelId
	turnID      string
	turnIndex   int
	nextMetrics core.Metrics
}

func newParser(salt []byte, path string) *parser {
	return &parser{
		h:    newHasher(salt),
		path: path,
		state: rolloutState{
			sessionID: sessionIDFromRolloutPath(path),
		},
	}
}

func (p *parser) Parse(data []byte) ([]core.NormalizedEvent, error) {
	var raw RawLine
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, core.NewError(core.ErrorKindInvalidEvent, "decode codex record", err)
	}
	events, err := raw.Normalized(p)
	p.state.turnIndex++
	return events, err
}

type RawLine struct {
	Type string

	timestamp   string
	sessionMeta *rawSessionMeta
	turnContext *rawTurnContext
	eventMsg    *rawEventMsg
	response    *rawResponseItem
}

type rawLineCommon struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
}

type rawSessionMeta struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	CWD       string `json:"cwd"`
	Git       struct {
		Branch string `json:"branch"`
	} `json:"git"`
}

type rawTurnContext struct {
	TurnID string `json:"turn_id"`
	CWD    string `json:"cwd"`
	Model  string `json:"model"`
}

type rawEventMsg struct {
	Type                  string          `json:"type"`
	TurnID                string          `json:"turn_id"`
	StartedAt             json.RawMessage `json:"started_at"`
	CompletedAt           json.RawMessage `json:"completed_at"`
	DurationMS            int64           `json:"duration_ms"`
	TimeToFirstTokenMS    int64           `json:"time_to_first_token_ms"`
	CollaborationModeKind string          `json:"collaboration_mode_kind"`
	Info                  struct {
		LastTokenUsage  rawTokenUsage `json:"last_token_usage"`
		TotalTokenUsage rawTokenUsage `json:"total_token_usage"`
	} `json:"info"`
}

type rawTokenUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

type rawResponseItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	CallID    string          `json:"call_id"`
	Output    json.RawMessage `json:"output"`
}

type responseContentBlock struct {
	Type       string `json:"type"`
	Text       string `json:"text"`
	InputText  string `json:"input_text"`
	OutputText string `json:"output_text"`
}

func (l *RawLine) UnmarshalJSON(data []byte) error {
	var common rawLineCommon
	if err := json.Unmarshal(data, &common); err != nil {
		return err
	}
	if common.Type == "" {
		return fmt.Errorf("missing codex record type")
	}

	l.Type = common.Type
	l.timestamp = common.Timestamp
	l.sessionMeta = nil
	l.turnContext = nil
	l.eventMsg = nil
	l.response = nil

	switch common.Type {
	case "session_meta":
		var outer struct {
			Payload rawSessionMeta `json:"payload"`
		}
		if err := json.Unmarshal(data, &outer); err != nil {
			return err
		}
		l.sessionMeta = &outer.Payload
	case "turn_context":
		var outer struct {
			Payload rawTurnContext `json:"payload"`
		}
		if err := json.Unmarshal(data, &outer); err != nil {
			return err
		}
		l.turnContext = &outer.Payload
	case "event_msg":
		var outer struct {
			Payload rawEventMsg `json:"payload"`
		}
		if err := json.Unmarshal(data, &outer); err != nil {
			return err
		}
		l.eventMsg = &outer.Payload
	case "response_item":
		var outer struct {
			Payload rawResponseItem `json:"payload"`
		}
		if err := json.Unmarshal(data, &outer); err != nil {
			return err
		}
		l.response = &outer.Payload
	default:
	}
	return nil
}

func (l RawLine) Normalized(p *parser) ([]core.NormalizedEvent, error) {
	switch {
	case l.sessionMeta != nil:
		return normalizeSessionMeta(l.timestamp, *l.sessionMeta, p), nil
	case l.turnContext != nil:
		if l.turnContext.TurnID != "" {
			p.state.turnID = l.turnContext.TurnID
		}
		if l.turnContext.CWD != "" {
			p.state.cwd = l.turnContext.CWD
		}
		if l.turnContext.Model != "" {
			p.state.model = core.ModelId(l.turnContext.Model)
		}
		return nil, nil
	case l.eventMsg != nil:
		return normalizeEventMsg(l.timestamp, *l.eventMsg, p), nil
	case l.response != nil:
		return normalizeResponseItem(l.timestamp, *l.response, p), nil
	default:
		return []core.NormalizedEvent{core.Unknown{Type: core.EventType(l.Type), Raw: sanitizedUnknown(l.Type)}}, nil
	}
}

func normalizeSessionMeta(ts string, raw rawSessionMeta, p *parser) []core.NormalizedEvent {
	if raw.ID != "" {
		p.state.sessionID = core.SessionId(raw.ID)
	}
	if raw.CWD != "" {
		p.state.cwd = raw.CWD
	}
	if raw.Git.Branch != "" {
		p.state.gitBranch = raw.Git.Branch
	}
	if raw.Timestamp != "" {
		ts = raw.Timestamp
	}
	return []core.NormalizedEvent{&core.SessionStart{Envelope: p.envelope(core.EventTypeSessionStart, ts, string(p.state.sessionID))}}
}

func normalizeEventMsg(ts string, raw rawEventMsg, p *parser) []core.NormalizedEvent {
	if raw.TurnID != "" {
		p.state.turnID = raw.TurnID
	}
	switch raw.Type {
	case "task_started":
		return []core.NormalizedEvent{&core.SessionStart{Envelope: p.envelope(core.EventTypeSessionStart, ts, raw.TurnID)}}
	case "token_count":
		p.state.nextMetrics = metricsFromTokenUsage(raw.Info.LastTokenUsage)
		return nil
	default:
		return []core.NormalizedEvent{core.Unknown{Type: core.EventType("event_msg." + raw.Type), Raw: sanitizedUnknown("event_msg." + raw.Type)}}
	}
}

func normalizeResponseItem(ts string, raw rawResponseItem, p *parser) []core.NormalizedEvent {
	switch raw.Type {
	case "message":
		switch raw.Role {
		case "user":
			hash := messageContentHash(p.h, raw.Content)
			if hash == "" {
				return nil
			}
			return []core.NormalizedEvent{&core.UserPrompt{Envelope: p.envelope(core.EventTypeUserPrompt, ts, ""), PromptHash: hash}}
		case "assistant":
			hash := messageContentHash(p.h, raw.Content)
			if hash == "" {
				return nil
			}
			env := p.envelope(core.EventTypeAssistantMessage, ts, "")
			env.Metrics = p.takeMetrics()
			return []core.NormalizedEvent{&core.AssistantMessage{Envelope: env, MessageHash: hash}}
		default:
			return []core.NormalizedEvent{core.Unknown{Type: core.EventType("response_item.message." + raw.Role), Raw: sanitizedUnknown("response_item.message." + raw.Role)}}
		}
	case "function_call", "custom_tool_call", "web_search_call", "tool_search_call":
		return []core.NormalizedEvent{&core.ToolCall{
			Envelope:   p.envelope(core.EventTypeToolCall, ts, raw.CallID),
			ToolCallID: core.ToolCallId(raw.CallID),
			Name:       raw.Name,
			Arguments:  sanitizedToolInput(p.h, raw.Arguments),
		}}
	case "function_call_output", "custom_tool_call_output", "web_search_output", "tool_search_output":
		return []core.NormalizedEvent{&core.ToolResult{
			Envelope:   p.envelope(core.EventTypeToolResult, ts, raw.CallID),
			ToolCallID: core.ToolCallId(raw.CallID),
			Result:     sanitizedToolOutput(p.h, raw.Output),
		}}
	default:
		return []core.NormalizedEvent{core.Unknown{Type: core.EventType("response_item." + raw.Type), Raw: sanitizedUnknown("response_item." + raw.Type)}}
	}
}

func (p *parser) envelope(eventType core.EventType, ts string, eventID string) core.Envelope {
	parsedTS, _ := parseTimestamp(ts)
	if eventID == "" {
		eventID = fmt.Sprintf("%s:%s:%d", ts, eventType, p.state.turnIndex)
	}
	env := core.Envelope{
		SchemaVersion: core.SchemaVersion,
		Type:          eventType,
		Source: core.SourceMeta{
			Harness:   Harness,
			Path:      normalizePath(p.path),
			GitBranch: p.state.gitBranch,
		},
		Session: core.SessionMeta{
			ID:  p.state.sessionID,
			CWD: p.state.cwd,
		},
		Model: core.ModelMeta{
			ID: p.state.model,
		},
		Event: core.EventMeta{
			ID:        eventID,
			Timestamp: parsedTS,
			TurnIndex: p.state.turnIndex,
		},
	}
	if env.Model.ID == "" {
		env.Model = core.ModelMeta{}
	}
	return env
}

func (p *parser) takeMetrics() core.Metrics {
	metrics := p.state.nextMetrics
	p.state.nextMetrics = core.Metrics{}
	return metrics
}

func metricsFromTokenUsage(usage rawTokenUsage) core.Metrics {
	return core.Metrics{
		InputTokens:          usage.InputTokens,
		OutputTokens:         usage.OutputTokens + usage.ReasoningOutputTokens,
		CacheReadInputTokens: usage.CachedInputTokens,
	}
}

func messageContentHash(h hasher, raw json.RawMessage) string {
	blocks := decodeContentBlocks(raw)
	if len(blocks) == 0 {
		return h.rawHash(raw)
	}
	var hashes []string
	for _, block := range blocks {
		text := block.Text
		if text == "" {
			text = block.InputText
		}
		if text == "" {
			text = block.OutputText
		}
		if text != "" {
			hashes = append(hashes, h.stringHash(text))
		}
	}
	if len(hashes) == 0 {
		return ""
	}
	joined, _ := json.Marshal(hashes)
	return h.rawHash(joined)
}

func decodeContentBlocks(raw json.RawMessage) []responseContentBlock {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var blocks []responseContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return blocks
	}
	return nil
}

func sanitizedToolInput(h hasher, raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil {
		return sanitizedObjectHash(h, raw, object, "argument")
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal([]byte(asString), &nested); err == nil {
			return sanitizedObjectHash(h, json.RawMessage(asString), nested, "argument")
		}
	}

	return map[string]any{
		"argument_hash": h.rawHash(raw),
		"argument_type": contentJSONType(raw),
	}
}

func sanitizedObjectHash(h hasher, raw json.RawMessage, object map[string]json.RawMessage, prefix string) map[string]any {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return map[string]any{
		prefix + "_hash": h.rawHash(raw),
		prefix + "_keys": keys,
	}
}

func sanitizedToolOutput(h hasher, raw json.RawMessage) json.RawMessage {
	payload := map[string]any{
		"output_hash": h.rawHash(raw),
		"output_type": contentJSONType(raw),
	}
	data, _ := json.Marshal(payload)
	return data
}

func sanitizedUnknown(eventType string) json.RawMessage {
	data, _ := json.Marshal(struct {
		Type string `json:"type"`
	}{Type: eventType})
	return data
}

func contentJSONType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "invalid"
	}
	switch value.(type) {
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

func parseTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts, nil
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(seconds, 0).UTC(), nil
	}
	if strings.Contains(value, ".") {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			sec, frac := mathModf(f)
			return time.Unix(int64(sec), int64(frac*1e9)).UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse timestamp %q", value)
}

func mathModf(f float64) (float64, float64) {
	whole := float64(int64(f))
	return whole, f - whole
}

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
	// held is the most-recent assistant message, kept back (O(1), exactly one)
	// until the next assistant / user prompt / session boundary / EOF so an
	// end-of-turn token_count can attach to it. We never buffer the whole turn —
	// that would make memory grow with the transcript and break the constant-
	// memory invariant for multi-hundred-MB rollouts.
	held *core.AssistantMessage
	// carry holds token deltas seen before any assistant was available to receive
	// them; drained onto the next held assistant.
	carry core.Metrics
}

type rolloutState struct {
	sessionID core.SessionId
	cwd       string
	gitBranch string
	model     core.ModelId
	turnID    string
	turnIndex int
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

func (p *parser) Flush() []core.NormalizedEvent {
	return p.flushHeld()
}

// flushHeld emits the held assistant message (if any). Called at turn / session
// boundaries and at EOF so the last assistant of every turn is emitted exactly
// once, after any end-of-turn token_count has been folded into its metrics.
func (p *parser) flushHeld() []core.NormalizedEvent {
	if p.held == nil {
		return nil
	}
	msg := p.held
	p.held = nil
	return []core.NormalizedEvent{msg}
}

// holdAssistant emits the previously-held assistant and holds the new one,
// draining any carried token deltas onto it.
func (p *parser) holdAssistant(msg *core.AssistantMessage) []core.NormalizedEvent {
	emitted := p.flushHeld()
	msg.Metrics = addMetrics(msg.Metrics, p.carry)
	p.carry = core.Metrics{}
	p.held = msg
	return emitted
}

// addTurnMetrics folds an end-of-turn token_count delta into the held assistant
// (or carries it forward if none is held yet). Deltas accumulate, so multiple
// token_counts in one turn sum correctly and never double-count the cumulative.
func (p *parser) addTurnMetrics(delta core.Metrics) {
	if delta == (core.Metrics{}) {
		return
	}
	if p.held != nil {
		p.held.Metrics = addMetrics(p.held.Metrics, delta)
		return
	}
	p.carry = addMetrics(p.carry, delta)
}

func addMetrics(a, b core.Metrics) core.Metrics {
	return core.Metrics{
		InputTokens:              a.InputTokens + b.InputTokens,
		OutputTokens:             a.OutputTokens + b.OutputTokens,
		CacheCreationInputTokens: a.CacheCreationInputTokens + b.CacheCreationInputTokens,
		CacheReadInputTokens:     a.CacheReadInputTokens + b.CacheReadInputTokens,
		DurationMS:               a.DurationMS + b.DurationMS,
	}
}

func (p *parser) setState(state rolloutState) {
	if state.sessionID != "" {
		p.state.sessionID = state.sessionID
	}
	if state.cwd != "" {
		p.state.cwd = state.cwd
	}
	if state.gitBranch != "" {
		p.state.gitBranch = state.gitBranch
	}
	if state.model != "" {
		p.state.model = state.model
	}
	if state.turnID != "" {
		p.state.turnID = state.turnID
	}
	p.state.turnIndex = state.turnIndex
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
	events := p.Flush()
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
	events = append(events, &core.SessionStart{Envelope: p.envelope(core.EventTypeSessionStart, ts, string(p.state.sessionID))})
	return events
}

func normalizeEventMsg(ts string, raw rawEventMsg, p *parser) []core.NormalizedEvent {
	if raw.TurnID != "" {
		p.state.turnID = raw.TurnID
	}
	switch raw.Type {
	case "task_started", "task_complete":
		// Turn boundaries: do NOT flush the held assistant. token_count usually
		// arrives around here, and the held assistant must still be available to
		// receive it; it is flushed by the next assistant / user prompt / EOF.
		return nil
	case "token_count":
		p.addTurnMetrics(metricsFromTokenUsage(raw.Info.LastTokenUsage))
		return nil
	case "agent_message", "user_message":
		// Redundant with response_item/message; skip rather than emit Unknown noise.
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
			// A user prompt opens a new turn: flush the prior turn's held assistant.
			events := p.flushHeld()
			hash := messageContentHash(p.h, raw.Content)
			if hash == "" {
				return events
			}
			return append(events, &core.UserPrompt{Envelope: p.envelope(core.EventTypeUserPrompt, ts, ""), PromptHash: hash})
		case "assistant":
			hash := messageContentHash(p.h, raw.Content)
			if hash == "" {
				return nil
			}
			env := p.envelope(core.EventTypeAssistantMessage, ts, "")
			return p.holdAssistant(&core.AssistantMessage{Envelope: env, MessageHash: hash})
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
	case "reasoning":
		return nil
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

func metricsFromTokenUsage(usage rawTokenUsage) core.Metrics {
	// Codex/OpenAI input_tokens is the total input; cached_input_tokens is a
	// subset surfaced separately for downstream cache-hit accounting.
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

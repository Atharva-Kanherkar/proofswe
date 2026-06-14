package claudecode

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
)

type RawEvent struct {
	Type string

	started   *rawCommon
	user      *rawUser
	assistant *rawAssistant
	result    *rawResult
}

type rawCommon struct {
	Type        string `json:"type"`
	UUID        string `json:"uuid"`
	ParentUUID  string `json:"parentUuid"`
	SessionID   string `json:"sessionId"`
	Timestamp   string `json:"timestamp"`
	CWD         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	IsSidechain bool   `json:"isSidechain"`
}

type rawUser struct {
	rawCommon
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type rawAssistant struct {
	rawCommon
	Message struct {
		ID      string          `json:"id"`
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   rawUsage        `json:"usage"`
	} `json:"message"`
}

type rawUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type rawResult struct {
	rawCommon
	Subtype string `json:"subtype"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   *bool           `json:"is_error"`
}

func (e *RawEvent) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Type == "" {
		return fmt.Errorf("missing claude code record type")
	}

	e.Type = probe.Type
	e.started = nil
	e.user = nil
	e.assistant = nil
	e.result = nil

	// Only the records that carry agent work are modeled; everything else
	// (system, last-prompt, pr-link, attachment, …) falls through to core.Unknown
	// so the waist stays small and old binaries survive new record types.
	switch probe.Type {
	case "started":
		var event rawCommon
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		e.started = &event
	case "user":
		var event rawUser
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		e.user = &event
	case "assistant":
		var event rawAssistant
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		e.assistant = &event
	case "result":
		var event rawResult
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		e.result = &event
	default:
	}
	return nil
}

func (e RawEvent) Normalized(h hasher, path string, turnIndex int) ([]core.NormalizedEvent, error) {
	switch {
	case e.started != nil:
		return []core.NormalizedEvent{&core.SessionStart{Envelope: envelope(*e.started, core.EventTypeSessionStart, path, turnIndex)}}, nil
	case e.user != nil:
		return normalizeUser(h, *e.user, path, turnIndex), nil
	case e.assistant != nil:
		return normalizeAssistant(h, *e.assistant, path, turnIndex), nil
	case e.result != nil:
		event := &core.SessionEnd{Envelope: envelope(e.result.rawCommon, core.EventTypeSessionEnd, path, turnIndex), Reason: e.result.Subtype}
		return []core.NormalizedEvent{event}, nil
	default:
		return []core.NormalizedEvent{core.Unknown{Type: core.EventType(e.Type), Raw: sanitizedUnknown(e.Type)}}, nil
	}
}

func normalizeUser(h hasher, raw rawUser, path string, turnIndex int) []core.NormalizedEvent {
	blocks := decodeContentBlocks(raw.Message.Content)
	if len(blocks) == 0 {
		hash := h.rawHash(raw.Message.Content)
		if hash == "" {
			return nil
		}
		return []core.NormalizedEvent{&core.UserPrompt{Envelope: envelope(raw.rawCommon, core.EventTypeUserPrompt, path, turnIndex), PromptHash: hash}}
	}

	var events []core.NormalizedEvent
	// A user record is usually either a typed prompt or a batch of tool results,
	// but it can carry both; capture the text blocks as a prompt instead of
	// dropping array-form prompts on the floor.
	if promptHash := userPromptHash(h, blocks); promptHash != "" {
		events = append(events, &core.UserPrompt{Envelope: envelope(raw.rawCommon, core.EventTypeUserPrompt, path, turnIndex), PromptHash: promptHash})
	}
	for _, block := range blocks {
		if block.Type != "tool_result" {
			continue
		}
		events = append(events, &core.ToolResult{
			Envelope:   envelope(raw.rawCommon, core.EventTypeToolResult, path, turnIndex),
			ToolCallID: core.ToolCallId(block.ToolUseID),
			Result:     sanitizedToolResult(h, block),
		})
	}
	return events
}

func normalizeAssistant(h hasher, raw rawAssistant, path string, turnIndex int) []core.NormalizedEvent {
	base := envelope(raw.rawCommon, core.EventTypeAssistantMessage, path, turnIndex)
	base.Model.ID = core.ModelId(raw.Message.Model)
	base.Metrics = core.Metrics{
		InputTokens:              raw.Message.Usage.InputTokens,
		OutputTokens:             raw.Message.Usage.OutputTokens,
		CacheCreationInputTokens: raw.Message.Usage.CacheCreationInputTokens,
		CacheReadInputTokens:     raw.Message.Usage.CacheReadInputTokens,
	}

	blocks := decodeContentBlocks(raw.Message.Content)
	var events []core.NormalizedEvent

	// Token usage is reported once per assistant turn. Attach it to the first
	// event emitted for the turn and zero it on the rest so a per-model token
	// aggregate does not multiply by the number of tool calls in the turn.
	metricsPlaced := false
	if messageHash := assistantMessageHash(h, blocks, raw.Message.Content); messageHash != "" {
		events = append(events, &core.AssistantMessage{Envelope: base, MessageHash: messageHash})
		metricsPlaced = true
	}
	for _, block := range blocks {
		if block.Type != "tool_use" {
			continue
		}
		callEnvelope := base
		callEnvelope.Type = core.EventTypeToolCall
		if metricsPlaced {
			callEnvelope.Metrics = core.Metrics{}
		} else {
			metricsPlaced = true
		}
		events = append(events, &core.ToolCall{
			Envelope:   callEnvelope,
			ToolCallID: core.ToolCallId(block.ID),
			Name:       block.Name,
			Arguments:  sanitizedToolInput(h, block.Input),
		})
	}
	return events
}

func decodeContentBlocks(raw json.RawMessage) []contentBlock {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return blocks
	}
	return nil
}

func userPromptHash(h hasher, blocks []contentBlock) string {
	return joinedTextHash(h, blocks)
}

func assistantMessageHash(h hasher, blocks []contentBlock, raw json.RawMessage) string {
	if len(blocks) == 0 {
		return h.rawHash(raw)
	}
	return joinedTextHash(h, blocks)
}

// joinedTextHash hashes each text block independently, then hashes the ordered
// list of those hashes. Per-block hashing keeps the door open to line-level
// survival matching later without re-hashing raw content.
func joinedTextHash(h hasher, blocks []contentBlock) string {
	var hashes []string
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			hashes = append(hashes, h.stringHash(block.Text))
		}
	}
	if len(hashes) == 0 {
		return ""
	}
	joined, _ := json.Marshal(hashes)
	return h.rawHash(joined)
}

func sanitizedToolInput(h hasher, raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(raw, &input); err != nil {
		return map[string]any{"input_hash": h.rawHash(raw)}
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return map[string]any{
		"input_hash": h.rawHash(raw),
		"input_keys": keys,
	}
}

func sanitizedToolResult(h hasher, block contentBlock) json.RawMessage {
	payload := map[string]any{
		"content_hash": h.rawHash(block.Content),
		"content_type": contentJSONType(block.Content),
	}
	if block.IsError != nil {
		payload["is_error"] = *block.IsError
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

package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultOpenAIModel is a lower-cost model suited to the small judging task.
// Overridable so the model id can change without a code edit.
const DefaultOpenAIModel = "gpt-5.4-mini"

// DefaultAnthropicModel preserves the previous Anthropic judge default.
const DefaultAnthropicModel = "claude-haiku-4-5-20251001"

const (
	anthropicVersion        = "2023-06-01"
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	defaultOpenAIBaseURL    = "https://api.openai.com"
)

// Doer is the slice of *http.Client used here, injectable so tests run offline.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// AnthropicJudge calls the Anthropic Messages API with the blinded prompt. The bias
// safeguard (no model identity in the prompt) is enforced by BuildPrompt.
type AnthropicJudge struct {
	Client  Doer
	APIKey  string
	Model   string
	BaseURL string
}

func (j AnthropicJudge) Assess(ctx context.Context, turns []Turn, skills []string) (Verdict, error) {
	if j.APIKey == "" {
		return Verdict{}, fmt.Errorf("judge: missing API key")
	}
	reqBody, err := json.Marshal(map[string]any{
		"model":      orDefault(j.Model, DefaultAnthropicModel),
		"max_tokens": 256,
		"messages":   []map[string]string{{"role": "user", "content": BuildPrompt(turns, skills)}},
	})
	if err != nil {
		return Verdict{}, err
	}

	url := orDefault(j.BaseURL, defaultAnthropicBaseURL) + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return Verdict{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", j.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	client := j.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Verdict{}, fmt.Errorf("judge: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Verdict{}, fmt.Errorf("judge: api status %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Verdict{}, fmt.Errorf("judge: decode response: %w", err)
	}
	for _, block := range parsed.Content {
		if block.Type == "text" && block.Text != "" {
			return ParseVerdict(block.Text)
		}
	}
	return Verdict{}, fmt.Errorf("judge: empty response")
}

// HTTPJudge is kept as a compatibility alias for the old Anthropic-only judge.
type HTTPJudge = AnthropicJudge

// OpenAIJudge calls the OpenAI Responses API with the same blinded prompt.
type OpenAIJudge struct {
	Client  Doer
	APIKey  string
	Model   string
	BaseURL string
}

func (j OpenAIJudge) Assess(ctx context.Context, turns []Turn, skills []string) (Verdict, error) {
	if j.APIKey == "" {
		return Verdict{}, fmt.Errorf("judge: missing API key")
	}
	reqBody, err := json.Marshal(map[string]any{
		"model":             orDefault(j.Model, DefaultOpenAIModel),
		"input":             BuildPrompt(turns, skills),
		"max_output_tokens": 256,
		"reasoning":         map[string]string{"effort": "low"},
	})
	if err != nil {
		return Verdict{}, err
	}

	url := orDefault(j.BaseURL, defaultOpenAIBaseURL) + "/v1/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return Verdict{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+j.APIKey)

	client := j.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Verdict{}, fmt.Errorf("judge: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Verdict{}, fmt.Errorf("judge: api status %d: %s", resp.StatusCode, string(body))
	}

	text, err := openAIResponseText(body)
	if err != nil {
		return Verdict{}, err
	}
	if text == "" {
		return Verdict{}, fmt.Errorf("judge: empty response")
	}
	return ParseVerdict(text)
}

func openAIResponseText(body []byte) (string, error) {
	var parsed struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("judge: decode response: %w", err)
	}
	if parsed.OutputText != "" {
		return parsed.OutputText, nil
	}
	for _, item := range parsed.Output {
		for _, content := range item.Content {
			if content.Text != "" && (content.Type == "" || content.Type == "output_text" || content.Type == "text") {
				return content.Text, nil
			}
		}
	}
	return "", nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

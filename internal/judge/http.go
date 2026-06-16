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

// DefaultModel is a cheap model suited to the small judging task. Overridable so
// the model id can change without a code edit.
const DefaultModel = "claude-haiku-4-5-20251001"

const (
	anthropicVersion = "2023-06-01"
	defaultBaseURL   = "https://api.anthropic.com"
)

// Doer is the slice of *http.Client used here, injectable so tests run offline.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// HTTPJudge calls the Anthropic Messages API with the blinded prompt. The bias
// safeguard (no model identity in the prompt) is enforced by BuildPrompt.
type HTTPJudge struct {
	Client  Doer
	APIKey  string
	Model   string
	BaseURL string
}

func (j HTTPJudge) Assess(ctx context.Context, turns []Turn, skills []string) (Verdict, error) {
	if j.APIKey == "" {
		return Verdict{}, fmt.Errorf("judge: missing API key")
	}
	reqBody, err := json.Marshal(map[string]any{
		"model":      orDefault(j.Model, DefaultModel),
		"max_tokens": 256,
		"messages":   []map[string]string{{"role": "user", "content": BuildPrompt(turns, skills)}},
	})
	if err != nil {
		return Verdict{}, err
	}

	url := orDefault(j.BaseURL, defaultBaseURL) + "/v1/messages"
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

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

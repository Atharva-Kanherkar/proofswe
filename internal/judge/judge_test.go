package judge

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestBuildPrompt_BlindAndIncludesTurns(t *testing.T) {
	p := BuildPrompt([]Turn{
		{Role: "user", Text: "fix the redirect"},
		{Role: "assistant", Text: "done, edited auth.go"},
	})
	if !strings.Contains(p, "developer: fix the redirect") {
		t.Errorf("prompt missing developer turn:\n%s", p)
	}
	if !strings.Contains(p, "assistant: done") {
		t.Errorf("prompt missing assistant turn:\n%s", p)
	}
	// Blinding: a concrete model id must never reach the judge.
	if strings.Contains(strings.ToLower(p), "claude") || strings.Contains(strings.ToLower(p), "gpt") {
		t.Errorf("prompt leaks model identity:\n%s", p)
	}
}

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
		outcome Outcome
		corr    int
	}{
		{"plain", `{"outcome":"accepted","corrections":0,"sentiment":0.9}`, false, OutcomeAccepted, 0},
		{"fenced", "```json\n{\"outcome\":\"corrected\",\"corrections\":2,\"sentiment\":-0.2}\n```", false, OutcomeCorrected, 2},
		{"prose-wrapped", `Sure: {"outcome":"abandoned","corrections":1,"sentiment":-0.8} done`, false, OutcomeAbandoned, 1},
		{"bad-outcome", `{"outcome":"great","corrections":0,"sentiment":1}`, true, "", 0},
		{"garbage", `not json`, true, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseVerdict(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", v)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v.Outcome != tc.outcome || v.Corrections != tc.corr {
				t.Errorf("got %+v, want outcome=%s corrections=%d", v, tc.outcome, tc.corr)
			}
		})
	}
}

func TestParseVerdict_ClampsSentiment(t *testing.T) {
	v, err := ParseVerdict(`{"outcome":"accepted","corrections":-3,"sentiment":5}`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Sentiment != 1 {
		t.Errorf("sentiment = %v, want clamped to 1", v.Sentiment)
	}
	if v.Corrections != 0 {
		t.Errorf("corrections = %d, want clamped to 0", v.Corrections)
	}
}

func TestScoreSuccess(t *testing.T) {
	cases := []struct {
		v    Verdict
		want float64
	}{
		{Verdict{OutcomeAccepted, 0, 0.8}, 100},  // 100 - 0 + 8 -> clamp 100
		{Verdict{OutcomeAccepted, 1, 0}, 92},     // 100 - 8
		{Verdict{OutcomeCorrected, 2, -0.2}, 42}, // 60 - 16 - 2
		{Verdict{OutcomeAbandoned, 0, -1}, 5},    // 15 - 10
	}
	for _, tc := range cases {
		if got := ScoreSuccess(tc.v); math.Abs(got-tc.want) > 0.05 {
			t.Errorf("ScoreSuccess(%+v) = %.1f, want %.1f", tc.v, got, tc.want)
		}
	}
}

type fakeDoer struct {
	status int
	body   string
}

func (f fakeDoer) Do(req *http.Request) (*http.Response, error) {
	if req.Header.Get("x-api-key") == "" || req.Header.Get("anthropic-version") == "" {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

func TestHTTPJudge_Assess(t *testing.T) {
	doer := fakeDoer{status: 200, body: `{"content":[{"type":"text","text":"{\"outcome\":\"accepted\",\"corrections\":0,\"sentiment\":0.7}"}]}`}
	j := HTTPJudge{Client: doer, APIKey: "test-key"}
	v, err := j.Assess(context.Background(), []Turn{{Role: "user", Text: "hi"}})
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if v.Outcome != OutcomeAccepted {
		t.Errorf("outcome = %s, want accepted", v.Outcome)
	}
}

func TestHTTPJudge_APIError(t *testing.T) {
	doer := fakeDoer{status: 500, body: `{"error":{"message":"boom"}}`}
	j := HTTPJudge{Client: doer, APIKey: "test-key"}
	if _, err := j.Assess(context.Background(), nil); err == nil {
		t.Error("expected error on non-200 status")
	}
}

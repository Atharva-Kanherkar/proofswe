package judge

import (
	"context"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestBuildPrompt_BlindAndIncludesTurns(t *testing.T) {
	p := BuildPrompt([]Turn{
		{Role: "user", Text: "fix the redirect"},
		{Role: "assistant", Text: "done, edited auth.go"},
	}, nil)
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

func TestBuildPrompt_ExplainsRealSWERubric(t *testing.T) {
	p := BuildPrompt([]Turn{
		{Role: "user", Text: "research enterprise tasks, build the E2B template, make it multi-turn, fix CI, and push"},
		{Role: "assistant", Text: "implemented the template, tests pass, PR is updated"},
		{Role: "user", Text: "merged; now release it"},
	}, nil)

	for _, want := range []string{
		"real software engineering is broader than writing code",
		"product direction",
		"requirement discovery",
		"CI failures",
		"deployment",
		"release",
		"normal task evolution",
		"Do NOT count these as corrections",
		"assistant-caused correction",
		"merged",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing SWE rubric fragment %q:\n%s", want, p)
		}
	}
}

func TestBuildPrompt_AsksForNoiseClassification(t *testing.T) {
	p := BuildPrompt([]Turn{{Role: "user", Text: "what should I build?"}}, nil)
	for _, want := range []string{"task_type", "pure general Q&A", "what should I build?", "Code edits are not required", "does not end in code", "concrete software artifact"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing noise-filter rule %q:\n%s", want, p)
		}
	}
}

func TestBuildPrompt_IncludesProductSteeringRules(t *testing.T) {
	p := BuildPrompt([]Turn{
		{Role: "user", Text: "why do i need an E2B template id? use my E2B keys and execute tasks in sandboxes"},
		{Role: "assistant", Text: "explained the distinction between account credentials and sandbox templates"},
		{Role: "user", Text: "remove hermes for now; this is for enterprises, add an ROI form, show traces"},
		{Role: "user", Text: "ci is failing"},
		{Role: "assistant", Text: "inspected the failing check, fixed it, pushed the branch"},
	}, []string{"proofswe-benchmark"})

	if strings.Contains(strings.ToLower(p), "claude") || strings.Contains(strings.ToLower(p), "gpt") {
		t.Errorf("prompt leaks model identity:\n%s", p)
	}
	for _, want := range []string{
		"new product direction",
		"added requirements",
		"environment/deployment constraints",
		"CI status updates",
		"credential handoff",
		"real CI/deployment constraints appeared late",
		"proofswe-benchmark",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing product-steering rule %q:\n%s", want, p)
		}
	}
}

func TestBuildPrompt_DoesNotTreatSubmitAsAcceptanceOrSoftenPainfulShipping(t *testing.T) {
	p := BuildPrompt([]Turn{
		{Role: "user", Text: "submit this transcript to proofswe"},
		{Role: "assistant", Text: "submitted"},
	}, nil)

	for _, bad := range []string{"push/submit", "submitted, or used", "calibrate them against whether the work ultimately shipped"} {
		if strings.Contains(p, bad) {
			t.Fatalf("prompt still contains deprecated rubric fragment %q:\n%s", bad, p)
		}
	}
	for _, want := range []string{
		"confirmed, or used",
		"Keep this separate from outcome",
		"a shipped or merged session can still have negative sentiment",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing sentiment/outcome separation %q:\n%s", want, p)
		}
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

func TestParseVerdict_ParsesTaskClassification(t *testing.T) {
	v, err := ParseVerdict(`{"task_type":"NOISE","reason":"general ideation","outcome":"accepted","corrections":0,"sentiment":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if v.TaskType != TaskTypeNoise || v.Reason != "general ideation" {
		t.Fatalf("verdict = %+v, want normalized noise classification", v)
	}
	if _, err := ParseVerdict(`{"task_type":"chat","outcome":"accepted","corrections":0,"sentiment":0}`); err == nil {
		t.Fatal("expected unknown task_type to fail")
	}
}

func TestParseVerdict_LegacyResponseDefaultsToSWE(t *testing.T) {
	v, err := ParseVerdict(`{"outcome":"accepted","corrections":0,"sentiment":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if v.TaskType != TaskTypeSWE {
		t.Fatalf("task_type = %q, want %q", v.TaskType, TaskTypeSWE)
	}
}

func TestScoreSuccess(t *testing.T) {
	cases := []struct {
		v    Verdict
		want float64
	}{
		{Verdict{Outcome: OutcomeAccepted, Sentiment: 0.8}, 100},                  // 100 - 0 + 8 -> clamp 100
		{Verdict{Outcome: OutcomeAccepted, Corrections: 1}, 92},                   // 100 - 8
		{Verdict{Outcome: OutcomeCorrected, Corrections: 2, Sentiment: -0.2}, 42}, // 60 - 16 - 2
		{Verdict{Outcome: OutcomeAbandoned, Sentiment: -1}, 5},                    // 15 - 10
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

func TestAnthropicJudge_Assess(t *testing.T) {
	doer := fakeDoer{status: 200, body: `{"content":[{"type":"text","text":"{\"outcome\":\"accepted\",\"corrections\":0,\"sentiment\":0.7}"}]}`}
	j := AnthropicJudge{Client: doer, APIKey: "test-key"}
	v, err := j.Assess(context.Background(), []Turn{{Role: "user", Text: "hi"}}, nil)
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if v.Outcome != OutcomeAccepted {
		t.Errorf("outcome = %s, want accepted", v.Outcome)
	}
}

type openAIFakeDoer struct {
	status int
	body   string
	req    string
}

func (f *openAIFakeDoer) Do(req *http.Request) (*http.Response, error) {
	if req.Header.Get("authorization") != "Bearer test-key" {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}
	body, _ := io.ReadAll(req.Body)
	f.req = string(body)
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

func TestOpenAIJudge_Assess(t *testing.T) {
	doer := &openAIFakeDoer{status: 200, body: `{"output":[{"content":[{"type":"output_text","text":"{\"outcome\":\"corrected\",\"corrections\":2,\"sentiment\":-0.3}"}]}]}`}
	j := OpenAIJudge{Client: doer, APIKey: "test-key"}
	v, err := j.Assess(context.Background(), []Turn{{Role: "user", Text: "hi"}}, nil)
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if v.Outcome != OutcomeCorrected || v.Corrections != 2 {
		t.Errorf("verdict = %+v, want corrected with 2 corrections", v)
	}
	if !strings.Contains(doer.req, `"model":"`+DefaultOpenAIModel+`"`) {
		t.Errorf("request missing default OpenAI model %q: %s", DefaultOpenAIModel, doer.req)
	}
	if !strings.Contains(doer.req, `"effort":"low"`) {
		t.Errorf("request missing low reasoning effort: %s", doer.req)
	}
	// The reasoning model shares max_output_tokens with its reasoning trace, so a
	// tiny budget truncates before any verdict. Pin the generous ceiling.
	if !strings.Contains(doer.req, `"max_output_tokens":`+strconv.Itoa(openAIMaxOutputTokens)) {
		t.Errorf("request missing raised output budget %d: %s", openAIMaxOutputTokens, doer.req)
	}
}

func TestOpenAIJudge_TruncatedReasoning(t *testing.T) {
	// Reasoning exhausted the budget before any message item: status=incomplete
	// with no output text. This must surface as a clear truncation error, not the
	// generic empty-response error (which reads as "the model said nothing").
	doer := &openAIFakeDoer{status: 200, body: `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}`}
	j := OpenAIJudge{Client: doer, APIKey: "test-key"}
	_, err := j.Assess(context.Background(), []Turn{{Role: "user", Text: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected truncation error, got nil")
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error = %q, want it to mention truncation", err)
	}
}

func TestAnthropicJudge_APIError(t *testing.T) {
	doer := fakeDoer{status: 500, body: `{"error":{"message":"boom"}}`}
	j := AnthropicJudge{Client: doer, APIKey: "test-key"}
	if _, err := j.Assess(context.Background(), nil, nil); err == nil {
		t.Error("expected error on non-200 status")
	}
}

func TestBuildPrompt_SkillNote(t *testing.T) {
	p := BuildPrompt([]Turn{{Role: "user", Text: "do the thing"}}, []string{"review-checkpoint"})
	if !strings.Contains(p, "review-checkpoint") || !strings.Contains(strings.ToLower(p), "skill") {
		t.Errorf("prompt missing skill note:\n%s", p)
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
	"github.com/Atharva-Kanherkar/proofswe/internal/judge"
	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

func TestSubmissionHandler_RunsServerJudgeAndScores(t *testing.T) {
	prev := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted, Corrections: 0, Sentiment: 0.8}}, nil
	}
	t.Cleanup(func() { newServerJudge = prev })

	handler, err := newSubmissionHandler(Config{}, judgeOptions{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	task := corpus.FromCapture(reproducibleCaptureForServe(), score.ExtractedSignals{
		Verification:   "passed",
		Termination:    "clean",
		HumanTurns:     1,
		Scope:          score.ScopeSignals{FilesTouched: 1},
		LandingQuality: "commit",
	}, true, nil, "sha256:abc123", "", time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC))
	body, _ := json.Marshal(submitRequest{SchemaVersion: submitSchemaVersion, Task: task})

	req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got submitResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v\n%s", err, rr.Body.String())
	}
	if got.Status != "judged" || got.Judge.Status != "server_judged" {
		t.Fatalf("response not server judged: %+v", got)
	}
	if got.Scorecard == nil || got.Scorecard.Composite <= 0 {
		t.Fatalf("missing scorecard: %+v", got.Scorecard)
	}
	if !strings.Contains(got.TaskID, "sha256:") {
		t.Fatalf("task id not carried: %+v", got)
	}
}

func TestSubmissionHandler_Health(t *testing.T) {
	prev := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted}}, nil
	}
	t.Cleanup(func() { newServerJudge = prev })

	handler, err := newSubmissionHandler(Config{}, judgeOptions{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.TrimSpace(rr.Body.String()) != `{"ok":true}` {
		t.Fatalf("body = %q", rr.Body.String())
	}
}

func TestSubmissionHandler_RequiresTokenWhenConfigured(t *testing.T) {
	prev := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted}}, nil
	}
	t.Cleanup(func() { newServerJudge = prev })

	handler, err := newSubmissionHandler(Config{Getenv: func(k string) string {
		if k == "PROOFSWE_API_TOKEN" {
			return "server-token"
		}
		return ""
	}}, judgeOptions{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	task := corpus.FromCapture(reproducibleCaptureForServe(), score.ExtractedSignals{}, true, nil, "sha256:abc123", "", time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC))
	body, _ := json.Marshal(submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rr.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(body))
	req.Header.Set("authorization", "Bearer server-token")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func reproducibleCaptureForServe() core.Task {
	return core.Task{
		Harness: "codex",
		Model:   core.TaskModel{ID: "gpt-test"},
		Repo: core.TaskRepo{
			RemoteURL:   "https://github.com/owner/repo.git",
			BaseCommit:  "abc123",
			LicenseSPDX: "MIT",
			IsPublic:    true,
		},
		Prompts: []core.TaskPrompt{{TurnIndex: 0, Role: "user", Text: "fix the bug"}},
		Trajectory: core.TaskTrajectory{
			AssistantMessages: []core.TaskText{{TurnIndex: 0, Text: "fixed"}},
			ToolCalls:         []core.TaskText{{TurnIndex: 0, Name: "apply_patch", Text: "patch"}},
		},
		Code: core.TaskCode{
			Patch: "+++ b/main.go\n+fix()\n",
			Files: []core.TaskFile{{Path: "main.go", Role: core.FileRoleSolution}},
		},
		RedactionReport: core.RedactionReport{ScrubberVersion: "redact/1", BestEffortNotice: "best effort"},
	}
}

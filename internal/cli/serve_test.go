package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
	"github.com/Atharva-Kanherkar/proofswe/internal/judge"
	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

func serveTestConfig() Config {
	return Config{Getenv: func(string) string { return "" }}
}

func TestSubmissionHandler_QueuesAndPollsServerJudge(t *testing.T) {
	var judgeCalls atomic.Int32
	prev := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return countingJudge{
			calls: &judgeCalls,
			verdict: judge.Verdict{
				Outcome:     judge.OutcomeAccepted,
				Corrections: 0,
				Sentiment:   0.8,
			},
		}, nil
	}
	t.Cleanup(func() { newServerJudge = prev })

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	handler, cleanup, err := newSubmissionHandlerWithContext(ctx, serveTestConfig(), judgeOptions{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	defer cleanup()
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
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got submitResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v\n%s", err, rr.Body.String())
	}
	if got.Status != submissionStatusQueued || got.Scorecard != nil {
		t.Fatalf("response not queued: %+v", got)
	}
	if judgeCalls.Load() != 0 {
		t.Fatalf("POST should not call judge inline, calls=%d", judgeCalls.Load())
	}
	if !strings.Contains(got.TaskID, "sha256:") {
		t.Fatalf("task id not carried: %+v", got)
	}

	var polled submitResponse
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(http.MethodGet, "/v1/submissions/"+got.SubmissionID, nil)
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("poll status = %d body=%s", rr.Code, rr.Body.String())
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &polled); err != nil {
			t.Fatalf("decode poll: %v", err)
		}
		if polled.Status == submissionStatusJudged && polled.Scorecard != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if polled.Status != submissionStatusJudged || polled.Scorecard == nil || polled.Scorecard.Composite <= 0 {
		t.Fatalf("submission did not become judged: %+v", polled)
	}
	if judgeCalls.Load() != 1 {
		t.Fatalf("judge calls = %d, want 1", judgeCalls.Load())
	}
}

func TestSubmissionHandler_ParallelSubmissionsQueueWithoutInlineJudging(t *testing.T) {
	var judgeCalls atomic.Int32
	prev := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return countingJudge{
			calls:   &judgeCalls,
			delay:   250 * time.Millisecond,
			verdict: judge.Verdict{Outcome: judge.OutcomeAccepted},
		}, nil
	}
	t.Cleanup(func() { newServerJudge = prev })

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	handler, cleanup, err := newSubmissionHandlerWithContext(ctx, serveTestConfig(), judgeOptions{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	defer cleanup()

	task := corpus.FromCapture(reproducibleCaptureForServe(), score.ExtractedSignals{}, true, nil, "sha256:abc123", "", time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC))
	body, _ := json.Marshal(submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	var wg sync.WaitGroup
	errCh := make(chan string, 100)
	start := time.Now()
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(body))
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusAccepted {
				errCh <- rr.Body.String()
			}
		}()
	}
	wg.Wait()
	close(errCh)
	if len(errCh) > 0 {
		t.Fatalf("parallel submit failed: %s", <-errCh)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("parallel submits took %s; requests appear coupled to judging", elapsed)
	}
	if calls := judgeCalls.Load(); calls > 1 {
		t.Fatalf("judge calls immediately after POSTs = %d, want at most one worker claim", calls)
	}
}

func TestSubmissionHandler_Health(t *testing.T) {
	prev := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted}}, nil
	}
	t.Cleanup(func() { newServerJudge = prev })

	handler, cleanup, err := newSubmissionHandlerWithContext(t.Context(), serveTestConfig(), judgeOptions{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	defer cleanup()
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

func TestSubmissionHandler_TokenIsOptIn(t *testing.T) {
	prev := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted}}, nil
	}
	t.Cleanup(func() { newServerJudge = prev })

	cfg := serveTestConfig()
	cfg.Getenv = func(k string) string {
		if k == "PROOFSWE_API_TOKEN" {
			return "server-token"
		}
		return ""
	}
	handler, cleanup, err := newSubmissionHandlerWithContext(t.Context(), cfg, judgeOptions{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	defer cleanup()

	task := corpus.FromCapture(reproducibleCaptureForServe(), score.ExtractedSignals{}, true, nil, "sha256:abc123", "", time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC))
	body, _ := json.Marshal(submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("public submit status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSubmissionHandler_RequiresTokenWhenFlagEnabled(t *testing.T) {
	prev := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted}}, nil
	}
	t.Cleanup(func() { newServerJudge = prev })

	cfg := serveTestConfig()
	cfg.Getenv = func(k string) string {
		switch k {
		case "PROOFSWE_API_TOKEN":
			return "server-token"
		case "PROOFSWE_REQUIRE_SUBMIT_TOKEN":
			return "true"
		default:
			return ""
		}
	}
	handler, cleanup, err := newSubmissionHandlerWithContext(t.Context(), cfg, judgeOptions{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	defer cleanup()

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
	if rr.Code != http.StatusAccepted {
		t.Fatalf("authenticated status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSubmissionHandler_RejectsInvalidTask(t *testing.T) {
	prev := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted}}, nil
	}
	t.Cleanup(func() { newServerJudge = prev })

	handler, cleanup, err := newSubmissionHandlerWithContext(t.Context(), serveTestConfig(), judgeOptions{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	defer cleanup()

	body, _ := json.Marshal(submitRequest{SchemaVersion: submitSchemaVersion, Task: corpus.Task{CorpusSchemaVersion: corpus.SchemaVersion}})
	req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid task status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMemorySubmissionStore_DedupesTasksAndRetriesJobs(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpus.FromCapture(reproducibleCaptureForServe(), score.ExtractedSignals{}, true, nil, "sha256:abc123", "@dev", time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC))
	req := submitRequest{SchemaVersion: submitSchemaVersion, ClientVersion: "test", Task: task}
	first, err := store.CreateSubmission(t.Context(), req)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := store.CreateSubmission(t.Context(), req)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.SubmissionID == second.SubmissionID {
		t.Fatal("submissions should be unique")
	}
	if got := store.taskCount(); got != 1 {
		t.Fatalf("task count = %d, want 1", got)
	}
	job, ok, err := store.ClaimJudgeJob(t.Context(), "worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim job ok=%v err=%v", ok, err)
	}
	if job.Attempts != 1 {
		t.Fatalf("attempts after claim = %d, want 1", job.Attempts)
	}
	if err := store.FailJudgeJob(t.Context(), job, "temporary", time.Now()); err != nil {
		t.Fatalf("fail job: %v", err)
	}
	job, ok, err = store.ClaimJudgeJob(t.Context(), "worker", time.Now().Add(time.Second))
	if err != nil || !ok {
		t.Fatalf("reclaim job ok=%v err=%v", ok, err)
	}
	run := judgeRunRecord{
		SubmissionID: job.SubmissionID,
		Verdict:      judge.Verdict{Outcome: judge.OutcomeAccepted},
		Scorecard:    &submitScorecard{Composite: 91},
		Status:       submissionStatusJudged,
		StartedAt:    time.Now(),
		CompletedAt:  time.Now(),
	}
	if err := store.CompleteJudgeJob(t.Context(), job, run); err != nil {
		t.Fatalf("complete job: %v", err)
	}
	rec, ok, err := store.GetSubmission(t.Context(), job.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get completed submission ok=%v err=%v", ok, err)
	}
	if rec.Status != submissionStatusJudged || rec.Scorecard == nil || rec.Scorecard.Composite != 91 {
		t.Fatalf("completed record = %+v", rec)
	}
	if got := store.judgeRunCount(); got != 1 {
		t.Fatalf("judge runs = %d, want 1", got)
	}
}

func TestMemorySubmissionStore_MarksPermanentJudgeFailure(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpus.FromCapture(reproducibleCaptureForServe(), score.ExtractedSignals{}, true, nil, "sha256:abc123", "", time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC))
	rec, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var job judgeJobRecord
	for i := 0; i < maxJudgeAttempts; i++ {
		var ok bool
		job, ok, err = store.ClaimJudgeJob(t.Context(), "worker", time.Now().Add(time.Second))
		if err != nil || !ok {
			t.Fatalf("claim %d ok=%v err=%v", i, ok, err)
		}
		if err := store.FailJudgeJob(t.Context(), job, "still failing", time.Now()); err != nil {
			t.Fatalf("fail %d: %v", i, err)
		}
	}
	got, ok, err := store.GetSubmission(t.Context(), rec.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get failed submission ok=%v err=%v", ok, err)
	}
	if got.Status != submissionStatusFailed || got.ErrorCode != "judge_failed" {
		t.Fatalf("failed submission = %+v", got)
	}
}

type countingJudge struct {
	calls   *atomic.Int32
	delay   time.Duration
	verdict judge.Verdict
	err     error
}

func (j countingJudge) Assess(context.Context, []judge.Turn, []string) (judge.Verdict, error) {
	j.calls.Add(1)
	if j.delay > 0 {
		time.Sleep(j.delay)
	}
	if j.err != nil {
		return judge.Verdict{}, j.err
	}
	return j.verdict, nil
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

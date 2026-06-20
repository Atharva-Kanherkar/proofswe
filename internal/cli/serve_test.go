package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func corpusTaskForServe(ex score.ExtractedSignals, landed bool) corpus.Task {
	task := corpus.FromCapture(reproducibleCaptureForServe(), ex, landed, nil, "", "", time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC))
	task.TaskID = corpusTaskID(task)
	return task
}

func mustCorpusTaskPath(t *testing.T, taskID string) string {
	t.Helper()
	path, err := corpusTaskPath(taskID)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	return path
}

func TestSubmissionHandler_QueuesAndPollsServerJudge(t *testing.T) {
	var judgeCalls atomic.Int32
	releaseJudge := make(chan struct{})
	prev := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return blockingJudge{
			calls:   &judgeCalls,
			release: releaseJudge,
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
	task := corpusTaskForServe(score.ExtractedSignals{
		Verification:   "passed",
		Termination:    "clean",
		HumanTurns:     1,
		Scope:          score.ScopeSignals{FilesTouched: 1},
		LandingQuality: "commit",
	}, true)
	body, _ := json.Marshal(submitRequest{SchemaVersion: submitSchemaVersion, Task: task})

	req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("POST did not return before judging was released")
	}
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
	if !strings.Contains(got.TaskID, "sha256:") {
		t.Fatalf("task id not carried: %+v", got)
	}
	close(releaseJudge)

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
	if polled.Scorecard.ScoreVersion != score.ScoreVersion {
		t.Fatalf("score_version = %q, want %q", polled.Scorecard.ScoreVersion, score.ScoreVersion)
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

	task := corpusTaskForServe(score.ExtractedSignals{}, true)
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

	task := corpusTaskForServe(score.ExtractedSignals{}, true)
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

	task := corpusTaskForServe(score.ExtractedSignals{}, true)
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
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	task.Contributor = "@dev"
	req := submitRequest{SchemaVersion: submitSchemaVersion, ClientVersion: "test", Task: task}
	first, err := store.CreateSubmission(t.Context(), req)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, ClientVersion: "retry", Task: task})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.SubmissionID == second.SubmissionID {
		t.Fatal("different payloads should create different submissions")
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
	if err := store.FailJudgeJob(t.Context(), job, "temporary", time.Now(), false); err != nil {
		t.Fatalf("fail job: %v", err)
	}
	job, ok, err = store.ClaimJudgeJob(t.Context(), "worker", time.Now().Add(judgeRetryBackoff+time.Second))
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
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	rec, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var job judgeJobRecord
	for i := 0; i < maxJudgeAttempts; i++ {
		var ok bool
		claimAt := time.Now().Add(time.Duration(i+1)*maxJudgeAttempts*judgeRetryBackoff + time.Second)
		job, ok, err = store.ClaimJudgeJob(t.Context(), "worker", claimAt)
		if err != nil || !ok {
			t.Fatalf("claim %d ok=%v err=%v", i, ok, err)
		}
		if err := store.FailJudgeJob(t.Context(), job, "still failing", time.Now(), false); err != nil {
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

func TestMemorySubmissionStore_IdempotentPayloadReusesSubmission(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	req := submitRequest{SchemaVersion: submitSchemaVersion, ClientVersion: "test", Task: task}
	first, err := store.CreateSubmission(t.Context(), req)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := store.CreateSubmission(t.Context(), req)
	if err != nil {
		t.Fatalf("create duplicate: %v", err)
	}
	if first.SubmissionID != second.SubmissionID {
		t.Fatalf("duplicate payload got new submission %q, want %q", second.SubmissionID, first.SubmissionID)
	}
	if len(store.jobs) != 1 {
		t.Fatalf("duplicate payload enqueued %d jobs, want 1", len(store.jobs))
	}
}

func TestMemorySubmissionStore_ReclaimsStaleJudgingJob(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	rec, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	first, ok, err := store.ClaimJudgeJob(t.Context(), "worker-1", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim first ok=%v err=%v", ok, err)
	}
	if first.Attempts != 1 {
		t.Fatalf("first attempts = %d, want 1", first.Attempts)
	}
	second, ok, err := store.ClaimJudgeJob(t.Context(), "worker-2", time.Now().Add(judgeVisibilityTimeout+time.Second))
	if err != nil || !ok {
		t.Fatalf("claim stale ok=%v err=%v", ok, err)
	}
	if second.ID != first.ID || second.Attempts != 2 {
		t.Fatalf("stale reclaim = %+v, want same job second attempt", second)
	}
	got, ok, err := store.GetSubmission(t.Context(), rec.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get submission ok=%v err=%v", ok, err)
	}
	if got.Status != submissionStatusJudging {
		t.Fatalf("status after stale reclaim = %q, want judging", got.Status)
	}
}

func TestMemorySubmissionStore_ClaimsJudgedJobForPublishRecovery(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	rec, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	job, ok, err := store.ClaimJudgeJob(t.Context(), "judge-worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim judge ok=%v err=%v", ok, err)
	}
	run := judgeRunRecord{
		SubmissionID: job.SubmissionID,
		Verdict:      judge.Verdict{Outcome: judge.OutcomeAccepted},
		Scorecard:    &submitScorecard{Composite: 87},
		Status:       submissionStatusJudged,
		StartedAt:    time.Now(),
		CompletedAt:  time.Now(),
	}
	if err := store.CompleteJudgeJob(t.Context(), job, run); err != nil {
		t.Fatalf("complete judge: %v", err)
	}
	if _, ok, err := store.ClaimJudgeJob(t.Context(), "judge-worker", time.Now().Add(judgeVisibilityTimeout+time.Second)); err != nil || ok {
		t.Fatalf("judged job should not be reclaimed as judge work ok=%v err=%v", ok, err)
	}
	publishJob, ok, err := store.ClaimPublishJob(t.Context(), "publish-worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim publish ok=%v err=%v", ok, err)
	}
	if publishJob.ID != job.ID || publishJob.Scorecard == nil || publishJob.Scorecard.Composite != 87 {
		t.Fatalf("publish job = %+v", publishJob)
	}
	got, ok, err := store.GetSubmission(t.Context(), rec.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get submission ok=%v err=%v", ok, err)
	}
	if got.Status != submissionStatusPublish {
		t.Fatalf("status = %q, want publishing", got.Status)
	}
}

func TestMemorySubmissionStore_PermanentJudgeFailureDoesNotRetry(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	rec, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	job, ok, err := store.ClaimJudgeJob(t.Context(), "worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if err := store.FailJudgeJob(t.Context(), job, "judge: missing API key", time.Now(), true); err != nil {
		t.Fatalf("fail permanent: %v", err)
	}
	if _, ok, err := store.ClaimJudgeJob(t.Context(), "worker", time.Now().Add(24*time.Hour)); err != nil || ok {
		t.Fatalf("permanent failure should not reclaim ok=%v err=%v", ok, err)
	}
	got, ok, err := store.GetSubmission(t.Context(), rec.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get submission ok=%v err=%v", ok, err)
	}
	if got.Status != submissionStatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

func TestMemorySubmissionStore_ListsPublishedCorpusFeedAndLeaderboard(t *testing.T) {
	store := newMemorySubmissionStore()
	firstTask := corpusTaskForServe(score.ExtractedSignals{}, true)
	firstTask.Model = "gpt-5"
	firstTask.Prompts[0].Text = "fix codex bug"
	firstTask.TaskID = corpusTaskID(firstTask)
	secondTask := corpusTaskForServe(score.ExtractedSignals{}, true)
	secondTask.Harness = "claudecode"
	secondTask.Model = "claude-opus"
	secondTask.Prompts[0].Text = "fix claude bug"
	secondTask.TaskID = corpusTaskID(secondTask)

	publishForLeaderboard(t, store, firstTask, "", 80, "solid fix", corpusMapping{
		Path:      mustCorpusTaskPath(t, firstTask.TaskID),
		PRURL:     "https://github.com/Atharva-Kanherkar/proofswe-corpus/pull/10",
		CommitSHA: "abc",
	})
	publishForLeaderboard(t, store, firstTask, "duplicate-upload", 70, "later duplicate", corpusMapping{
		Path:      mustCorpusTaskPath(t, firstTask.TaskID),
		PRURL:     "https://github.com/Atharva-Kanherkar/proofswe-corpus/pull/10",
		CommitSHA: "abc",
	})
	publishForLeaderboard(t, store, secondTask, "", 90, "excellent fix", corpusMapping{
		Path:      mustCorpusTaskPath(t, secondTask.TaskID),
		PRURL:     "https://github.com/Atharva-Kanherkar/proofswe-corpus/pull/11",
		CommitSHA: "def",
	})

	records, err := store.ListPublishedCorpus(t.Context(), publishedCorpusQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list published: %v", err)
	}
	models, err := store.ListPublishedModelStats(t.Context(), publishedCorpusQuery{})
	if err != nil {
		t.Fatalf("list model stats: %v", err)
	}
	resp := buildLeaderboardResponse(records, models, time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC))
	if len(resp.Recent) != 2 || len(resp.Models) != 2 {
		t.Fatalf("leaderboard response = %+v", resp)
	}
	if resp.Recent[0].GitHubPRURL == "" || resp.Recent[0].GitHubURL == "" || resp.Recent[0].Summary == "" {
		t.Fatalf("recent item missing public display fields: %+v", resp.Recent[0])
	}
	if resp.Models[0].Model != "claude-opus" || resp.Models[0].AverageScore != 90 {
		t.Fatalf("model rows = %+v", resp.Models)
	}
	for _, model := range resp.Models {
		if model.Model == "gpt-5" && (model.SubmissionCount != 1 || model.AverageScore != 70) {
			t.Fatalf("duplicate task inflated model stats: %+v", model)
		}
	}
	limited, err := store.ListPublishedCorpus(t.Context(), publishedCorpusQuery{Limit: 1})
	if err != nil {
		t.Fatalf("limited list: %v", err)
	}
	limitedResp := buildLeaderboardResponse(limited, models, time.Now())
	if len(limitedResp.Recent) != 1 || len(limitedResp.Models) != 2 {
		t.Fatalf("limit should affect recent feed only: %+v", limitedResp)
	}

	filtered, err := store.ListPublishedCorpus(t.Context(), publishedCorpusQuery{Harness: "codex", Limit: 10})
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Harness != "codex" {
		t.Fatalf("filtered records = %+v", filtered)
	}
}

// TestMemorySubmissionStore_LatestPublishedTieBreaksBySeq reproduces the coarse
// clock collision seen on Windows: two submissions for the same task publish
// within the same OS clock tick, so their UpdatedAt timestamps are identical.
// The most recently created submission must win deterministically, even when
// its random SubmissionID sorts before the earlier one's.
func TestMemorySubmissionStore_LatestPublishedTieBreaksBySeq(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	task.Model = "gpt-5"
	task.TaskID = corpusTaskID(task)
	store.tasks[task.TaskID] = task

	tie := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
	scored := func(composite float64) *submitScorecard {
		return &submitScorecard{Composite: composite, ScoreVersion: "score/test"}
	}
	// Earlier submission: higher (lexically later) ID but lower seq.
	store.submissions["sub_zzz"] = submissionRecord{
		SubmissionID: "sub_zzz", TaskID: task.TaskID, Status: submissionStatusPubDone,
		Scorecard: scored(80), GitHubPath: "tasks/x.json", UpdatedAt: tie, seq: 1,
	}
	// Later submission: lower (lexically earlier) ID but higher seq — must win.
	store.submissions["sub_aaa"] = submissionRecord{
		SubmissionID: "sub_aaa", TaskID: task.TaskID, Status: submissionStatusPubDone,
		Scorecard: scored(70), GitHubPath: "tasks/x.json", UpdatedAt: tie, seq: 2,
	}

	records, err := store.ListPublishedCorpus(t.Context(), publishedCorpusQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list published: %v", err)
	}
	if len(records) != 1 || records[0].Submission.SubmissionID != "sub_aaa" {
		t.Fatalf("expected latest submission sub_aaa to win tie, got %+v", records)
	}
	models, err := store.ListPublishedModelStats(t.Context(), publishedCorpusQuery{})
	if err != nil {
		t.Fatalf("list model stats: %v", err)
	}
	if len(models) != 1 || models[0].SubmissionCount != 1 || models[0].LatestScore != 70 {
		t.Fatalf("tie should resolve to latest score 70, got %+v", models)
	}
}

func TestSubmissionHandler_LeaderboardEndpoint(t *testing.T) {
	prevJudge := newServerJudge
	newServerJudge = func(Config, judgeOptions) (judge.Judge, error) {
		return judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted, Sentiment: 0.8}}, nil
	}
	prevPublisher := newServerCorpusPublisher
	publisher := &fakeCorpusPublisher{}
	newServerCorpusPublisher = func(Config) corpusPublisher { return publisher }
	t.Cleanup(func() {
		newServerJudge = prevJudge
		newServerCorpusPublisher = prevPublisher
	})

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	handler, cleanup, err := newSubmissionHandlerWithContext(ctx, serveTestConfig(), judgeOptions{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	defer cleanup()

	task := corpusTaskForServe(score.ExtractedSignals{Verification: "passed"}, true)
	task.Model = "gpt-5"
	task.Prompts[0].Text = "publish a displayable task"
	task.TaskID = corpusTaskID(task)
	publisher.mapping = corpusMapping{
		Path:      mustCorpusTaskPath(t, task.TaskID),
		PRURL:     "https://github.com/Atharva-Kanherkar/proofswe-corpus/pull/42",
		CommitSHA: "commit42",
	}
	body, _ := json.Marshal(submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d body=%s", rr.Code, rr.Body.String())
	}

	var got leaderboardResponse
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(http.MethodGet, "/v1/leaderboard?harness=codex&limit=5", nil)
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("leaderboard status = %d body=%s", rr.Code, rr.Body.String())
		}
		if rr.Header().Get("access-control-allow-origin") != "*" {
			t.Fatalf("missing public CORS header: %v", rr.Header())
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode leaderboard: %v\n%s", err, rr.Body.String())
		}
		if len(got.Recent) == 1 && len(got.Models) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(got.Recent) != 1 || got.Recent[0].GitHubPRURL == "" || got.Recent[0].GitHubURL == "" || got.Recent[0].Summary == "" {
		t.Fatalf("leaderboard response = %+v", got)
	}
	if got.Recent[0].TaskID != task.TaskID || got.Models[0].Model != "gpt-5" {
		t.Fatalf("wrong leaderboard item = %+v", got)
	}
	// The expandable detail surfaces the developer's opening ask and the
	// deterministic outcome/score breakdown — content that is already public in
	// the corpus repo.
	if got.Recent[0].TaskStatement != "publish a displayable task" {
		t.Fatalf("task statement missing from detail: %+v", got.Recent[0])
	}
	if got.Recent[0].Outcome == nil || got.Recent[0].Outcome.Verification != "passed" {
		t.Fatalf("outcome missing from detail: %+v", got.Recent[0])
	}
	if len(got.Recent[0].Axes) == 0 {
		t.Fatalf("scored axes missing from detail: %+v", got.Recent[0])
	}
	// The raw agent trajectory and the code patch must still never enter the
	// feed — only the task statement, outcome, and score breakdown do.
	encoded := rr.Body.String()
	for _, forbidden := range []string{"fixed", "+++ b/main.go", "apply_patch"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("leaderboard leaked raw corpus content %q in %s", forbidden, encoded)
		}
	}
}

func TestSubmissionHandler_LeaderboardRejectsInvalidLimit(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodGet, "/v1/leaderboard?limit=999999", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMemorySubmissionStore_RejectsTaskIDProvenanceConflict(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	if _, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task}); err != nil {
		t.Fatalf("create: %v", err)
	}
	tampered := task
	tampered.Model = "different-agent"
	if _, _, err := store.GetTaskMapping(t.Context(), tampered); !errors.Is(err, errTaskIDConflict) {
		t.Fatalf("GetTaskMapping error = %v, want errTaskIDConflict", err)
	}
	tampered = task
	tampered.Prompts[0].Text = "different opening prompt"
	if err := validateSubmittedTask(tampered); err == nil || !strings.Contains(err.Error(), "task_id mismatch") {
		t.Fatalf("validate mutated task error = %v, want task_id mismatch", err)
	}
}

func TestValidateSubmittedTaskRejectsPatchlessWithoutHistoricalBase(t *testing.T) {
	task := corpusTaskForServe(score.ExtractedSignals{Scope: score.ScopeSignals{FilesTouched: 1}}, true)
	task.Code = corpus.Code{}
	task.TaskID = corpusTaskID(task)

	if err := validateSubmittedTask(task); err == nil || !strings.Contains(err.Error(), "base commit was not inferred") {
		t.Fatalf("validate patchless without historical base = %v", err)
	}
	task.Repo.BaseCommitSource = corpus.BaseCommitSourceTranscriptStart
	if err := validateSubmittedTask(task); err != nil {
		t.Fatalf("validate patchless historical task: %v", err)
	}

	task.Outcome.FilesTouched = 0
	if err := validateSubmittedTask(task); err == nil || !strings.Contains(err.Error(), "no transcript edit evidence") {
		t.Fatalf("validate patchless without edit evidence = %v", err)
	}
}

func TestValidateSubmittedTaskRequiresAgreementForRawCodePublication(t *testing.T) {
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	task.Repo.LicenseSPDX = ""
	task.TaskID = corpusTaskID(task)

	if err := validateSubmittedTask(task); err == nil || !strings.Contains(err.Error(), "code publication agreement required") {
		t.Fatalf("validate raw code without agreement = %v", err)
	}
	task.CodePublicationAgreementVersion = corpus.CodePublicationAgreementVersion
	if err := validateSubmittedTask(task); err != nil {
		t.Fatalf("validate raw code with agreement: %v", err)
	}
}

func TestSubmitRateLimiter(t *testing.T) {
	limiter := newSubmitRateLimiter(2, time.Minute)
	req := httptest.NewRequest(http.MethodPost, "/v1/submissions", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	if !limiter.allow(req) {
		t.Fatal("first request should pass")
	}
	if !limiter.allow(req) {
		t.Fatal("second request should pass")
	}
	if limiter.allow(req) {
		t.Fatal("third request should be rate limited")
	}
	other := httptest.NewRequest(http.MethodPost, "/v1/submissions", nil)
	other.RemoteAddr = "203.0.113.11:1234"
	if !limiter.allow(other) {
		t.Fatal("different client should get its own bucket")
	}
}

func TestSubmissionWorker_PersistsVerdictAfterContextCanceled(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	rec, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	job, ok, err := store.ClaimJudgeJob(t.Context(), "worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w := submissionWorker{
		store: store,
		judge: judge.FakeJudge{V: judge.Verdict{
			Outcome:   judge.OutcomeAccepted,
			Sentiment: 0.5,
		}},
	}
	w.process(ctx, job)
	got, ok, err := store.GetSubmission(t.Context(), rec.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get submission ok=%v err=%v", ok, err)
	}
	if got.Status != submissionStatusJudged || got.Scorecard == nil {
		t.Fatalf("worker did not persist completed verdict after cancellation: %+v", got)
	}
}

func TestPostgresSubmissionSchemaProtectsQueuedJobSnapshot(t *testing.T) {
	if !strings.Contains(postgresMigrations, "task_json JSONB NOT NULL") {
		t.Fatal("judge_jobs must snapshot task_json so queued jobs do not read mutable tasks rows")
	}
	if !strings.Contains(postgresMigrations, "payload_sha256 TEXT NOT NULL UNIQUE") {
		t.Fatal("submissions must make payload_sha256 unique for idempotency")
	}
}

func TestCorpusTaskPath(t *testing.T) {
	path, err := corpusTaskPath("sha256:abcdef123456")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if path != "tasks/sha256/ab/abcdef123456.json" {
		t.Fatalf("path = %q", path)
	}
}

func TestSubmissionWorker_PublishesJudgedTask(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	rec, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	job, ok, err := store.ClaimJudgeJob(t.Context(), "worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	publisher := &fakeCorpusPublisher{mapping: corpusMapping{Path: mustCorpusTaskPath(t, task.TaskID), PRURL: "https://github.com/Atharva-Kanherkar/proofswe-corpus/pull/1", CommitSHA: "abc"}}
	w := submissionWorker{
		store:     store,
		judge:     judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted}},
		publisher: publisher,
	}
	w.process(t.Context(), job)
	got, ok, err := store.GetSubmission(t.Context(), rec.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if got.Status != submissionStatusPubDone || got.GitHubPath == "" || got.GitHubPRURL == "" || got.Scorecard == nil {
		t.Fatalf("published submission = %+v", got)
	}
	if publisher.calls.Load() != 1 {
		t.Fatalf("publisher calls = %d, want 1", publisher.calls.Load())
	}
}

func TestSubmissionWorker_FiltersNoise(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	rec, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	job, ok, err := store.ClaimJudgeJob(t.Context(), "worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	publisher := &fakeCorpusPublisher{mapping: corpusMapping{Path: "must-not-publish.json"}}
	w := submissionWorker{
		store: store,
		judge: judge.FakeJudge{V: judge.Verdict{
			Outcome:  judge.OutcomeAccepted,
			TaskType: judge.TaskTypeNoise,
			Reason:   "pure open-ended product ideation with no concrete artifact",
		}},
		publisher: publisher,
	}
	w.process(t.Context(), job)

	got, ok, err := store.GetSubmission(t.Context(), rec.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if got.Status != submissionStatusFiltered || got.Scorecard != nil {
		t.Fatalf("filtered submission = %+v, want terminal filtered status without scorecard", got)
	}
	if got.ErrorCode != "noise" || got.ErrorMessage == "" {
		t.Fatalf("filter metadata = %q %q, want noise reason", got.ErrorCode, got.ErrorMessage)
	}
	if publisher.calls.Load() != 0 {
		t.Fatalf("publisher calls = %d, want 0", publisher.calls.Load())
	}
	if !isTerminalSubmissionStatus(got.Status) {
		t.Fatalf("filtered status must stop client polling")
	}
	if records, err := store.ListPublishedCorpus(t.Context(), publishedCorpusQuery{}); err != nil || len(records) != 0 {
		t.Fatalf("leaderboard records = %v, err=%v; filtered task must be absent", records, err)
	}
}

func TestSubmissionWorker_DuplicateTaskReusesExistingMapping(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	first, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	publisher := &fakeCorpusPublisher{mapping: corpusMapping{Path: mustCorpusTaskPath(t, task.TaskID), PRURL: "https://github.com/Atharva-Kanherkar/proofswe-corpus/pull/1"}}
	w := submissionWorker{store: store, judge: judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted}}, publisher: publisher}
	job, _, _ := store.ClaimJudgeJob(t.Context(), "worker", time.Now())
	w.process(t.Context(), job)

	second, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, ClientVersion: "retry", Task: task})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	job, ok, err := store.ClaimJudgeJob(t.Context(), "worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim second ok=%v err=%v", ok, err)
	}
	w.process(t.Context(), job)
	got, ok, err := store.GetSubmission(t.Context(), second.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get second ok=%v err=%v", ok, err)
	}
	if got.GitHubPRURL == "" || got.Status != submissionStatusPubDone {
		t.Fatalf("second submission mapping = %+v", got)
	}
	if first.SubmissionID == second.SubmissionID {
		t.Fatal("second distinct payload should have its own submission")
	}
	if publisher.calls.Load() != 1 {
		t.Fatalf("duplicate task should reuse mapping; publisher calls = %d", publisher.calls.Load())
	}
}

func TestSubmissionWorker_PublishFailureKeepsScorecardAndRetriesWithoutRejudge(t *testing.T) {
	store := newMemorySubmissionStore()
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	rec, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, Task: task})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	job, ok, err := store.ClaimJudgeJob(t.Context(), "worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	var judgeCalls atomic.Int32
	publisher := &fakeCorpusPublisher{
		failures: 1,
		mapping:  corpusMapping{Path: mustCorpusTaskPath(t, task.TaskID), PRURL: "https://github.com/Atharva-Kanherkar/proofswe-corpus/pull/1"},
	}
	w := submissionWorker{
		store:     store,
		judge:     countingJudge{calls: &judgeCalls, verdict: judge.Verdict{Outcome: judge.OutcomeAccepted}},
		publisher: publisher,
	}
	w.process(t.Context(), job)
	got, ok, err := store.GetSubmission(t.Context(), rec.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get after failure ok=%v err=%v", ok, err)
	}
	if got.Status != submissionStatusJudged || got.Scorecard == nil {
		t.Fatalf("publish failure should preserve judged scorecard: %+v", got)
	}
	job, ok, err = store.ClaimPublishJob(t.Context(), "worker", time.Now().Add(judgeRetryBackoff+time.Second))
	if err != nil || !ok {
		t.Fatalf("claim retry ok=%v err=%v", ok, err)
	}
	w.process(t.Context(), job)
	got, ok, err = store.GetSubmission(t.Context(), rec.SubmissionID)
	if err != nil || !ok {
		t.Fatalf("get after retry ok=%v err=%v", ok, err)
	}
	if got.Status != submissionStatusPubDone || got.GitHubPath == "" {
		t.Fatalf("retry did not publish: %+v", got)
	}
	if judgeCalls.Load() != 1 {
		t.Fatalf("retry should not rejudge; judge calls = %d", judgeCalls.Load())
	}
}

func TestGitHubConflictClassification(t *testing.T) {
	if !isGitHubConflict(githubAPIError{StatusCode: http.StatusUnprocessableEntity}) {
		t.Fatal("422 should be treated as branch/PR conflict")
	}
	if !isGitHubNotFound(githubAPIError{StatusCode: http.StatusNotFound}) {
		t.Fatal("404 should be treated as missing content")
	}
	if isPermanentPublishFailure(githubAPIError{StatusCode: http.StatusForbidden}) {
		t.Fatal("403 should stay retryable because GitHub uses it for rate limits")
	}
}

func TestGitHubCorpusPublisher_UsesRepositoryDefaultBranch(t *testing.T) {
	task := corpusTaskForServe(score.ExtractedSignals{}, true)
	path := mustCorpusTaskPath(t, task.TaskID)
	var sawRefSHA, sawPutBranch, sawPullBase string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/corpus":
			_, _ = w.Write([]byte(`{"default_branch":"trunk"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/corpus/git/ref/heads/trunk":
			_, _ = w.Write([]byte(`{"object":{"sha":"base-sha"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/corpus/git/refs":
			var body struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create ref: %v", err)
			}
			sawRefSHA = body.SHA
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/corpus/contents/"+path:
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		case r.Method == http.MethodPut && r.URL.Path == "/repos/acme/corpus/contents/"+path:
			var body struct {
				Branch string `json:"branch"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode put content: %v", err)
			}
			sawPutBranch = body.Branch
			_, _ = w.Write([]byte(`{"commit":{"sha":"commit-sha"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/corpus/pulls":
			var body struct {
				Base string `json:"base"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create PR: %v", err)
			}
			sawPullBase = body.Base
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/acme/corpus/pull/7"}`))
		default:
			t.Fatalf("unexpected GitHub request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	publisher := githubCorpusPublisher{client: server.Client(), token: "token", repo: "acme/corpus", baseURL: server.URL}
	mapping, err := publisher.Publish(t.Context(), task, &submitScorecard{Composite: 92})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if mapping.Path != path || mapping.PRURL == "" || mapping.CommitSHA != "commit-sha" {
		t.Fatalf("mapping = %+v", mapping)
	}
	if sawRefSHA != "base-sha" || sawPullBase != "trunk" {
		t.Fatalf("default branch not honored: ref sha=%q pull base=%q", sawRefSHA, sawPullBase)
	}
	if !strings.HasPrefix(sawPutBranch, "proofswe/task/") {
		t.Fatalf("put branch = %q", sawPutBranch)
	}
}

type fakeCorpusPublisher struct {
	calls    atomic.Int32
	failures int
	mapping  corpusMapping
}

func (p *fakeCorpusPublisher) Publish(context.Context, corpus.Task, *submitScorecard) (corpusMapping, error) {
	p.calls.Add(1)
	if p.failures > 0 {
		p.failures--
		return corpusMapping{}, errors.New("temporary github failure")
	}
	return p.mapping, nil
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

type blockingJudge struct {
	calls   *atomic.Int32
	release <-chan struct{}
	verdict judge.Verdict
	err     error
}

func (j blockingJudge) Assess(ctx context.Context, _ []judge.Turn, _ []string) (judge.Verdict, error) {
	j.calls.Add(1)
	select {
	case <-j.release:
	case <-ctx.Done():
		return judge.Verdict{}, ctx.Err()
	}
	if j.err != nil {
		return judge.Verdict{}, j.err
	}
	return j.verdict, nil
}

func publishForLeaderboard(t *testing.T, store *memorySubmissionStore, task corpus.Task, clientVersion string, score float64, note string, mapping corpusMapping) {
	t.Helper()
	rec, err := store.CreateSubmission(t.Context(), submitRequest{SchemaVersion: submitSchemaVersion, ClientVersion: clientVersion, Task: task})
	if err != nil {
		t.Fatalf("create submission: %v", err)
	}
	job, ok, err := store.ClaimJudgeJob(t.Context(), "judge-worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim judge ok=%v err=%v", ok, err)
	}
	if job.SubmissionID != rec.SubmissionID {
		t.Fatalf("claimed submission %q, want %q", job.SubmissionID, rec.SubmissionID)
	}
	if err := store.CompleteJudgeJob(t.Context(), job, judgeRunRecord{
		SubmissionID: rec.SubmissionID,
		Scorecard:    &submitScorecard{Composite: score, ScoreVersion: "score/test", Note: note, Axes: []submitAxis{{Name: "success", Score: score, Detail: note}}},
		Status:       submissionStatusJudged,
		StartedAt:    time.Now(),
		CompletedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("complete judge: %v", err)
	}
	publishJob, ok, err := store.ClaimPublishJob(t.Context(), "publish-worker", time.Now())
	if err != nil || !ok {
		t.Fatalf("claim publish ok=%v err=%v", ok, err)
	}
	if err := store.CompletePublishJob(t.Context(), publishJob, mapping); err != nil {
		t.Fatalf("complete publish: %v", err)
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

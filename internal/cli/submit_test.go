package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
)

func submitTestConfig(t *testing.T, repo string, stdout, stderr io.Writer) Config {
	t.Helper()
	return Config{
		HomeDir: t.TempDir(),
		WorkDir: repo,
		Stdout:  stdout,
		Stderr:  stderr,
		Version: "test",
		Getenv:  func(string) string { return "" },
	}
}

func reproducibleSubmitFixture(t *testing.T) (repo, transcript string) {
	t.Helper()
	gitAvailable(t)
	repo = t.TempDir()
	initRepo(t, repo)
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nADDED_BY_AGENT\n")
	transcript = contributeTranscript(t, "add a feature to keep.txt")
	return repo, transcript
}

func TestSubmitCommand_PostsTaskAndPrintsScorecard(t *testing.T) {
	repo, transcript := reproducibleSubmitFixture(t)
	var sawTask corpus.Task
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("authorization") != "" {
			t.Errorf("unexpected authorization header: %q", r.Header.Get("authorization"))
		}
		var got submitRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		sawTask = got.Task
		_, _ = io.WriteString(w, `{
			"submission_id":"sub_test",
			"task_id":"`+got.Task.TaskID+`",
			"status":"judged",
			"judge":{"status":"server_judged","model":"gpt-5.4-mini","version":"judge/1"},
			"scorecard":{"composite":82,"axes":[{"name":"success","score":88,"detail":"server judged"}],"note":"official server score"},
			"url":"https://proofswe.dev/submissions/sub_test"
		}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	cfg := submitTestConfig(t, repo, &stdout, io.Discard)
	if err := runSubmitCommand(t.Context(), cfg, []string{"--endpoint", server.URL, transcript}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"proofswe submit · judged", "sub_test", "official score: 82 / 100", "server_judged", "https://proofswe.dev/submissions/sub_test"} {
		if !strings.Contains(out, want) {
			t.Errorf("submit output missing %q\n%s", want, out)
		}
	}
	if sawTask.TaskID == "" || len(sawTask.Prompts) == 0 || sawTask.Code.Patch == "" {
		t.Fatalf("server did not receive a complete task: %+v", sawTask)
	}
}

func TestSubmitCommand_NoPathAutoDetectsLatestTranscript(t *testing.T) {
	repo, transcript := reproducibleSubmitFixture(t)
	otherRepo := t.TempDir()
	initRepo(t, otherRepo)
	home := t.TempDir()
	current := filepath.Join(home, ".claude", "projects", "-tmp-current", "current.jsonl")
	other := filepath.Join(home, ".claude", "projects", "-tmp-other", "other.jsonl")
	subagent := filepath.Join(home, ".claude", "projects", "-tmp-current", "session-a", "subagents", "subagent.jsonl")
	for _, path := range []string{current, other, subagent} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	data, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	currentData := strings.ReplaceAll(string(data), `"timestamp":`, `"cwd":`+strconvQuote(repo)+`,"timestamp":`)
	mustWrite(t, current, currentData)
	mustWrite(t, other, `{"type":"user","uuid":"other","sessionId":"other","cwd":`+strconvQuote(otherRepo)+`,"timestamp":"2026-06-01T00:00:00Z","message":{"role":"user","content":"other repo"}}`+"\n")
	mustWrite(t, subagent, `{"type":"user","uuid":"sub","sessionId":"sub","cwd":`+strconvQuote(repo)+`,"timestamp":"2026-06-01T00:00:00Z","message":{"role":"user","content":"subagent prompt"}}`+"\n")
	baseTime := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for path, modTime := range map[string]time.Time{
		current:  baseTime,
		other:    baseTime.Add(time.Hour),
		subagent: baseTime.Add(2 * time.Hour),
	} {
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	var sawTask corpus.Task
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got submitRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		sawTask = got.Task
		_, _ = io.WriteString(w, `{"submission_id":"sub_auto","status":"judged","scorecard":{"composite":75}}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	cfg := submitTestConfig(t, repo, &stdout, io.Discard)
	cfg.HomeDir = home
	if err := runSubmitCommand(t.Context(), cfg, []string{"--endpoint", server.URL}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(sawTask.Prompts) == 0 || sawTask.Prompts[0].Text != "add a feature to keep.txt" {
		t.Fatalf("auto-detected wrong transcript task: %+v", sawTask.Prompts)
	}
	if !strings.Contains(stdout.String(), "official score: 75 / 100") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestSubmitCommand_NoWaitReturnsQueuedResponse(t *testing.T) {
	repo, transcript := reproducibleSubmitFixture(t)
	var getCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getCalls++
			_, _ = io.WriteString(w, `{"submission_id":"sub_wait","status":"judged","scorecard":{"composite":99}}`)
			return
		}
		_, _ = io.WriteString(w, `{"submission_id":"sub_wait","status":"queued","url":"/v1/submissions/sub_wait"}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	cfg := submitTestConfig(t, repo, &stdout, io.Discard)
	if err := runSubmitCommand(t.Context(), cfg, []string{"--no-wait", "--endpoint", server.URL, transcript}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if getCalls != 0 {
		t.Fatalf("--no-wait made %d poll requests", getCalls)
	}
	if !strings.Contains(stdout.String(), "official score pending") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestSubmitCommand_RejectsTooManyPaths(t *testing.T) {
	err := runSubmitCommand(t.Context(), submitTestConfig(t, t.TempDir(), io.Discard, io.Discard), []string{"one.jsonl", "two.jsonl"})
	if err == nil || !strings.Contains(err.Error(), "at most one") {
		t.Fatalf("error = %v, want at most one path", err)
	}
}

func TestSubmitCommand_JSON(t *testing.T) {
	repo, transcript := reproducibleSubmitFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"submission_id":"sub_json","status":"queued","scorecard":{"composite":64}}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	cfg := submitTestConfig(t, repo, &stdout, io.Discard)
	if err := runSubmitCommand(t.Context(), cfg, []string{"--json", "--endpoint", server.URL, transcript}); err != nil {
		t.Fatalf("submit --json: %v", err)
	}
	var got submitResponse
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode json output: %v\n%s", err, stdout.String())
	}
	if got.SubmissionID != "sub_json" || got.Status != "queued" || got.Scorecard == nil || got.Scorecard.Composite != 64 {
		t.Fatalf("json output = %+v", got)
	}
}

func TestSubmitCommand_UsesEnvEndpointAndToken(t *testing.T) {
	repo, transcript := reproducibleSubmitFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer proofswe-token" {
			t.Errorf("authorization = %q, want bearer token", got)
		}
		_, _ = io.WriteString(w, `{"submission_id":"sub_env","status":"judged"}`)
	}))
	defer server.Close()

	cfg := submitTestConfig(t, repo, io.Discard, io.Discard)
	cfg.Getenv = func(k string) string {
		switch k {
		case "PROOFSWE_API_URL":
			return server.URL
		case "PROOFSWE_API_TOKEN":
			return "proofswe-token"
		default:
			return ""
		}
	}
	if err := runSubmitCommand(t.Context(), cfg, []string{transcript}); err != nil {
		t.Fatalf("submit with env endpoint/token: %v", err)
	}
}

func TestSubmitEndpoint_DefaultsToHostedComDomain(t *testing.T) {
	cfg := submitTestConfig(t, t.TempDir(), io.Discard, io.Discard)
	if got := submitEndpoint(cfg, ""); got != "https://proofswe.com/v1/submissions" {
		t.Fatalf("default endpoint = %q", got)
	}
}

func TestSubmitCommand_RejectsNonReproducibleWithoutForce(t *testing.T) {
	cfg := submitTestConfig(t, t.TempDir(), io.Discard, io.Discard)
	transcript := contributeTranscript(t, "do something")
	err := runSubmitCommand(t.Context(), cfg, []string{"--endpoint", "http://127.0.0.1:1", transcript})
	if err == nil {
		t.Fatal("expected non-reproducible task to fail before network")
	}
	if !strings.Contains(err.Error(), "reproducible") {
		t.Fatalf("error = %v, want reproducibility refusal", err)
	}
}

func TestSubmitTask_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "judge unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := submitTask(t.Context(), server.URL, "", submitRequest{SchemaVersion: submitSchemaVersion})
	if err == nil {
		t.Fatal("expected server error")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "judge unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestPollSubmission_WaitsThroughPublishingButStopsAtJudged(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"submission_id":"sub_publish","status":"published","scorecard":{"composite":90},"github_path":"tasks/sha256/aa/task.json"}`)
	}))
	defer server.Close()

	got, err := pollSubmission(t.Context(), server.URL, "", submitResponse{
		SubmissionID: "sub_publish",
		Status:       submissionStatusPublish,
		URL:          server.URL,
		Scorecard:    &submitScorecard{Composite: 90},
	}, time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("poll publishing: %v", err)
	}
	if got.Status != submissionStatusPubDone || calls != 1 {
		t.Fatalf("publishing poll status=%q calls=%d", got.Status, calls)
	}

	got, err = pollSubmission(t.Context(), server.URL, "", submitResponse{
		SubmissionID: "sub_judged",
		Status:       submissionStatusJudged,
		URL:          server.URL,
		Scorecard:    &submitScorecard{Composite: 81},
	}, time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("poll judged: %v", err)
	}
	if got.Status != submissionStatusJudged || calls != 1 {
		t.Fatalf("judged poll status=%q calls=%d", got.Status, calls)
	}
}

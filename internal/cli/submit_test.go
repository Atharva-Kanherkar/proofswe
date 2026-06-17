package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
)

// contributeTranscript writes a minimal claudecode session with the given prompt
// and one edit, enough for contribute to extract a task.
func contributeTranscript(t *testing.T, prompt string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"user","uuid":"u","sessionId":"sess","timestamp":"2026-06-01T00:00:01Z","message":{"role":"user","content":` + strconvQuote(prompt) + `}}`,
		`{"type":"assistant","uuid":"a","sessionId":"sess","timestamp":"2026-06-01T00:00:02Z","message":{"role":"assistant","model":"claude-opus-test","content":[{"type":"text","text":"on it"},{"type":"tool_use","id":"t","name":"Edit","input":{"file":"keep.txt"}}]}}`,
	}
	mustWrite(t, path, strings.Join(lines, "\n")+"\n")
	return path
}

func TestContributeEmitsReproducibleTask(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo) // public origin + MIT license + base commit
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nADDED_BY_AGENT\n")

	out := filepath.Join(t.TempDir(), "task.json")
	cfg := Config{
		HomeDir: t.TempDir(),
		WorkDir: repo,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Getenv:  func(string) string { return "" },
	}
	transcript := contributeTranscript(t, "add a feature to keep.txt")

	if err := runContributeCommand(cfg, []string{"--out", out, transcript}); err != nil {
		t.Fatalf("contribute: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read task.json: %v", err)
	}
	var task corpus.Task
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatalf("task.json is not valid: %v", err)
	}
	if probs := corpus.ReproducibilityProblems(task); len(probs) != 0 {
		t.Fatalf("emitted task is not reproducible: %v", probs)
	}
	if task.Repo.LicenseSPDX != "MIT" || !task.Repo.IsPublic || task.Repo.BaseCommit == "" {
		t.Errorf("repo state not captured: %+v", task.Repo)
	}
	if len(task.Prompts) == 0 || task.Prompts[0].Text != "add a feature to keep.txt" {
		t.Errorf("prompt not captured: %+v", task.Prompts)
	}
	if !strings.Contains(task.Code.Patch, "+ADDED_BY_AGENT") {
		t.Errorf("code patch not captured: %q", task.Code.Patch)
	}
	if task.Scorecard == nil {
		t.Errorf("scorecard missing")
	}
}

func TestContributeAllowsReproducibleMetadataWithoutPatch(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	runGitForTestEnv(t, repo, []string{
		"GIT_AUTHOR_DATE=2026-05-31T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-05-31T00:00:00Z",
	}, "commit", "--amend", "--no-edit", "--date", "2026-05-31T00:00:00Z")
	baseCommit := gitOutputForTest(t, repo, "rev-parse", "HEAD")
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nADDED_BY_AGENT\n")
	commitAllAtForTest(t, repo, "agent work", "2026-06-02T00:00:00Z")
	finalCommit := gitOutputForTest(t, repo, "rev-parse", "HEAD")

	out := filepath.Join(t.TempDir(), "task.json")
	cfg := Config{
		HomeDir: t.TempDir(),
		WorkDir: repo,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Getenv:  func(string) string { return "" },
	}
	transcript := contributeTranscript(t, "add a feature to keep.txt")

	// A historical session (clean tree -> no captured patch) must reproduce from
	// the pre-work commit at transcript start, not the final committed work.
	if err := runContributeCommand(cfg, []string{"--out", out, transcript}); err != nil {
		t.Fatalf("patchless reproducible task refused: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read task.json: %v", err)
	}
	var task corpus.Task
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatalf("task.json is not valid: %v", err)
	}
	if probs := corpus.ReproducibilityProblems(task); len(probs) != 0 {
		t.Fatalf("patchless task flagged non-reproducible: %v", probs)
	}
	if task.Code.Patch != "" {
		t.Errorf("expected empty patch for a clean tree, got %q", task.Code.Patch)
	}
	if task.Repo.BaseCommit != baseCommit {
		t.Errorf("base commit = %s, want historical base %s (final HEAD %s)", task.Repo.BaseCommit, baseCommit, finalCommit)
	}
	if task.Repo.BaseCommitSource != corpus.BaseCommitSourceTranscriptStart {
		t.Errorf("base commit source = %q, want %q", task.Repo.BaseCommitSource, corpus.BaseCommitSourceTranscriptStart)
	}
	if len(task.Prompts) == 0 {
		t.Errorf("prompt not captured")
	}
}

func TestContributeRequiresAgreementForRawCodePublication(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	if err := os.Remove(filepath.Join(repo, "LICENSE")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nADDED_BY_AGENT\n")
	cfg := Config{
		HomeDir: t.TempDir(),
		WorkDir: repo,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Getenv:  func(string) string { return "" },
	}
	transcript := contributeTranscript(t, "add a feature to keep.txt")

	err := runContributeCommand(cfg, []string{"--print", transcript})
	if err == nil || !strings.Contains(err.Error(), "proofswe agent install --accept-code-publication-agreement") {
		t.Fatalf("expected agreement error, got %v", err)
	}
	var stdout bytes.Buffer
	cfg.Stdout = &stdout
	if err := runContributeCommand(cfg, []string{"--accept-code-publication-agreement", "--print", transcript}); err != nil {
		t.Fatalf("contribute with agreement: %v", err)
	}
	var task corpus.Task
	if err := json.Unmarshal(stdout.Bytes(), &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if task.CodePublicationAgreementVersion != corpus.CodePublicationAgreementVersion {
		t.Fatalf("agreement version = %q, want %q", task.CodePublicationAgreementVersion, corpus.CodePublicationAgreementVersion)
	}
}

func TestContributeUsesStoredCodePublicationAgreement(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	if err := os.Remove(filepath.Join(repo, "LICENSE")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nADDED_BY_AGENT\n")
	var stdout bytes.Buffer
	cfg := Config{
		HomeDir: t.TempDir(),
		WorkDir: repo,
		Stdout:  &stdout,
		Stderr:  io.Discard,
		Getenv:  func(string) string { return "" },
	}
	if err := acceptCodePublicationAgreement(cfg); err != nil {
		t.Fatalf("accept agreement: %v", err)
	}
	transcript := contributeTranscript(t, "add a feature to keep.txt")

	if err := runContributeCommand(cfg, []string{"--print", transcript}); err != nil {
		t.Fatalf("contribute with stored agreement: %v", err)
	}
	var task corpus.Task
	if err := json.Unmarshal(stdout.Bytes(), &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if task.CodePublicationAgreementVersion != corpus.CodePublicationAgreementVersion {
		t.Fatalf("agreement version = %q, want %q", task.CodePublicationAgreementVersion, corpus.CodePublicationAgreementVersion)
	}
}

func TestContributeRefusesNonReproducible(t *testing.T) {
	// WorkDir is a bare temp dir: no git remote, no license, no commit.
	cfg := Config{
		HomeDir: t.TempDir(),
		WorkDir: t.TempDir(),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Getenv:  func(string) string { return "" },
	}
	transcript := contributeTranscript(t, "do something")

	err := runContributeCommand(cfg, []string{"--print", transcript})
	if err == nil {
		t.Fatal("expected refusal for non-reproducible session")
	}
	if !strings.Contains(err.Error(), "reproducible") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestContributeForceEmitsNonReproducible(t *testing.T) {
	var stdout bytes.Buffer
	cfg := Config{
		HomeDir: t.TempDir(),
		WorkDir: t.TempDir(),
		Stdout:  &stdout,
		Stderr:  io.Discard,
		Getenv:  func(string) string { return "" },
	}
	transcript := contributeTranscript(t, "do something")

	if err := runContributeCommand(cfg, []string{"--force", "--print", transcript}); err != nil {
		t.Fatalf("force contribute: %v", err)
	}
	var task corpus.Task
	if err := json.Unmarshal(stdout.Bytes(), &task); err != nil {
		t.Fatalf("force output is not valid task.json: %v", err)
	}
	if task.CorpusSchemaVersion != corpus.SchemaVersion {
		t.Errorf("schema version = %d", task.CorpusSchemaVersion)
	}
}

func TestPublicRemoteDetectionAcceptsCommonGitHosts(t *testing.T) {
	tests := []struct {
		remote string
		want   bool
	}{
		{"https://github.com/owner/repo.git", true},
		{"git@github.com:owner/repo.git", true},
		{"ssh://git@gitlab.com/owner/repo.git", true},
		{"git@codeberg.org:owner/repo.git", true},
		{"https://github.enterprise.example/owner/repo.git", false},
		{"../local/repo", false},
	}
	for _, tt := range tests {
		if got := isPublicRemote(tt.remote); got != tt.want {
			t.Errorf("isPublicRemote(%q) = %t, want %t", tt.remote, got, tt.want)
		}
	}
}

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

func TestContributeRefusesReproducibleMetadataWithoutPatch(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo) // clean repo: no agent-produced diff to publish

	cfg := Config{
		HomeDir: t.TempDir(),
		WorkDir: repo,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Getenv:  func(string) string { return "" },
	}
	transcript := contributeTranscript(t, "add a feature to keep.txt")

	err := runContributeCommand(cfg, []string{"--print", transcript})
	if err == nil {
		t.Fatal("expected refusal for a task without a code patch")
	}
	if !strings.Contains(err.Error(), "reproducible") {
		t.Errorf("unexpected error: %v", err)
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

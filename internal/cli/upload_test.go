package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUploadDiscoveryGroupsTranscriptsByRepo(t *testing.T) {
	gitAvailable(t)
	home := t.TempDir()
	repoA := t.TempDir()
	repoB := t.TempDir()
	initRepo(t, repoA)
	initRepo(t, repoB)
	claudePath := writeUploadClaudeTranscript(t, home, repoA, "claude-a", "touch keep.txt")
	codexPath := writeUploadCodexTranscript(t, home, repoB, "codex-b")
	cfg := Config{
		HomeDir: home,
		Getenv: func(key string) string {
			if key == "CODEX_HOME" {
				return filepath.Join(home, ".codex")
			}
			return ""
		},
	}

	groups, err := discoverUploadRepoGroups(t.Context(), cfg, "")
	if err != nil {
		t.Fatalf("discover upload groups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %+v, want two repo groups", groups)
	}
	byRepo := map[string][]uploadTranscript{}
	for _, group := range groups {
		byRepo[group.RepoRoot] = group.Transcripts
	}
	repoAKey := canonicalSubmitPath(repoA)
	repoBKey := canonicalSubmitPath(repoB)
	if got := byRepo[repoAKey]; len(got) != 1 || got[0].Harness != "claudecode" || got[0].Path != claudePath {
		t.Fatalf("repo A transcripts = %+v", got)
	}
	if got := byRepo[repoBKey]; len(got) != 1 || got[0].Harness != "codex" || got[0].Path != codexPath {
		t.Fatalf("repo B transcripts = %+v", got)
	}
}

func TestUploadSelectionParsesRepoAndTranscriptMenus(t *testing.T) {
	got, err := parseUploadSelection("1,3-4", 5, true)
	if err != nil {
		t.Fatalf("parse repo selection: %v", err)
	}
	if want := []int{0, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selection = %v, want %v", got, want)
	}
	got, err = parseUploadSelection("all", 3, true)
	if err != nil {
		t.Fatalf("parse all: %v", err)
	}
	if want := []int{0, 1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("all selection = %v, want %v", got, want)
	}
	if _, err := parseUploadSelection("9", 3, true); err == nil {
		t.Fatal("expected out-of-range selection to fail")
	}
	group := uploadRepoGroup{
		RepoRoot: "/repo",
		Transcripts: []uploadTranscript{
			{Path: "one.jsonl", Harness: "claudecode"},
			{Path: "two.jsonl", Harness: "claudecode"},
			{Path: "three.jsonl", Harness: "codex"},
		},
	}
	var stdout bytes.Buffer
	selected, err := promptUploadTranscriptDeselection(Config{
		Stdin:  strings.NewReader("2\n"),
		Stdout: &stdout,
	}, group)
	if err != nil {
		t.Fatalf("deselect prompt: %v", err)
	}
	if gotPaths := uploadPaths(selected); !reflect.DeepEqual(gotPaths, []string{"one.jsonl", "three.jsonl"}) {
		t.Fatalf("selected paths = %v", gotPaths)
	}
}

func TestUploadCommandNonInteractiveRequiresSelection(t *testing.T) {
	gitAvailable(t)
	home := t.TempDir()
	repo := t.TempDir()
	initRepo(t, repo)
	writeUploadClaudeTranscript(t, home, repo, "claude-a", "touch keep.txt")
	err := runUpload(t.Context(), Config{
		HomeDir: home,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Getenv:  func(string) string { return "" },
	}, uploadOptions{Wait: true, BatchSize: 10})
	if err == nil || !strings.Contains(err.Error(), "--repo or --all") {
		t.Fatalf("error = %v, want non-interactive selection guidance", err)
	}
}

func TestUploadCommandSubmitsSelectedTranscriptsInBatches(t *testing.T) {
	gitAvailable(t)
	home := t.TempDir()
	repo := t.TempDir()
	initRepo(t, repo)
	first := writeUploadClaudeTranscript(t, home, repo, "claude-a", "edit keep.txt")
	second := writeUploadClaudeTranscript(t, home, repo, "claude-b", "edit keep.txt again")
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nADDED_BY_AGENT\n")
	var posted []submitRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got submitRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		posted = append(posted, got)
		_, _ = io.WriteString(w, `{"submission_id":"sub_bulk","status":"queued"}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	cfg := Config{
		HomeDir: home,
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  io.Discard,
		Version: "test",
		Getenv:  func(string) string { return "" },
	}
	err := runUpload(t.Context(), cfg, uploadOptions{
		Repos:        []string{repo},
		Endpoint:     server.URL,
		BatchSize:    1,
		Wait:         false,
		PollInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if len(posted) != 2 {
		t.Fatalf("posted %d requests, want 2", len(posted))
	}
	if posted[0].ClientVersion != "test" || posted[0].Task.TaskID == "" || len(posted[0].Task.Prompts) == 0 {
		t.Fatalf("first posted task incomplete: %+v", posted[0])
	}
	out := stdout.String()
	for _, want := range []string{"Batch 1-1 of 2", "Batch 2-2 of 2", "submitted 2", filepath.Base(first), filepath.Base(second)} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestUploadClaudeProjectSlugIsFilenameSafe(t *testing.T) {
	slug := uploadClaudeProjectSlug(`C:\Users\runneradmin\repo`)
	for _, invalid := range []string{":", "<", ">", `"`, "|", "?", "*"} {
		if strings.Contains(slug, invalid) {
			t.Fatalf("slug %q contains invalid path character %q", slug, invalid)
		}
	}
}

func writeUploadClaudeTranscript(t *testing.T, home, repo, sessionID, prompt string) string {
	t.Helper()
	slug := uploadClaudeProjectSlug(repo)
	path := filepath.Join(home, ".claude", "projects", slug, sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"user","uuid":"u-` + sessionID + `","sessionId":"` + sessionID + `","cwd":` + strconvQuote(repo) + `,"timestamp":"2026-06-01T00:00:01Z","message":{"role":"user","content":` + strconvQuote(prompt) + `}}`,
		`{"type":"assistant","uuid":"a-` + sessionID + `","sessionId":"` + sessionID + `","cwd":` + strconvQuote(repo) + `,"timestamp":"2026-06-01T00:00:02Z","message":{"role":"assistant","model":"claude-opus-test","content":[{"type":"text","text":"on it"},{"type":"tool_use","id":"t","name":"Edit","input":{"file":"keep.txt"}}]}}`,
	}
	mustWrite(t, path, strings.Join(lines, "\n")+"\n")
	if err := os.Chtimes(path, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	return path
}

func uploadClaudeProjectSlug(repo string) string {
	slug := strings.ReplaceAll(filepath.ToSlash(canonicalSubmitPath(repo)), "/", "-")
	return strings.NewReplacer(":", "-", "<", "-", ">", "-", `"`, "-", "|", "-", "?", "-", "*", "-").Replace(slug)
}

func writeUploadCodexTranscript(t *testing.T, home, repo, sessionID string) string {
	t.Helper()
	path := filepath.Join(home, ".codex", "sessions", "2026", "06", "01", "rollout-2026-06-01T00-00-00-"+sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, `{"cwd":`+strconvQuote(repo)+`,"timestamp":"2026-06-01T00:00:00Z"}`+"\n")
	return path
}

func uploadPaths(items []uploadTranscript) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Path)
	}
	return out
}

package cli

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/hashing"
)

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	mustWrite(t, filepath.Join(dir, "keep.txt"), "line1\nline2\n")
	mustWrite(t, filepath.Join(dir, "LICENSE"), "MIT License\n")
	run("add", "keep.txt")
	run("add", "LICENSE")
	run("commit", "-m", "init")
	run("remote", "add", "origin", "https://github.com/Atharva-Kanherkar/proofswe.git")
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// snapshotConfig returns a Config rooted at an isolated home, and the matching salt.
func snapshotConfig(t *testing.T, repo string) (Config, hashing.Hasher) {
	t.Helper()
	home := t.TempDir()
	cfg := Config{HomeDir: home, WorkDir: repo, Stdout: io.Discard, Stderr: io.Discard}
	// Force the salt to exist so the test hashes with the same key snapshot uses.
	salt, err := hashing.LoadSalt(proofsweStateDir(cfg))
	if err != nil {
		t.Fatalf("LoadSalt: %v", err)
	}
	return cfg, hashing.New(salt)
}

func readPending(t *testing.T, cfg Config, sessionID string) PendingRecord {
	t.Helper()
	data, err := os.ReadFile(pendingRecordPath(cfg, sessionID))
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	var rec PendingRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("decode pending: %v", err)
	}
	return rec
}

func enableCodeConsent(t *testing.T, cfg Config) {
	t.Helper()
	if err := runConsentSet(cfg, []string{"--tier=code"}); err != nil {
		t.Fatalf("enable code consent: %v", err)
	}
}

// Acceptance 1 + 3: the default tier still produces exactly the expected salted
// line hashes for keeprate, and stores no raw code in the pending/task records.
func TestDefaultTierCapturesSaltedPendingLineHashes(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	cfg, h := snapshotConfig(t, repo)

	// Agent edits a tracked file (adds two lines) and creates an untracked file.
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nADDED_ALPHA\n  ADDED_BETA  \n")
	mustWrite(t, filepath.Join(repo, "new.go"), "NEW_GAMMA\n\nNEW_DELTA\n")

	in := hookInput{SessionID: "sess-1", CWD: repo}
	if err := snapshot(cfg, "claudecode", in, time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	rec := readPending(t, cfg, "sess-1")

	want := map[PendingLine]bool{
		{PathHash: h.StringHash("keep.txt"), LineHash: h.StringHash("ADDED_ALPHA")}: true,
		{PathHash: h.StringHash("keep.txt"), LineHash: h.StringHash("ADDED_BETA")}:  true, // trimmed
		{PathHash: h.StringHash("new.go"), LineHash: h.StringHash("NEW_GAMMA")}:     true,
		{PathHash: h.StringHash("new.go"), LineHash: h.StringHash("NEW_DELTA")}:     true,
	}
	got := map[PendingLine]bool{}
	for _, l := range rec.Lines {
		got[l] = true
	}
	if len(got) != len(want) {
		t.Fatalf("line count = %d, want %d\n got=%v", len(rec.Lines), len(want), rec.Lines)
	}
	for l := range want {
		if !got[l] {
			t.Fatalf("missing expected hashed line %+v\n got=%v", l, rec.Lines)
		}
	}

	task, err := findTaskRecordBySession(cfg, "sess-1")
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if task.Code.Patch != "" || task.Code.TestPatch != "" || task.Repo.RemoteURL != "" {
		t.Fatalf("default task record leaked raw code/repo content: %+v", task)
	}

	// No raw code anywhere in the serialized pending or task records.
	pendingRaw, _ := os.ReadFile(pendingRecordPath(cfg, "sess-1"))
	taskPath, err := taskRecordPath(cfg, task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	taskRaw, _ := os.ReadFile(taskPath)
	raw := string(pendingRaw) + string(taskRaw)
	for _, secret := range []string{"ADDED_ALPHA", "ADDED_BETA", "NEW_GAMMA", "NEW_DELTA", "line1"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("record leaked raw content %q:\n%s", secret, raw)
		}
	}
	if rec.SchemaVersion != pendingSchemaVersion {
		t.Fatalf("schema_version = %d, want %d", rec.SchemaVersion, pendingSchemaVersion)
	}
}

// Acceptance 2: a non-git cwd writes hashes-only records and does not error.
func TestSnapshotNonGitCwdWritesNothing(t *testing.T) {
	gitAvailable(t)
	plain := t.TempDir() // not a git repo
	cfg, _ := snapshotConfig(t, plain)

	in := hookInput{SessionID: "sess-x", CWD: plain}
	if err := snapshot(cfg, "claudecode", in, time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatalf("snapshot non-git: %v", err)
	}
	rec := readPending(t, cfg, "sess-x")
	if rec.RepoPath != "" || len(rec.Lines) != 0 {
		t.Fatalf("non-git default pending = %+v, want no repo path or lines", rec)
	}
}

// Acceptance 4: metadata (model, turns, tool calls) is populated from the adapter
// for both Claude Code and Codex transcripts.
func TestSnapshotMetadataFromAdapter(t *testing.T) {
	gitAvailable(t)

	claudeTranscript := strings.Join([]string{
		`{"type":"started","uuid":"a","sessionId":"s","timestamp":"2026-06-01T00:00:00Z","cwd":"/w","gitBranch":"main"}`,
		`{"type":"user","uuid":"b","sessionId":"s","timestamp":"2026-06-01T00:00:01Z","message":{"role":"user","content":"do the thing"}}`,
		`{"type":"assistant","uuid":"c","sessionId":"s","timestamp":"2026-06-01T00:00:05Z","message":{"role":"assistant","model":"claude-opus-4-7","content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
	}, "\n") + "\n"

	codexTranscript := strings.Join([]string{
		`{"timestamp":"2026-06-01T00:00:00Z","type":"session_meta","payload":{"id":"s","cwd":"/w","git":{"branch":"main"}}}`,
		`{"timestamp":"2026-06-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"t","cwd":"/w","model":"gpt-5-codex"}}`,
		`{"timestamp":"2026-06-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"do the thing"}]}}`,
		`{"timestamp":"2026-06-01T00:00:03Z","type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"c1","arguments":"{}"}}`,
		`{"timestamp":"2026-06-01T00:00:06Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`,
	}, "\n") + "\n"

	cases := []struct {
		harness    string
		transcript string
		wantModel  string
	}{
		{"claudecode", claudeTranscript, "claude-opus-4-7"},
		{"codex", codexTranscript, "gpt-5-codex"},
	}
	for _, tc := range cases {
		t.Run(tc.harness, func(t *testing.T) {
			repo := t.TempDir()
			initRepo(t, repo)
			cfg, _ := snapshotConfig(t, repo)
			enableCodeConsent(t, cfg)
			mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nEDIT\n")

			tpath := filepath.Join(t.TempDir(), "transcript.jsonl")
			mustWrite(t, tpath, tc.transcript)

			in := hookInput{SessionID: "sess-" + tc.harness, CWD: repo, TranscriptPath: tpath}
			if err := snapshot(cfg, tc.harness, in, time.Unix(1_700_000_000, 0)); err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			rec := readPending(t, cfg, "sess-"+tc.harness)
			if rec.Model != tc.wantModel {
				t.Fatalf("model = %q, want %q", rec.Model, tc.wantModel)
			}
			if rec.TurnCount != 1 {
				t.Fatalf("turn_count = %d, want 1", rec.TurnCount)
			}
			if rec.ToolCallCount != 1 {
				t.Fatalf("tool_call_count = %d, want 1", rec.ToolCallCount)
			}
			if rec.DurationMS <= 0 {
				t.Fatalf("duration_ms = %d, want > 0", rec.DurationMS)
			}
		})
	}
}

// Acceptance 5: re-running snapshot for the same session id overwrites in place.
func TestSnapshotIsIdempotent(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	cfg, _ := snapshotConfig(t, repo)
	enableCodeConsent(t, cfg)
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nONE\n")

	in := hookInput{SessionID: "dup", CWD: repo}
	if err := snapshot(cfg, "claudecode", in, time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatalf("snapshot 1: %v", err)
	}
	first := readPending(t, cfg, "dup")

	// More work, snapshot again with the same session id.
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nONE\nTWO\n")
	if err := snapshot(cfg, "claudecode", in, time.Unix(1_700_000_100, 0)); err != nil {
		t.Fatalf("snapshot 2: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(proofsweStateDir(cfg), "pending"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("pending files = %d, want 1 (idempotent overwrite)", len(entries))
	}
	second := readPending(t, cfg, "dup")
	if len(second.Lines) <= len(first.Lines) {
		t.Fatalf("second snapshot should reflect more added lines: first=%d second=%d", len(first.Lines), len(second.Lines))
	}
}

// The hook entrypoint reads session info from stdin and writes a record.
func TestHookStopTriggersSnapshotFromStdin(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	home := t.TempDir()
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nHOOKLINE\n")

	// Marshal so a Windows repo path's backslashes are JSON-escaped correctly.
	stdin, err := json.Marshal(map[string]string{"session_id": "hooked", "cwd": repo, "transcript_path": ""})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Args:    []string{"hook", "codex", "Stop"},
		Stdin:   strings.NewReader(string(stdin)),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		HomeDir: home,
		WorkDir: repo,
		Getenv:  func(string) string { return "" },
	}
	if err := runHook(t.Context(), cfg, cfg.Args[1:]); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	if _, err := os.Stat(pendingRecordPath(cfg, "hooked")); err != nil {
		t.Fatalf("expected pending record from hook, err = %v", err)
	}
}

package cli

import (
	"bytes"
	"context"
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

func commitAll(t *testing.T, dir, msg string) {
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
	run("add", ".")
	run("commit", "-m", msg)
}

func writePendingRecord(t *testing.T, cfg Config, sessionID string, record PendingRecord) {
	t.Helper()
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(pendingRecordPath(cfg, sessionID), data, 0o600); err != nil {
		t.Fatalf("write pending: %v", err)
	}
}

func pendingForTest(h hashing.Hasher, repo, sessionID string, capturedAt time.Time, lines ...lineRef) PendingRecord {
	pendingLines := make([]PendingLine, 0, len(lines))
	for _, line := range lines {
		pendingLines = append(pendingLines, PendingLine{
			PathHash: h.StringHash(line.path),
			LineHash: h.StringHash(strings.TrimSpace(line.text)),
		})
	}
	return PendingRecord{
		SchemaVersion: pendingSchemaVersion,
		Harness:       "codex",
		SessionID:     sessionID,
		RepoPath:      repo,
		CapturedAt:    capturedAt,
		Model:         "gpt-test",
		TurnCount:     3,
		ToolCallCount: 4,
		Lines:         pendingLines,
	}
}

func readDatapoints(t *testing.T, cfg Config) []ResolvedDatapoint {
	t.Helper()
	data, err := os.ReadFile(dataLogPath(cfg))
	if err != nil {
		t.Fatalf("read data log: %v", err)
	}
	var points []ResolvedDatapoint
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var point ResolvedDatapoint
		if err := json.Unmarshal(line, &point); err != nil {
			t.Fatalf("decode datapoint: %v\n%s", err, line)
		}
		points = append(points, point)
	}
	return points
}

func TestResolveKeeprateAndCommittedFromWorkingTreeAndHEAD(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	cfg, h := snapshotConfig(t, repo)
	now := time.Unix(1_700_100_000, 0).UTC()

	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nCOMMITTED_LINE\n")
	commitAll(t, repo, "agent line landed")
	mustWrite(t, filepath.Join(repo, "keep.txt"), "line1\nline2\nCOMMITTED_LINE\nWORKTREE_LINE\n")

	record := pendingForTest(h, repo, "sess-1", now.Add(-25*time.Hour),
		lineRef{path: "keep.txt", text: "COMMITTED_LINE"},
		lineRef{path: "keep.txt", text: "WORKTREE_LINE"},
		lineRef{path: "keep.txt", text: "MISSING_LINE"},
	)
	writePendingRecord(t, cfg, "sess-1", record)

	if err := resolvePending(cfg, resolveOptions{Maturity: 24 * time.Hour, Now: func() time.Time { return now }}); err != nil {
		t.Fatalf("resolvePending: %v", err)
	}
	points := readDatapoints(t, cfg)
	if len(points) != 1 {
		t.Fatalf("datapoints = %d, want 1", len(points))
	}
	got := points[0]
	if got.SchemaVersion != dataSchemaVersion || got.EventType != resolvedEventType {
		t.Fatalf("schema/event = %d/%q, want %d/%q", got.SchemaVersion, got.EventType, dataSchemaVersion, resolvedEventType)
	}
	if got.Model != "gpt-test" || got.Harness != "codex" || got.Turns != 3 || got.ToolCalls != 4 {
		t.Fatalf("metadata = %+v, want pending metadata", got)
	}
	if got.RepoHash != h.StringHash(repo) {
		t.Fatalf("repo_hash = %q, want salted repo hash", got.RepoHash)
	}
	if got.LinesAdded != 3 || got.LinesSurvived != 2 {
		t.Fatalf("line counts = %d/%d, want 2/3 survived", got.LinesSurvived, got.LinesAdded)
	}
	if got.Keeprate != float64(2)/float64(3) {
		t.Fatalf("keeprate = %v, want 2/3", got.Keeprate)
	}
	if !got.Committed {
		t.Fatalf("committed = false, want true")
	}
	if _, err := os.Stat(pendingRecordPath(cfg, "sess-1")); !os.IsNotExist(err) {
		t.Fatalf("pending record still exists, err=%v", err)
	}

	raw, err := os.ReadFile(dataLogPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"COMMITTED_LINE", "WORKTREE_LINE", "MISSING_LINE", repo} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("data log leaked %q:\n%s", leaked, raw)
		}
	}
}

func TestResolveRespectsMaturityWindow(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	cfg, h := snapshotConfig(t, repo)
	now := time.Unix(1_700_100_000, 0).UTC()

	record := pendingForTest(h, repo, "young", now.Add(-time.Hour),
		lineRef{path: "keep.txt", text: "line1"},
	)
	writePendingRecord(t, cfg, "young", record)

	if err := resolvePending(cfg, resolveOptions{Maturity: 24 * time.Hour, Now: func() time.Time { return now }}); err != nil {
		t.Fatalf("resolvePending: %v", err)
	}
	if _, err := os.Stat(dataLogPath(cfg)); !os.IsNotExist(err) {
		t.Fatalf("data log should not exist for immature record, err=%v", err)
	}
	if _, err := os.Stat(pendingRecordPath(cfg, "young")); err != nil {
		t.Fatalf("pending record should remain: %v", err)
	}
}

func TestResolveRenamedLineDoesNotSurvive(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	cfg, h := snapshotConfig(t, repo)
	now := time.Unix(1_700_100_000, 0).UTC()

	record := pendingForTest(h, repo, "rename", now.Add(-25*time.Hour),
		lineRef{path: "old.txt", text: "SAME_LINE"},
	)
	writePendingRecord(t, cfg, "rename", record)
	mustWrite(t, filepath.Join(repo, "new.txt"), "SAME_LINE\n")

	if err := resolvePending(cfg, resolveOptions{Maturity: 24 * time.Hour, Now: func() time.Time { return now }}); err != nil {
		t.Fatalf("resolvePending: %v", err)
	}
	got := readDatapoints(t, cfg)[0]
	if got.LinesAdded != 1 || got.LinesSurvived != 0 || got.Keeprate != 0 || got.Committed {
		t.Fatalf("renamed line datapoint = %+v, want not survived and not committed", got)
	}
}

func TestResolveZeroLinePendingRecord(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	cfg, h := snapshotConfig(t, repo)
	now := time.Unix(1_700_100_000, 0).UTC()

	record := pendingForTest(h, repo, "zero", now.Add(-25*time.Hour))
	writePendingRecord(t, cfg, "zero", record)

	if err := resolvePending(cfg, resolveOptions{Maturity: 24 * time.Hour, Now: func() time.Time { return now }}); err != nil {
		t.Fatalf("resolvePending: %v", err)
	}
	got := readDatapoints(t, cfg)[0]
	if got.LinesAdded != 0 || got.LinesSurvived != 0 || got.Keeprate != 0 || got.Committed {
		t.Fatalf("zero-line datapoint = %+v, want zero values", got)
	}
}

func TestResolveSkipsInvalidPendingRecord(t *testing.T) {
	repo := t.TempDir()
	cfg, _ := snapshotConfig(t, repo)
	path := pendingRecordPath(cfg, "bad")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := resolvePending(cfg, resolveOptions{Maturity: 0, Now: func() time.Time { return time.Unix(1_700_100_000, 0) }})
	if err == nil || !strings.Contains(err.Error(), "decode pending record") {
		t.Fatalf("resolvePending error = %v, want decode error", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("invalid pending should remain: %v", statErr)
	}
	if _, statErr := os.Stat(dataLogPath(cfg)); !os.IsNotExist(statErr) {
		t.Fatalf("data log should not exist, err=%v", statErr)
	}
}

func TestResolveCommandUsesSamePath(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	cfg, h := snapshotConfig(t, repo)
	record := pendingForTest(h, repo, "cmd", time.Unix(1_700_000_000, 0),
		lineRef{path: "keep.txt", text: "line1"},
	)
	writePendingRecord(t, cfg, "cmd", record)
	cfg.Args = []string{"resolve", "--maturity=0s"}

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run(resolve): %v", err)
	}
	got := readDatapoints(t, cfg)[0]
	if got.LinesAdded != 1 || got.LinesSurvived != 1 || !got.Committed {
		t.Fatalf("resolve command datapoint = %+v, want committed surviving line", got)
	}
}

func TestHookSessionStartTriggersResolveAfterNotice(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	cfg, h := snapshotConfig(t, repo)
	now := time.Now().UTC()
	record := pendingForTest(h, repo, "hook-resolve", now.Add(-25*time.Hour),
		lineRef{path: "keep.txt", text: "line1"},
	)
	writePendingRecord(t, cfg, "hook-resolve", record)

	var stderr bytes.Buffer
	cfg.Args = []string{"hook", "codex", "SessionStart"}
	cfg.Stdin = strings.NewReader("{}")
	cfg.Stdout = io.Discard
	cfg.Stderr = &stderr
	cfg.Getenv = func(string) string { return "" }
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run(hook SessionStart): %v", err)
	}
	if !strings.HasPrefix(stderr.String(), noticeLine+"\n") {
		t.Fatalf("stderr = %q, want notice first", stderr.String())
	}
	if got := readDatapoints(t, cfg)[0]; got.LinesSurvived != 1 {
		t.Fatalf("hook datapoint = %+v, want resolved", got)
	}
}

package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/adapter/claudecode"
	"github.com/Atharva-Kanherkar/proofswe/internal/adapter/codex"
	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/hashing"
)

const pendingSchemaVersion = 1

// PendingRecord is the snapshot written on session end: privacy-safe line hashes
// of what the agent produced plus session metadata. The resolve phase (#9) reads
// it on the next SessionStart to compute survival. RepoPath is kept in cleartext
// (local-only) because resolve must re-open the repo; line content is never stored
// — only salted hashes.
type PendingRecord struct {
	SchemaVersion int           `json:"schema_version"`
	Harness       string        `json:"harness"`
	SessionID     string        `json:"session_id"`
	RepoPath      string        `json:"repo_path"`
	CapturedAt    time.Time     `json:"captured_at"`
	Model         string        `json:"model,omitempty"`
	TurnCount     int           `json:"turn_count"`
	ToolCallCount int           `json:"tool_call_count"`
	DurationMS    int64         `json:"duration_ms,omitempty"`
	Lines         []PendingLine `json:"lines"`
}

// PendingLine is one agent-produced added line: salted hashes of its repo-relative
// path and its trimmed text. No raw content.
type PendingLine struct {
	PathHash string `json:"path_hash"`
	LineHash string `json:"line_hash"`
}

// hookInput is the JSON each harness writes to the hook's stdin. Claude Code and
// Codex both use these snake_case fields; Codex additionally supplies model and
// may send a null transcript_path.
type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Model          string `json:"model"`
}

func parseHookInput(r io.Reader) (hookInput, error) {
	var in hookInput
	if r == nil {
		return in, nil
	}
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return in, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return in, nil
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return in, err
	}
	return in, nil
}

// snapshot writes the pending record for a just-ended session. It is best-effort:
// a non-git cwd, an unavailable salt, or a missing transcript degrades gracefully
// (the caller logs and exits 0) so capture never disrupts the user's session.
func snapshot(cfg Config, harness string, in hookInput, now time.Time) error {
	return snapshotContext(context.Background(), cfg, harness, in, now)
}

func snapshotContext(ctx context.Context, cfg Config, harness string, in hookInput, now time.Time) error {
	cwd := in.CWD
	if cwd == "" {
		cwd = cfg.WorkDir
	}

	salt, err := hashing.LoadSalt(proofsweStateDir(cfg))
	if err != nil {
		return fmt.Errorf("load hash salt: %w", err)
	}
	h := hashing.New(salt)
	resolved, err := effectiveConsent(cfg, "")
	if err != nil {
		return fmt.Errorf("resolve consent: %w", err)
	}

	task, _, err := captureTaskRecord(ctx, cfg, harness, in, now, salt, resolved)
	if err != nil {
		return err
	}
	if err := writeCapturedTask(cfg, task); err != nil {
		return err
	}

	var repoRoot string
	var added []lineRef
	var ok bool
	repoRoot, ok = gitRepoRootContext(ctx, cwd)
	if ok {
		added, err = gitAddedLinesContext(ctx, repoRoot)
		if err != nil {
			return fmt.Errorf("compute added lines: %w", err)
		}
	}

	lines := make([]PendingLine, 0, len(added))
	for _, ref := range added {
		normalized := strings.TrimSpace(ref.text)
		if normalized == "" {
			continue
		}
		lines = append(lines, PendingLine{
			PathHash: h.StringHash(ref.path),
			LineHash: h.StringHash(normalized),
		})
	}

	meta := sessionMetadata(harness, in, salt)
	record := PendingRecord{
		SchemaVersion: pendingSchemaVersion,
		Harness:       harness,
		SessionID:     in.SessionID,
		RepoPath:      repoRoot,
		CapturedAt:    now.UTC(),
		Model:         meta.model,
		TurnCount:     meta.turns,
		ToolCallCount: meta.toolCalls,
		DurationMS:    meta.durationMS,
		Lines:         lines,
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// Idempotent: one file per session id, overwritten on re-run.
	return writeFileAtomic(pendingRecordPath(cfg, in.SessionID), data, 0o600)
}

type lineRef struct {
	path string
	text string
}

func gitRepoRoot(cwd string) (string, bool) {
	return gitRepoRootContext(context.Background(), cwd)
}

func gitRepoRootContext(ctx context.Context, cwd string) (string, bool) {
	if cwd == "" {
		return "", false
	}
	out, err := runGitContext(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	return root, true
}

// gitAddedLines returns the lines the agent added: working-tree additions vs HEAD
// (tracked files) plus the full contents of untracked, non-ignored files. Paths
// are repo-relative. Committed-during-session changes need a session-start baseline
// and are deferred (see the scope-metric open decision).
func gitAddedLinesContext(ctx context.Context, root string) ([]lineRef, error) {
	var refs []lineRef

	if _, err := runGitContext(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		// core.quotePath=false keeps non-ASCII paths unquoted so header parsing is exact.
		diff, err := runGitContext(ctx, root, "-c", "core.quotePath=false", "diff", "--src-prefix=a/", "--dst-prefix=b/", "--no-color", "HEAD")
		if err != nil {
			return nil, err
		}
		refs = append(refs, parseDiffAddedLines(diff)...)
	}

	out, err := runGitContext(ctx, root, "-c", "core.quotePath=false", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, rel := range splitNUL(out) {
		rel = normalizeRepoRelativePath(rel)
		if rel == "" {
			continue
		}
		content, err := readRepoFile(root, rel)
		if err != nil || isBinary(content) {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(content))
		scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			refs = append(refs, lineRef{path: rel, text: scanner.Text()})
		}
	}
	return refs, nil
}

// parseDiffAddedLines extracts added lines (and their repo-relative file) from a
// unified `git diff` — lines starting with '+' but not the '+++' file header.
func parseDiffAddedLines(diff []byte) []lineRef {
	var refs []lineRef
	current := ""
	scanner := bufio.NewScanner(bytes.NewReader(diff))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			path := strings.TrimPrefix(line, "+++ ")
			if path == "/dev/null" {
				current = ""
				continue
			}
			current = normalizeRepoRelativePath(strings.TrimPrefix(path, "b/"))
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if current != "" {
				refs = append(refs, lineRef{path: current, text: line[1:]})
			}
		}
	}
	return refs
}

// normalizeRepoRelativePath canonicalizes a git-emitted path so the capture
// side hashes the same string the resolve side does. Git emits forward-slash,
// repo-relative paths on every platform under core.quotePath=false; Clean+ToSlash
// round-trips those identically while collapsing any "./" prefix. It deliberately
// does NOT rewrite backslashes: resolve.go hashes raw git paths, so munging "\"
// here would desync the two sides for a (legal, rare) backslash filename on POSIX.
func normalizeRepoRelativePath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." {
		return ""
	}
	return strings.TrimPrefix(path, "./")
}

type metadata struct {
	model      string
	turns      int
	toolCalls  int
	durationMS int64
}

// sessionMetadata pulls model, turn count, tool-call count, and duration from the
// session transcript via the matching adapter. Falls back to the harness-provided
// model hint when the transcript is unavailable (Codex may send a null path).
func sessionMetadata(harness string, in hookInput, salt []byte) metadata {
	meta := metadata{model: in.Model}
	if in.TranscriptPath == "" {
		return meta
	}

	events, err := parseTranscript(harness, salt, in.TranscriptPath)
	if err != nil {
		return meta
	}

	var minTS, maxTS time.Time
	for _, event := range events {
		env := eventEnvelope(event)
		if ts := env.Event.Timestamp; !ts.IsZero() {
			if minTS.IsZero() || ts.Before(minTS) {
				minTS = ts
			}
			if ts.After(maxTS) {
				maxTS = ts
			}
		}
		if env.Model.ID != "" {
			meta.model = string(env.Model.ID)
		}
		switch event.(type) {
		case *core.UserPrompt:
			meta.turns++
		case *core.ToolCall:
			meta.toolCalls++
		}
	}
	if !minTS.IsZero() && maxTS.After(minTS) {
		meta.durationMS = maxTS.Sub(minTS).Milliseconds()
	}
	return meta
}

func parseTranscript(harness string, salt []byte, path string) ([]core.NormalizedEvent, error) {
	switch harness {
	case "claudecode":
		return claudecode.ParseFile(salt, path)
	case "codex":
		return codex.ParseFile(salt, path)
	default:
		return nil, fmt.Errorf("unknown harness %q", harness)
	}
}

func eventEnvelope(event core.NormalizedEvent) core.Envelope {
	switch e := event.(type) {
	case *core.SessionStart:
		return e.Envelope
	case *core.UserPrompt:
		return e.Envelope
	case *core.AssistantMessage:
		return e.Envelope
	case *core.ToolCall:
		return e.Envelope
	case *core.ToolResult:
		return e.Envelope
	case *core.SessionEnd:
		return e.Envelope
	default:
		return core.Envelope{}
	}
}

func runGit(dir string, args ...string) ([]byte, error) {
	return runGitContext(context.Background(), dir, args...)
}

func runGitContext(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func readRepoFile(root, rel string) ([]byte, error) {
	rel = strings.ReplaceAll(rel, "\\", string(filepath.Separator))
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return nil, fmt.Errorf("unsafe repo-relative path %q", rel)
	}
	return readFileLimited(filepath.Join(root, clean))
}

func readFileLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	// Cap per-file reads so a pathological untracked blob can't blow memory.
	return io.ReadAll(io.LimitReader(f, 16<<20))
}

func splitNUL(data []byte) []string {
	parts := strings.Split(string(data), "\x00")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}

func proofsweStateDir(cfg Config) string {
	return filepath.Join(homeDir(cfg), ".proofswe")
}

func pendingRecordPath(cfg Config, sessionID string) string {
	name := strings.ReplaceAll(strings.ReplaceAll(sessionID, "/", "_"), string(filepath.Separator), "_")
	if name == "" {
		name = "unknown-session"
	}
	return filepath.Join(proofsweStateDir(cfg), "pending", name+".json")
}

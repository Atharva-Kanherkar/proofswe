package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/hashing"
)

const (
	dataSchemaVersion      = 1
	resolvedEventType      = "session_resolved"
	defaultResolveMaturity = 24 * time.Hour
)

// ResolvedDatapoint is the append-only data.jsonl record emitted when a pending
// snapshot matures. It intentionally contains no raw paths or code content.
type ResolvedDatapoint struct {
	SchemaVersion  int       `json:"schema_version"`
	EventType      string    `json:"event_type"`
	Timestamp      time.Time `json:"ts"`
	Model          string    `json:"model,omitempty"`
	Harness        string    `json:"harness"`
	RepoHash       string    `json:"repo_hash"`
	Turns          int       `json:"turns"`
	ToolCalls      int       `json:"tool_calls"`
	LinesAdded     int       `json:"lines_added"`
	LinesSurvived  int       `json:"lines_survived"`
	Keeprate       float64   `json:"keeprate"`
	Committed      bool      `json:"committed"`
	ResolvedAfterH float64   `json:"resolved_after_h"`
}

type resolveOptions struct {
	Maturity time.Duration
	Now      func() time.Time
}

func (o resolveOptions) normalized() resolveOptions {
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

func runResolveCommand(cfg Config, args []string) error {
	flags := flag.NewFlagSet("resolve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	maturity := defaultResolveMaturity
	flags.DurationVar(&maturity, "maturity", defaultResolveMaturity, "minimum pending record age")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%w: resolve takes no positional arguments", ErrUsage)
	}
	if maturity < 0 {
		return fmt.Errorf("%w: --maturity must be non-negative", ErrUsage)
	}

	return resolvePending(cfg, resolveOptions{Maturity: maturity, Now: time.Now})
}

func resolvePending(cfg Config, opts resolveOptions) error {
	opts = opts.normalized()
	now := opts.Now().UTC()

	pendingDir := filepath.Join(proofsweStateDir(cfg), "pending")
	entries, err := os.ReadDir(pendingDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	salt, err := hashing.LoadSalt(proofsweStateDir(cfg))
	if err != nil {
		return fmt.Errorf("load hash salt: %w", err)
	}
	h := hashing.New(salt)

	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(pendingDir, entry.Name())
		if err := resolvePendingFile(cfg, h, path, now, opts.Maturity); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Name(), err))
		}
	}
	return errors.Join(errs...)
}

func resolvePendingFile(cfg Config, h hashing.Hasher, path string, now time.Time, maturity time.Duration) error {
	record, err := readPendingRecordFile(path)
	if err != nil {
		return err
	}
	if record.SchemaVersion != pendingSchemaVersion {
		return fmt.Errorf("unsupported pending schema_version %d", record.SchemaVersion)
	}
	if now.Sub(record.CapturedAt.UTC()) < maturity {
		return nil
	}

	datapoint, err := resolveRecord(h, record, now)
	if err != nil {
		return err
	}
	if err := appendDatapoint(cfg, datapoint); err != nil {
		return fmt.Errorf("append datapoint: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove pending record: %w", err)
	}
	return nil
}

func readPendingRecordFile(path string) (PendingRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PendingRecord{}, err
	}
	var record PendingRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return PendingRecord{}, fmt.Errorf("decode pending record: %w", err)
	}
	return record, nil
}

func resolveRecord(h hashing.Hasher, record PendingRecord, now time.Time) (ResolvedDatapoint, error) {
	if record.RepoPath == "" {
		return ResolvedDatapoint{}, fmt.Errorf("pending record missing repo_path")
	}

	wanted := wantedPathHashes(record.Lines)
	working, err := workingTreeLineIndex(record.RepoPath, h, wanted)
	if err != nil {
		return ResolvedDatapoint{}, fmt.Errorf("index working tree: %w", err)
	}
	head, err := headLineIndex(record.RepoPath, h, wanted)
	if err != nil {
		return ResolvedDatapoint{}, fmt.Errorf("index HEAD: %w", err)
	}

	linesSurvived := 0
	committed := false
	for _, line := range record.Lines {
		inWorking := working.contains(line.PathHash, line.LineHash)
		inHead := head.contains(line.PathHash, line.LineHash)
		if inWorking || inHead {
			linesSurvived++
		}
		if inHead {
			committed = true
		}
	}

	linesAdded := len(record.Lines)
	keeprate := 0.0
	if linesAdded > 0 {
		keeprate = float64(linesSurvived) / float64(linesAdded)
	}

	return ResolvedDatapoint{
		SchemaVersion:  dataSchemaVersion,
		EventType:      resolvedEventType,
		Timestamp:      now,
		Model:          record.Model,
		Harness:        record.Harness,
		RepoHash:       h.StringHash(record.RepoPath),
		Turns:          record.TurnCount,
		ToolCalls:      record.ToolCallCount,
		LinesAdded:     linesAdded,
		LinesSurvived:  linesSurvived,
		Keeprate:       keeprate,
		Committed:      committed,
		ResolvedAfterH: now.Sub(record.CapturedAt.UTC()).Hours(),
	}, nil
}

func wantedPathHashes(lines []PendingLine) map[string]bool {
	wanted := make(map[string]bool, len(lines))
	for _, line := range lines {
		if line.PathHash != "" {
			wanted[line.PathHash] = true
		}
	}
	return wanted
}

type lineHashIndex map[string]map[string]bool

func (idx lineHashIndex) add(pathHash, lineHash string) {
	if pathHash == "" || lineHash == "" {
		return
	}
	lines := idx[pathHash]
	if lines == nil {
		lines = map[string]bool{}
		idx[pathHash] = lines
	}
	lines[lineHash] = true
}

func (idx lineHashIndex) contains(pathHash, lineHash string) bool {
	if idx == nil {
		return false
	}
	return idx[pathHash][lineHash]
}

func workingTreeLineIndex(root string, h hashing.Hasher, wanted map[string]bool) (lineHashIndex, error) {
	out, err := runGit(root, "-c", "core.quotePath=false", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}

	idx := lineHashIndex{}
	for _, rel := range splitNUL(out) {
		pathHash := h.StringHash(rel)
		if !wanted[pathHash] {
			continue
		}
		content, err := readRepoFile(root, rel)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := addContentLineHashes(idx, pathHash, content, h); err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
	}
	return idx, nil
}

func headLineIndex(root string, h hashing.Hasher, wanted map[string]bool) (lineHashIndex, error) {
	if _, err := runGit(root, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return lineHashIndex{}, nil
	}
	out, err := runGit(root, "-c", "core.quotePath=false", "ls-tree", "-r", "-z", "--name-only", "HEAD")
	if err != nil {
		return nil, err
	}

	idx := lineHashIndex{}
	for _, rel := range splitNUL(out) {
		pathHash := h.StringHash(rel)
		if !wanted[pathHash] {
			continue
		}
		content, err := runGit(root, "show", "HEAD:"+rel)
		if err != nil {
			return nil, err
		}
		if err := addContentLineHashes(idx, pathHash, content, h); err != nil {
			return nil, fmt.Errorf("HEAD:%s: %w", rel, err)
		}
	}
	return idx, nil
}

func addContentLineHashes(idx lineHashIndex, pathHash string, content []byte, h hashing.Hasher) error {
	if isBinary(content) {
		return nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		normalized := strings.TrimSpace(scanner.Text())
		if normalized == "" {
			continue
		}
		idx.add(pathHash, h.StringHash(normalized))
	}
	return scanner.Err()
}

func appendDatapoint(cfg Config, datapoint ResolvedDatapoint) error {
	data, err := json.Marshal(datapoint)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	path := dataLogPath(cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	return file.Close()
}

func dataLogPath(cfg Config) string {
	return filepath.Join(proofsweStateDir(cfg), "data.jsonl")
}

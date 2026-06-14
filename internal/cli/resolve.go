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
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/hashing"
)

const (
	dataSchemaVersion      = 1
	resolvedEventType      = "session_resolved"
	defaultResolveMaturity = 24 * time.Hour
	hookResolveLimit       = 8
)

var resolvingClaimCounter atomic.Uint64

// ResolvedDatapoint is the append-only data.jsonl record emitted when a pending
// snapshot matures. It intentionally contains no raw paths or code content.
type ResolvedDatapoint struct {
	SchemaVersion  int       `json:"schema_version"`
	EventType      string    `json:"event_type"`
	Timestamp      time.Time `json:"ts"`
	SessionHash    string    `json:"session_hash,omitempty"`
	Model          string    `json:"model,omitempty"`
	Harness        string    `json:"harness"`
	RepoHash       string    `json:"repo_hash"`
	Turns          int       `json:"turns"`
	ToolCalls      int       `json:"tool_calls"`
	LinesAdded     int       `json:"lines_added"`
	LinesSurvived  int       `json:"lines_survived"`
	LinesCommitted int       `json:"lines_committed"`
	Keeprate       float64   `json:"keeprate"`
	Committed      bool      `json:"committed"`
	ResolvedAfterH float64   `json:"resolved_after_h"`
}

type resolveOptions struct {
	Maturity   time.Duration
	Now        func() time.Time
	MaxRecords int
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
	processed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if opts.MaxRecords > 0 && processed >= opts.MaxRecords {
			break
		}
		path := filepath.Join(pendingDir, entry.Name())
		if err := resolvePendingFile(cfg, h, path, now, opts.Maturity); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Name(), err))
		}
		processed++
	}
	return errors.Join(errs...)
}

func resolvePendingFile(cfg Config, h hashing.Hasher, path string, now time.Time, maturity time.Duration) error {
	claimedPath, claimed, err := claimPendingFile(path, now)
	if err != nil {
		return fmt.Errorf("claim pending record: %w", err)
	}
	if !claimed {
		return nil
	}

	record, err := readPendingRecordFile(claimedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return quarantineClaimedPending(cfg, claimedPath, fmt.Errorf("decode pending record: %w", err))
	}
	if record.SchemaVersion != pendingSchemaVersion {
		return quarantineClaimedPending(cfg, claimedPath, fmt.Errorf("unsupported pending schema_version %d", record.SchemaVersion))
	}
	if record.CapturedAt.IsZero() {
		return quarantineClaimedPending(cfg, claimedPath, fmt.Errorf("pending record missing captured_at"))
	}
	if now.Sub(record.CapturedAt.UTC()) < maturity {
		if err := os.Rename(claimedPath, path); err != nil {
			return fmt.Errorf("restore immature pending record: %w", err)
		}
		return nil
	}

	datapoint, err := resolveRecord(h, record, now)
	if err != nil {
		return quarantineClaimedPending(cfg, claimedPath, err)
	}
	if err := appendDatapoint(cfg, datapoint); err != nil {
		if restoreErr := os.Rename(claimedPath, path); restoreErr != nil {
			return errors.Join(fmt.Errorf("append datapoint: %w", err), fmt.Errorf("restore pending record: %w", restoreErr))
		}
		return fmt.Errorf("append datapoint: %w", err)
	}
	if err := os.Remove(claimedPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("remove pending record: %w", err)
	}
	return nil
}

func claimPendingFile(path string, now time.Time) (string, bool, error) {
	claimed := path + ".resolving-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(now.UnixNano(), 10) + "-" + strconv.FormatUint(resolvingClaimCounter.Add(1), 10)
	if err := os.Rename(path, claimed); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return claimed, true, nil
}

func quarantineClaimedPending(cfg Config, claimedPath string, cause error) error {
	dir := filepath.Join(proofsweStateDir(cfg), "quarantine")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.Join(cause, fmt.Errorf("create quarantine dir: %w", err))
	}
	dst := filepath.Join(dir, filepath.Base(claimedPath)+".failed")
	if err := os.Rename(claimedPath, dst); err != nil {
		return errors.Join(cause, fmt.Errorf("quarantine pending record: %w", err))
	}
	return cause
}

func readPendingRecordFile(path string) (PendingRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PendingRecord{}, err
	}
	var record PendingRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return PendingRecord{}, err
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

	survival := unionLineIndexes(working, head)
	committedLines := head.clone()
	linesSurvived := 0
	linesCommitted := 0
	for _, line := range record.Lines {
		if survival.consume(line.PathHash, line.LineHash) {
			linesSurvived++
		}
		if committedLines.consume(line.PathHash, line.LineHash) {
			linesCommitted++
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
		SessionHash:    h.StringHash(record.SessionID),
		Model:          record.Model,
		Harness:        record.Harness,
		RepoHash:       h.StringHash(record.RepoPath),
		Turns:          record.TurnCount,
		ToolCalls:      record.ToolCallCount,
		LinesAdded:     linesAdded,
		LinesSurvived:  linesSurvived,
		LinesCommitted: linesCommitted,
		Keeprate:       keeprate,
		Committed:      linesCommitted > 0,
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

type lineHashIndex map[string]map[string]int

func (idx lineHashIndex) add(pathHash, lineHash string) {
	if pathHash == "" || lineHash == "" {
		return
	}
	lines := idx[pathHash]
	if lines == nil {
		lines = map[string]int{}
		idx[pathHash] = lines
	}
	lines[lineHash]++
}

func (idx lineHashIndex) consume(pathHash, lineHash string) bool {
	if idx == nil {
		return false
	}
	if idx[pathHash][lineHash] <= 0 {
		return false
	}
	idx[pathHash][lineHash]--
	return true
}

func (idx lineHashIndex) clone() lineHashIndex {
	out := make(lineHashIndex, len(idx))
	for pathHash, lines := range idx {
		out[pathHash] = make(map[string]int, len(lines))
		for lineHash, count := range lines {
			out[pathHash][lineHash] = count
		}
	}
	return out
}

func unionLineIndexes(a, b lineHashIndex) lineHashIndex {
	out := a.clone()
	for pathHash, lines := range b {
		outLines := out[pathHash]
		if outLines == nil {
			outLines = map[string]int{}
			out[pathHash] = outLines
		}
		for lineHash, count := range lines {
			if count > outLines[lineHash] {
				outLines[lineHash] = count
			}
		}
	}
	return out
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

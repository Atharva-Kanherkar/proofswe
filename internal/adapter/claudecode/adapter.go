package claudecode

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/reader"
)

const Harness core.HarnessName = "claudecode"

type Adapter struct {
	Root   string
	Logger *slog.Logger
}

type Transcript struct {
	Path        string
	ProjectSlug string
	RepoPath    string
	SessionID   core.SessionId
}

func New(root string) Adapter {
	return Adapter{Root: root}
}

func (a Adapter) root() string {
	if a.Root != "" {
		return a.Root
	}
	return DefaultRoot()
}

func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func (a Adapter) Detect() error {
	root := a.root()
	if root == "" {
		return core.NewError(core.ErrorKindAdapter, "claude code root unavailable", nil)
	}
	info, err := os.Stat(filepath.Join(root, "projects"))
	if err != nil {
		return core.NewError(core.ErrorKindAdapter, "detect claude code projects", err)
	}
	if !info.IsDir() {
		return core.NewError(core.ErrorKindAdapter, "claude code projects path is not a directory", nil)
	}
	return nil
}

func (a Adapter) Enable() error {
	return nil
}

func (a Adapter) Disable() error {
	return nil
}

func (a Adapter) Capture(core.CaptureTrigger) iter.Seq[core.NormalizedEvent] {
	return func(yield func(core.NormalizedEvent) bool) {
		transcripts, err := Discover(a.root())
		if err != nil {
			if a.Logger != nil {
				a.Logger.Warn("discover claude code transcripts", "error", err)
			}
			return
		}

		for _, transcript := range transcripts {
			if !captureTranscript(transcript, a.Logger, yield) {
				return
			}
		}
	}
}

func Discover(root string) ([]Transcript, error) {
	if root == "" {
		root = DefaultRoot()
	}
	if root == "" {
		return nil, core.NewError(core.ErrorKindAdapter, "claude code root unavailable", nil)
	}

	projectsDir := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, core.NewError(core.ErrorKindAdapter, "read claude code projects", err)
	}

	var transcripts []Transcript
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectSlug := entry.Name()
		projectDir := filepath.Join(projectsDir, projectSlug)
		files, err := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
		if err != nil {
			return nil, core.NewError(core.ErrorKindAdapter, "glob claude code transcripts", err)
		}
		sort.Strings(files)
		for _, path := range files {
			sessionID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			transcripts = append(transcripts, Transcript{
				Path:        path,
				ProjectSlug: projectSlug,
				RepoPath:    transcriptRepoPath(path, projectSlug),
				SessionID:   core.SessionId(sessionID),
			})
		}
	}

	sort.Slice(transcripts, func(i, j int) bool {
		return transcripts[i].Path < transcripts[j].Path
	})
	return transcripts, nil
}

func ProjectSlugToRepoPath(slug string) string {
	if slug == "" {
		return ""
	}
	trimmed := strings.TrimPrefix(slug, "-")
	if trimmed == "" {
		return string(filepath.Separator)
	}
	return string(filepath.Separator) + filepath.FromSlash(strings.ReplaceAll(trimmed, "-", "/"))
}

func transcriptRepoPath(path string, projectSlug string) string {
	file, err := os.Open(path)
	if err != nil {
		return ProjectSlugToRepoPath(projectSlug)
	}
	defer func() {
		_ = file.Close()
	}()

	line, err := bufio.NewReader(file).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return ProjectSlugToRepoPath(projectSlug)
	}
	var probe struct {
		CWD string `json:"cwd"`
	}
	if err := json.Unmarshal(line, &probe); err != nil || probe.CWD == "" {
		return ProjectSlugToRepoPath(projectSlug)
	}
	return filepath.Clean(probe.CWD)
}

func ParseFile(path string) ([]core.NormalizedEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, core.NewError(core.ErrorKindAdapter, "open claude code transcript", err)
	}

	var events []core.NormalizedEvent
	turnIndex := 0
	_, err = reader.ReadNewLines(file, 0, reader.Options{}, func(line []byte, _ int64) error {
		normalized, err := ParseRaw(line, path, turnIndex)
		if err != nil {
			return err
		}
		events = append(events, normalized...)
		turnIndex++
		return nil
	})
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, core.NewError(core.ErrorKindAdapter, "close claude code transcript", err)
	}
	return events, nil
}

func ParseRaw(data []byte, path string, turnIndex int) ([]core.NormalizedEvent, error) {
	var raw RawEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, core.NewError(core.ErrorKindInvalidEvent, "decode claude code record", err)
	}
	return raw.Normalized(path, turnIndex)
}

func captureTranscript(transcript Transcript, logger *slog.Logger, yield func(core.NormalizedEvent) bool) bool {
	file, err := os.Open(transcript.Path)
	if err != nil {
		if logger != nil {
			logger.Warn("open claude code transcript", "path", transcript.Path, "error", err)
		}
		return true
	}
	defer func() {
		if err := file.Close(); err != nil && logger != nil {
			logger.Warn("close claude code transcript", "path", transcript.Path, "error", err)
		}
	}()

	turnIndex := 0
	_, err = reader.ReadNewLines(file, 0, reader.Options{Logger: logger}, func(line []byte, _ int64) error {
		events, err := ParseRaw(line, transcript.Path, turnIndex)
		if err != nil {
			return err
		}
		turnIndex++
		for _, event := range events {
			if !yield(event) {
				return errStopCapture
			}
		}
		return nil
	})
	if err != nil && logger != nil && !errors.Is(err, errStopCapture) {
		logger.Warn("read claude code transcript", "path", transcript.Path, "error", err)
	}
	return !errors.Is(err, errStopCapture)
}

var errStopCapture = fmt.Errorf("stop claude code capture")

func envelope(common rawCommon, eventType core.EventType, path string, turnIndex int) core.Envelope {
	ts, _ := time.Parse(time.RFC3339Nano, common.Timestamp)
	sessionID := common.SessionID
	if sessionID == "" {
		sessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	return core.Envelope{
		SchemaVersion: core.SchemaVersion,
		Type:          eventType,
		Source: core.SourceMeta{
			Harness:   Harness,
			Path:      strings.ReplaceAll(filepath.ToSlash(path), `\`, "/"),
			GitBranch: common.GitBranch,
		},
		Session: core.SessionMeta{
			ID:  core.SessionId(sessionID),
			CWD: common.CWD,
		},
		Event: core.EventMeta{
			ID:         common.UUID,
			Timestamp:  ts,
			TurnIndex:  turnIndex,
			IsSubagent: common.IsSidechain,
		},
	}
}

func hashRaw(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func hashString(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

package claudecode

import (
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
	Root     string
	StateDir string
	Logger   *slog.Logger
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

func (a Adapter) stateDir() string {
	if a.StateDir != "" {
		return a.StateDir
	}
	return DefaultStateDir()
}

func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func DefaultStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".proofswe")
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

// Enable / Disable are intentionally no-ops here: writing the sentinel-tagged
// SessionStart/Stop hooks into ~/.claude/settings.json (and the kill-switch that
// guards them) is owned by the cli hook layer, which sequences registration across
// every adapter. This adapter only discovers and parses transcripts.
func (a Adapter) Enable() error {
	return nil
}

func (a Adapter) Disable() error {
	return nil
}

func (a Adapter) Capture(core.CaptureTrigger) iter.Seq[core.NormalizedEvent] {
	return func(yield func(core.NormalizedEvent) bool) {
		// Fail closed: a missing salt must never downgrade us to unsalted hashes.
		salt, err := loadOrCreateSalt(a.stateDir())
		if err != nil {
			if a.Logger != nil {
				a.Logger.Warn("load proofswe hash salt", "error", err)
			}
			return
		}

		transcripts, err := Discover(a.root())
		if err != nil {
			if a.Logger != nil {
				a.Logger.Warn("discover claude code transcripts", "error", err)
			}
			return
		}

		for _, transcript := range transcripts {
			if !captureTranscript(transcript, salt, a.stateDir(), a.Logger, yield) {
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
		repoPath := ProjectSlugToRepoPath(projectSlug)

		// Main transcripts live at <slug>/<session>.jsonl, but subagent and
		// workflow transcripts are nested deeper under <session>/subagents/…;
		// walk the whole subtree so the bulk of sessions is not silently skipped.
		walkErr := filepath.WalkDir(projectDir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			sessionID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			transcripts = append(transcripts, Transcript{
				Path:        path,
				ProjectSlug: projectSlug,
				RepoPath:    repoPath,
				SessionID:   core.SessionId(sessionID),
			})
			return nil
		})
		if walkErr != nil {
			return nil, core.NewError(core.ErrorKindAdapter, "walk claude code transcripts", walkErr)
		}
	}

	sort.Slice(transcripts, func(i, j int) bool {
		return transcripts[i].Path < transcripts[j].Path
	})
	return transcripts, nil
}

// ProjectSlugToRepoPath best-effort decodes Claude Code's project slug back to a
// filesystem path. It is lossy (slugs replace every path separator with '-', so a
// repo whose own name contains '-' cannot be recovered exactly); the authoritative
// working directory is carried per-event in session.cwd.
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

// cursorPath maps a transcript path to its resume-cursor file under the state dir.
func cursorPath(stateDir, transcriptPath string) string {
	sum := sha256.Sum256([]byte(transcriptPath))
	return filepath.Join(stateDir, "cursors", hex.EncodeToString(sum[:])+".cursor")
}

func ParseFile(salt []byte, path string) ([]core.NormalizedEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, core.NewError(core.ErrorKindAdapter, "open claude code transcript", err)
	}

	var events []core.NormalizedEvent
	turnIndex := 0
	_, err = reader.ReadNewLines(file, 0, reader.Options{}, func(line []byte, _ int64) error {
		normalized, parseErr := ParseRaw(salt, line, path, turnIndex)
		turnIndex++
		if parseErr != nil {
			// Lenient, like the reader: skip a malformed record and keep going.
			return nil
		}
		events = append(events, normalized...)
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

func ParseRaw(salt []byte, data []byte, path string, turnIndex int) ([]core.NormalizedEvent, error) {
	var raw RawEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, core.NewError(core.ErrorKindInvalidEvent, "decode claude code record", err)
	}
	return raw.Normalized(newHasher(salt), path, turnIndex)
}

func captureTranscript(transcript Transcript, salt []byte, stateDir string, logger *slog.Logger, yield func(core.NormalizedEvent) bool) bool {
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

	cursorFile := cursorPath(stateDir, transcript.Path)
	cursor, err := reader.LoadCursor(cursorFile)
	if err != nil {
		if logger != nil {
			logger.Warn("load transcript cursor", "path", transcript.Path, "error", err)
		}
		cursor = 0
	}

	// turnIndex is the ordinal of a record within this capture pass over the new
	// bytes; events are keyed downstream by event.id (the record uuid), so the
	// index only needs to order events seen together, not survive across passes.
	turnIndex := 0
	stats, err := reader.ReadNewLines(file, cursor, reader.Options{Logger: logger}, func(line []byte, _ int64) error {
		events, parseErr := ParseRaw(salt, line, transcript.Path, turnIndex)
		turnIndex++
		if parseErr != nil {
			if logger != nil {
				logger.Warn("skip malformed claude code record", "path", transcript.Path, "error", parseErr)
			}
			return nil
		}
		for _, event := range events {
			if !yield(event) {
				return errStopCapture
			}
		}
		return nil
	})
	if errors.Is(err, errStopCapture) {
		// Consumer stopped early; do not advance the cursor so nothing is skipped.
		return false
	}
	if err != nil && logger != nil {
		logger.Warn("read claude code transcript", "path", transcript.Path, "error", err)
	}
	if saveErr := reader.SaveCursor(cursorFile, stats.Cursor); saveErr != nil && logger != nil {
		logger.Warn("save transcript cursor", "path", transcript.Path, "error", saveErr)
	}
	return true
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

package codex

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
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/reader"
)

const Harness core.HarnessName = "codex"

type Adapter struct {
	Root     string
	StateDir string
	Logger   *slog.Logger
}

type Transcript struct {
	Path      string
	SessionID core.SessionId
}

type IndexedSession struct {
	ID         core.SessionId
	ThreadName string
	UpdatedAt  time.Time
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
	return filepath.Join(home, ".codex")
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
		return core.NewError(core.ErrorKindAdapter, "codex root unavailable", nil)
	}
	info, err := os.Stat(filepath.Join(root, "sessions"))
	if err != nil {
		return core.NewError(core.ErrorKindAdapter, "detect codex sessions", err)
	}
	if !info.IsDir() {
		return core.NewError(core.ErrorKindAdapter, "codex sessions path is not a directory", nil)
	}
	return nil
}

// Enable / Disable are intentionally no-ops here: user-level ~/.codex/config.toml
// hook registration and the kill-switch belong to the cli hook layer.
func (a Adapter) Enable() error {
	return nil
}

func (a Adapter) Disable() error {
	return nil
}

func (a Adapter) Capture(core.CaptureTrigger) iter.Seq[core.NormalizedEvent] {
	return func(yield func(core.NormalizedEvent) bool) {
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
				a.Logger.Warn("discover codex rollouts", "error", err)
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
		return nil, core.NewError(core.ErrorKindAdapter, "codex root unavailable", nil)
	}

	sessionsDir := filepath.Join(root, "sessions")
	var transcripts []Transcript
	err := filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasPrefix(filepath.Base(path), "rollout-") || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		transcripts = append(transcripts, Transcript{
			Path:      path,
			SessionID: sessionIDFromRolloutPath(path),
		})
		return nil
	})
	if err != nil {
		return nil, core.NewError(core.ErrorKindAdapter, "walk codex rollouts", err)
	}

	sort.Slice(transcripts, func(i, j int) bool {
		return transcripts[i].Path < transcripts[j].Path
	})
	return transcripts, nil
}

func EnumerateSessionIndex(root string) ([]IndexedSession, error) {
	if root == "" {
		root = DefaultRoot()
	}
	if root == "" {
		return nil, core.NewError(core.ErrorKindAdapter, "codex root unavailable", nil)
	}

	file, err := os.Open(filepath.Join(root, "session_index.jsonl"))
	if err != nil {
		return nil, core.NewError(core.ErrorKindAdapter, "open codex session index", err)
	}
	defer func() {
		_ = file.Close()
	}()

	var sessions []IndexedSession
	_, err = reader.ReadNewLines(file, 0, reader.Options{}, func(line []byte, _ int64) error {
		var raw struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
			UpdatedAt  string `json:"updated_at"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil
		}
		updatedAt, _ := parseTimestamp(raw.UpdatedAt)
		sessions = append(sessions, IndexedSession{
			ID:         core.SessionId(raw.ID),
			ThreadName: raw.ThreadName,
			UpdatedAt:  updatedAt,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ID < sessions[j].ID
	})
	return sessions, nil
}

func cursorPath(stateDir, transcriptPath string) string {
	sum := sha256.Sum256([]byte(transcriptPath))
	return filepath.Join(stateDir, "cursors", hex.EncodeToString(sum[:])+".cursor")
}

func ParseFile(salt []byte, path string) ([]core.NormalizedEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, core.NewError(core.ErrorKindAdapter, "open codex rollout", err)
	}

	parser := newParser(salt, path)
	var events []core.NormalizedEvent
	_, err = reader.ReadNewLines(file, 0, reader.Options{}, func(line []byte, _ int64) error {
		normalized, parseErr := parser.Parse(line)
		if parseErr != nil {
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
		return nil, core.NewError(core.ErrorKindAdapter, "close codex rollout", err)
	}
	return events, nil
}

func ParseRaw(salt []byte, data []byte, path string, turnIndex int) ([]core.NormalizedEvent, error) {
	parser := newParser(salt, path)
	parser.state.turnIndex = turnIndex
	return parser.Parse(data)
}

func captureTranscript(transcript Transcript, salt []byte, stateDir string, logger *slog.Logger, yield func(core.NormalizedEvent) bool) bool {
	file, err := os.Open(transcript.Path)
	if err != nil {
		if logger != nil {
			logger.Warn("open codex rollout", "path", transcript.Path, "error", err)
		}
		return true
	}
	defer func() {
		if err := file.Close(); err != nil && logger != nil {
			logger.Warn("close codex rollout", "path", transcript.Path, "error", err)
		}
	}()

	cursorFile := cursorPath(stateDir, transcript.Path)
	cursor, err := reader.LoadCursor(cursorFile)
	if err != nil {
		if logger != nil {
			logger.Warn("load rollout cursor", "path", transcript.Path, "error", err)
		}
		cursor = 0
	}

	parser := newParser(salt, transcript.Path)
	stats, err := reader.ReadNewLines(file, cursor, reader.Options{Logger: logger}, func(line []byte, _ int64) error {
		events, parseErr := parser.Parse(line)
		if parseErr != nil {
			if logger != nil {
				logger.Warn("skip malformed codex record", "path", transcript.Path, "error", parseErr)
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
		return false
	}
	if err != nil && logger != nil {
		logger.Warn("read codex rollout", "path", transcript.Path, "error", err)
	}
	if saveErr := reader.SaveCursor(cursorFile, stats.Cursor); saveErr != nil && logger != nil {
		logger.Warn("save rollout cursor", "path", transcript.Path, "error", saveErr)
	}
	return true
}

var errStopCapture = fmt.Errorf("stop codex capture")

var rolloutNamePattern = regexp.MustCompile(`^rollout-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-(.+)$`)

func sessionIDFromRolloutPath(path string) core.SessionId {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	matches := rolloutNamePattern.FindStringSubmatch(base)
	if len(matches) == 2 {
		return core.SessionId(matches[1])
	}
	return core.SessionId(base)
}

func normalizePath(path string) string {
	return strings.ReplaceAll(filepath.ToSlash(path), `\`, "/")
}

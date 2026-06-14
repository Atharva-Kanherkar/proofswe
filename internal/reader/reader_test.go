package reader

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
)

func TestReadNewYieldsFixtureRecords(t *testing.T) {
	path := writeJSONL(
		t,
		eventLine(core.EventTypeSessionStart, "a"),
		eventLine(core.EventTypeUserPrompt, "b"),
		eventLine(core.EventTypeSessionEnd, "c"),
	)

	result := readPath(t, path, 0, Options{})

	if len(result.Events) != 3 {
		t.Fatalf("len(Events) = %d, want 3", len(result.Events))
	}
	if result.Stats.Emitted != 3 {
		t.Fatalf("Emitted = %d, want 3", result.Stats.Emitted)
	}
	wantTypes := []core.EventType{core.EventTypeSessionStart, core.EventTypeUserPrompt, core.EventTypeSessionEnd}
	for i, want := range wantTypes {
		if got := result.Events[i].EventType(); got != want {
			t.Fatalf("Events[%d].EventType() = %q, want %q", i, got, want)
		}
	}
	if result.Stats.Cursor != fileSize(t, path) {
		t.Fatalf("Cursor = %d, want file size %d", result.Stats.Cursor, fileSize(t, path))
	}
}

func TestReadNewResumesFromPersistedCursor(t *testing.T) {
	path := writeJSONL(
		t,
		eventLine(core.EventTypeSessionStart, "a"),
		eventLine(core.EventTypeUserPrompt, "b"),
	)
	first := readPath(t, path, 0, Options{})
	if len(first.Events) != 2 {
		t.Fatalf("first len(Events) = %d, want 2", len(first.Events))
	}

	appendJSONL(
		t, path,
		eventLine(core.EventTypeToolCall, "c"),
		eventLine(core.EventTypeSessionEnd, "d"),
	)
	second := readPath(t, path, first.Stats.Cursor, Options{})

	if len(second.Events) != 2 {
		t.Fatalf("second len(Events) = %d, want 2", len(second.Events))
	}
	if second.Events[0].EventType() != core.EventTypeToolCall {
		t.Fatalf("second first event = %q, want %q", second.Events[0].EventType(), core.EventTypeToolCall)
	}
	if second.Stats.Cursor != fileSize(t, path) {
		t.Fatalf("second Cursor = %d, want file size %d", second.Stats.Cursor, fileSize(t, path))
	}
}

func TestReadNewLeavesCursorBeforePartialLine(t *testing.T) {
	complete := eventLine(core.EventTypeSessionStart, "a") + "\n"
	partialStart := `{"schema_version":1,"type":"user_prompt","source":{"harness":"codex"}`
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte(complete+partialStart), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	first := readPath(t, path, 0, Options{})
	if len(first.Events) != 1 {
		t.Fatalf("first len(Events) = %d, want 1", len(first.Events))
	}
	if first.Stats.Cursor != int64(len(complete)) {
		t.Fatalf("first Cursor = %d, want %d", first.Stats.Cursor, len(complete))
	}

	appendRaw(t, path, []byte(`,"session":{"id":"b"}}`+"\n"))
	second := readPath(t, path, first.Stats.Cursor, Options{})
	if len(second.Events) != 1 {
		t.Fatalf("second len(Events) = %d, want 1", len(second.Events))
	}
	if second.Events[0].EventType() != core.EventTypeUserPrompt {
		t.Fatalf("second event type = %q, want %q", second.Events[0].EventType(), core.EventTypeUserPrompt)
	}
	if second.Stats.Cursor != fileSize(t, path) {
		t.Fatalf("second Cursor = %d, want file size %d", second.Stats.Cursor, fileSize(t, path))
	}
}

func TestReadNewSkipsMalformedLineAndLogs(t *testing.T) {
	path := writeJSONL(
		t,
		eventLine(core.EventTypeSessionStart, "a"),
		`{"schema_version":1,"type":`,
		eventLine(core.EventTypeSessionEnd, "b"),
	)
	var logOut bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOut, &slog.HandlerOptions{}))

	result := readPath(t, path, 0, Options{Logger: logger})

	if len(result.Events) != 2 {
		t.Fatalf("len(Events) = %d, want 2", len(result.Events))
	}
	if result.Stats.Malformed != 1 {
		t.Fatalf("Malformed = %d, want 1", result.Stats.Malformed)
	}
	if !strings.Contains(logOut.String(), "skip malformed jsonl line") {
		t.Fatalf("log output missing malformed line message: %s", logOut.String())
	}
	if result.Stats.Cursor != fileSize(t, path) {
		t.Fatalf("Cursor = %d, want file size %d", result.Stats.Cursor, fileSize(t, path))
	}
}

func TestReadNewSkipsBlankLinesSilently(t *testing.T) {
	path := writeJSONL(
		t,
		eventLine(core.EventTypeSessionStart, "a"),
		"",
		"   ",
		eventLine(core.EventTypeSessionEnd, "b"),
	)
	var logOut bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOut, nil))

	result := readPath(t, path, 0, Options{Logger: logger})

	if len(result.Events) != 2 {
		t.Fatalf("len(Events) = %d, want 2", len(result.Events))
	}
	if result.Stats.Blank != 2 {
		t.Fatalf("Blank = %d, want 2", result.Stats.Blank)
	}
	if result.Stats.Malformed != 0 {
		t.Fatalf("Malformed = %d, want 0", result.Stats.Malformed)
	}
	if logOut.Len() != 0 {
		t.Fatalf("log output = %q, want empty", logOut.String())
	}
}

func TestCursorPersistenceRoundTripsAndMissingIsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor", "events.cursor")

	got, err := LoadCursor(path)
	if err != nil {
		t.Fatalf("LoadCursor(missing) error = %v", err)
	}
	if got != 0 {
		t.Fatalf("LoadCursor(missing) = %d, want 0", got)
	}

	if err := SaveCursor(path, 12345); err != nil {
		t.Fatalf("SaveCursor() error = %v", err)
	}
	got, err = LoadCursor(path)
	if err != nil {
		t.Fatalf("LoadCursor() error = %v", err)
	}
	if got != 12345 {
		t.Fatalf("LoadCursor() = %d, want 12345", got)
	}
}

func TestReadNewMaxLineGuardSkipsOversizedCompleteLine(t *testing.T) {
	path := writeJSONL(
		t,
		eventLine(core.EventTypeSessionStart, "a"),
		`{"schema_version":1,"type":"user_prompt","payload":"`+strings.Repeat("x", 128)+`"}`,
		eventLine(core.EventTypeSessionEnd, "b"),
	)
	var logOut bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOut, nil))

	result := readPath(t, path, 0, Options{MaxLineBytes: 120, Logger: logger})

	if len(result.Events) != 2 {
		t.Fatalf("len(Events) = %d, want 2", len(result.Events))
	}
	if result.Stats.Oversized != 1 {
		t.Fatalf("Oversized = %d, want 1", result.Stats.Oversized)
	}
	if !strings.Contains(logOut.String(), "skip oversized jsonl line") {
		t.Fatalf("log output missing oversized line message: %s", logOut.String())
	}
	if result.Stats.Cursor != fileSize(t, path) {
		t.Fatalf("Cursor = %d, want file size %d", result.Stats.Cursor, fileSize(t, path))
	}
}

func TestReadNewOversizedPartialDoesNotAdvanceCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	data := eventLine(core.EventTypeSessionStart, "a") + "\n" + strings.Repeat("x", 128)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result := readPath(t, path, 0, Options{MaxLineBytes: 120})

	wantCursor := int64(len(eventLine(core.EventTypeSessionStart, "a")) + 1)
	if result.Stats.Cursor != wantCursor {
		t.Fatalf("Cursor = %d, want %d", result.Stats.Cursor, wantCursor)
	}
	if len(result.Events) != 1 {
		t.Fatalf("len(Events) = %d, want 1", len(result.Events))
	}
}

func TestReadNewEmitErrorDoesNotAdvancePastEvent(t *testing.T) {
	path := writeJSONL(
		t,
		eventLine(core.EventTypeSessionStart, "a"),
		eventLine(core.EventTypeSessionEnd, "b"),
	)
	emitErr := errors.New("sink unavailable")

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	stats, err := ReadNew(file, 0, Options{}, func(core.NormalizedEvent) error {
		return emitErr
	})
	if !errors.Is(err, emitErr) {
		t.Fatalf("ReadNew() error = %v, want %v", err, emitErr)
	}
	if stats.Cursor != 0 {
		t.Fatalf("Cursor = %d, want 0", stats.Cursor)
	}
	if stats.Emitted != 0 {
		t.Fatalf("Emitted = %d, want 0", stats.Emitted)
	}
}

type readResult struct {
	Stats  Stats
	Events []core.NormalizedEvent
}

func readPath(t *testing.T, path string, cursor int64, opts Options) readResult {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	var events []core.NormalizedEvent
	stats, err := ReadNew(file, cursor, opts, func(event core.NormalizedEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}
	return readResult{Stats: stats, Events: events}
}

func writeJSONL(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	appendJSONL(t, path, lines...)
	return path
}

func appendJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		appendRaw(t, path, []byte(line+"\n"))
	}
}

func appendRaw(t *testing.T, path string, data []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		t.Fatalf("Write() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	return info.Size()
}

func eventLine(eventType core.EventType, sessionID string) string {
	return fmt.Sprintf(
		`{"schema_version":1,"type":%q,"source":{"harness":"codex"},"session":{"id":%q}}`,
		eventType,
		sessionID,
	)
}

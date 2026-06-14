//go:build !windows

package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
)

func TestCaptureConstantMemoryLargeRollout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi-hundred-MB Codex rollout test in short mode")
	}
	if os.Getenv("PROOFSWE_RUN_LONG_CODEX_TEST") != "1" {
		t.Skip("set PROOFSWE_RUN_LONG_CODEX_TEST=1 to synthesize a multi-hundred-MB rollout")
	}

	root := t.TempDir()
	state := t.TempDir()
	path := filepath.Join(root, "sessions", "2026", "06", "01", "rollout-2026-06-01T00-00-00-large-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	const targetBytes int64 = 256 << 20
	line := paddedUserMessageLine(1024)
	writeRepeatedLine(t, path, line, targetBytes)

	adapter := Adapter{Root: root, StateDir: state}
	var emitted int
	for event := range adapter.Capture(core.CaptureTriggerStop) {
		if event.EventType() != core.EventTypeUserPrompt {
			t.Fatalf("event type = %q, want %q", event.EventType(), core.EventTypeUserPrompt)
		}
		emitted++
	}
	if emitted == 0 {
		t.Fatal("emitted = 0, want events")
	}

	runtime.GC()
	maxRSSMB := maxRSSMegabytes(t)
	if maxRSSMB > 75 {
		t.Fatalf("max RSS = %d MB, want <= 75 MB", maxRSSMB)
	}
}

func paddedUserMessageLine(size int) string {
	base := `{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"redacted fixture line"}]}}`
	padding := size - len(base) - len(`,"padding":""`)
	if padding < 0 {
		padding = 0
	}
	return strings.TrimSuffix(base, "}") + fmt.Sprintf(`,"padding":%q}`, strings.Repeat("x", padding))
}

func writeRepeatedLine(t *testing.T, path string, line string, targetBytes int64) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	record := []byte(line + "\n")
	var written int64
	for written < targetBytes {
		n, err := file.Write(record)
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		written += int64(n)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func maxRSSMegabytes(t *testing.T) int64 {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatalf("Getrusage() error = %v", err)
	}
	return usage.Maxrss / 1024
}

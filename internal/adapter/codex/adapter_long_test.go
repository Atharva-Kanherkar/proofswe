package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
)

// TestCaptureConstantMemoryLargeRollout guards Invariant 4: capture memory must
// stay constant regardless of transcript size. It synthesizes a rollout whose
// single turn never closes (no task boundary, so nothing can be flushed early)
// and asserts peak Go heap stays far below the file size. Skipped under -short.
func TestCaptureConstantMemoryLargeRollout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-rollout streaming test in short mode")
	}

	const fileBytes int64 = 96 << 20 // 96 MB on one open turn
	const heapBudgetMB = 32          // O(1) capture stays tiny; buffering the turn would blow past this

	root := t.TempDir()
	state := t.TempDir()
	path := filepath.Join(root, "sessions", "2026", "06", "01", "rollout-2026-06-01T00-00-00-large-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeRepeatedLine(t, path, paddedUserMessageLine(1024), fileBytes)

	adapter := Adapter{Root: root, StateDir: state}
	var emitted int
	var peakHeap uint64
	var ms runtime.MemStats
	for event := range adapter.Capture(core.CaptureTriggerStop) {
		if event.EventType() != core.EventTypeUserPrompt {
			t.Fatalf("event type = %q, want %q", event.EventType(), core.EventTypeUserPrompt)
		}
		emitted++
		if emitted%20000 == 0 {
			runtime.ReadMemStats(&ms)
			if ms.HeapInuse > peakHeap {
				peakHeap = ms.HeapInuse
			}
		}
	}
	if emitted == 0 {
		t.Fatal("emitted = 0, want events")
	}

	runtime.ReadMemStats(&ms)
	if ms.HeapInuse > peakHeap {
		peakHeap = ms.HeapInuse
	}
	peakMB := peakHeap >> 20
	if peakMB > heapBudgetMB {
		t.Fatalf("peak heap = %d MB on a %d MB rollout, want <= %d MB (capture must not buffer the transcript)",
			peakMB, fileBytes>>20, heapBudgetMB)
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

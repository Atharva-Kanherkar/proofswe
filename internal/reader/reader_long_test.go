//go:build !windows

package reader

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

func TestReadNewConstantMemoryLargeJSONL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping >=1GB reader test in short mode")
	}
	if os.Getenv("PROOFSWE_RUN_LONG_READER_TEST") != "1" {
		t.Skip("set PROOFSWE_RUN_LONG_READER_TEST=1 to synthesize >=1GB JSONL")
	}

	path := filepath.Join(t.TempDir(), "large.jsonl")
	const targetBytes int64 = 1 << 30
	line := eventLineWithPadding(256)
	writeRepeatedLine(t, path, line, targetBytes)

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	var emitted int
	stats, err := ReadNew(file, 0, Options{MaxLineBytes: int64(len(line) + 1)}, func(core.NormalizedEvent) error {
		emitted++
		return nil
	})
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}
	if stats.Cursor < targetBytes {
		t.Fatalf("Cursor = %d, want at least %d", stats.Cursor, targetBytes)
	}
	if stats.Emitted != emitted {
		t.Fatalf("Emitted = %d, callback count = %d", stats.Emitted, emitted)
	}
	if emitted == 0 {
		t.Fatal("callback count = 0, want emitted events")
	}

	runtime.GC()
	maxRSSMB := maxRSSMegabytes(t)
	if maxRSSMB > 50 {
		t.Fatalf("max RSS = %d MB, want <= 50 MB", maxRSSMB)
	}
}

func eventLineWithPadding(size int) string {
	base := eventLine("session_start", "large")
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

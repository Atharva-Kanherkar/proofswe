package reader

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
)

const (
	DefaultMaxLineBytes = 16 << 20
	readerBufferBytes   = 64 << 10
)

var ErrLineTooLong = errors.New("jsonl line exceeds max bytes")

type Options struct {
	MaxLineBytes int64
	Logger       *slog.Logger
}

type Stats struct {
	Cursor    int64
	Emitted   int
	Malformed int
	Oversized int
	Blank     int
}

func ReadNew(file *os.File, cursor int64, opts Options, emit func(core.NormalizedEvent) error) (Stats, error) {
	if emit == nil {
		return Stats{}, fmt.Errorf("reader: nil emit callback")
	}

	var emitted int
	var malformed int
	stats, err := ReadNewLines(file, cursor, opts, func(line []byte, offset int64) error {
		event, err := ParseLine(line)
		if err != nil {
			malformed++
			logReaderError(opts.Logger, "skip malformed jsonl line", err, "offset", offset)
			return nil
		}
		if err := emit(event); err != nil {
			return err
		}
		emitted++
		return nil
	})
	stats.Emitted = emitted
	stats.Malformed = malformed
	return stats, err
}

func ReadNewLines(file *os.File, cursor int64, opts Options, emit func(line []byte, offset int64) error) (Stats, error) {
	if file == nil {
		return Stats{}, fmt.Errorf("reader: nil file")
	}
	if cursor < 0 {
		return Stats{}, fmt.Errorf("reader: negative cursor %d", cursor)
	}
	if emit == nil {
		return Stats{}, fmt.Errorf("reader: nil emit callback")
	}

	if _, err := file.Seek(cursor, io.SeekStart); err != nil {
		return Stats{}, fmt.Errorf("reader: seek cursor %d: %w", cursor, err)
	}

	maxLineBytes := opts.MaxLineBytes
	if maxLineBytes <= 0 {
		maxLineBytes = DefaultMaxLineBytes
	}

	stats := Stats{Cursor: cursor}
	buf := bufio.NewReaderSize(file, readerBufferBytes)
	for {
		line, n, complete, oversized, err := readCompleteLine(buf, maxLineBytes)
		if err != nil {
			return stats, err
		}
		if !complete {
			return stats, nil
		}

		lineOffset := stats.Cursor
		nextCursor := stats.Cursor + n
		if oversized {
			stats.Cursor = nextCursor
			stats.Oversized++
			logReaderError(opts.Logger, "skip oversized jsonl line", ErrLineTooLong, "offset", lineOffset, "bytes", n)
			continue
		}
		if isBlankLine(line) {
			stats.Cursor = nextCursor
			stats.Blank++
			continue
		}

		if err := emit(line, lineOffset); err != nil {
			return stats, fmt.Errorf("reader: emit line at offset %d: %w", lineOffset, err)
		}
		stats.Cursor = nextCursor
		stats.Emitted++
	}
}

func ParseLine(line []byte) (core.NormalizedEvent, error) {
	line = trimLineEnding(line)
	if len(line) == 0 {
		return nil, core.NewError(core.ErrorKindInvalidEvent, "empty jsonl line", nil)
	}
	return decodeNormalizedEvent(line)
}

func LoadCursor(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("reader: read cursor: %w", err)
	}

	text := strings.TrimSpace(string(data))
	if text == "" {
		return 0, nil
	}
	offset, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("reader: parse cursor: %w", err)
	}
	if offset < 0 {
		return 0, fmt.Errorf("reader: negative persisted cursor %d", offset)
	}
	return offset, nil
}

func SaveCursor(path string, cursor int64) error {
	if cursor < 0 {
		return fmt.Errorf("reader: negative cursor %d", cursor)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("reader: create cursor dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".cursor-*")
	if err != nil {
		return fmt.Errorf("reader: create temp cursor: %w", err)
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := fmt.Fprintf(tmp, "%d\n", cursor); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("reader: write cursor: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("reader: sync cursor: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("reader: close cursor: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("reader: rename cursor: %w", err)
	}
	removeTmp = false

	if err := syncDir(dir); err != nil {
		return fmt.Errorf("reader: sync cursor dir: %w", err)
	}
	return nil
}

func readCompleteLine(r *bufio.Reader, maxLineBytes int64) ([]byte, int64, bool, bool, error) {
	var line []byte
	var total int64
	var oversized bool

	for {
		chunk, err := r.ReadSlice('\n')
		total += int64(len(chunk))
		if total > maxLineBytes {
			oversized = true
		}
		if !oversized {
			line = append(line, chunk...)
		}

		switch {
		case err == nil:
			if oversized {
				return nil, total, true, true, nil
			}
			return line, total, true, false, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			// Live harness transcripts commit records by newline. A final record
			// without '\n' is indistinguishable from an in-progress write, so leave
			// the cursor before it and retry on the next pass.
			return nil, total, false, oversized, nil
		default:
			return nil, total, false, oversized, fmt.Errorf("reader: read jsonl line: %w", err)
		}
	}
}

func trimLineEnding(line []byte) []byte {
	line = bytesTrimSuffix(line, '\n')
	return bytesTrimSuffix(line, '\r')
}

func isBlankLine(line []byte) bool {
	return len(bytes.TrimSpace(trimLineEnding(line))) == 0
}

func bytesTrimSuffix(data []byte, suffix byte) []byte {
	if len(data) > 0 && data[len(data)-1] == suffix {
		return data[:len(data)-1]
	}
	return data
}

func logReaderError(logger *slog.Logger, msg string, err error, attrs ...any) {
	if logger == nil {
		return
	}
	args := append([]any{"error", err}, attrs...)
	logger.Warn(msg, args...)
}

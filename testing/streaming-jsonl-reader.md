# Streaming JSONL Reader Contract

Task: issue #4, streaming JSONL reader with byte-offset resume cursor.

## Functional Expectations

- The reader processes newline-delimited normalized events from an `*os.File`
  using buffered reads, not `bufio.Scanner`.
- The reader starts from a supplied byte cursor and only yields records after
  that cursor.
- The reader streams each decoded event to a caller-provided emit callback and
  does not retain decoded events in a package-owned slice.
- The returned cursor is the byte offset after the last fully parsed complete
  line.
- A trailing partial line is not decoded and does not advance the cursor.
- A complete final line is defined by a trailing newline; a final record without
  a newline is treated as partial because live harness transcripts are assumed
  to newline-terminate committed records.
- Malformed complete lines are reported through a caller-provided logger and
  skipped without preventing later valid lines from being decoded.
- Blank complete lines are skipped silently.
- A configurable max-line guard rejects oversized complete lines without
  unbounded memory growth.
- Cursor persistence writes and reads a byte offset in a crash-safe way using a
  temporary file, fsync, and same-directory rename.
- The default JSON decoder path uses stdlib `encoding/json` through
  `core.UnmarshalNormalizedEvent`.
- An optional faster JSON decoder may exist only behind an off-by-default build
  tag; stdlib must remain the default path.
- The reader package must not import or call mmap APIs and must not use
  `bufio.Scanner`.

## Tests To Add Or Run

- Unit: fixture JSONL emits exactly the expected number and types of records
  without retaining decoded events in the reader result.
- Unit: resume reads an initial file, returns a cursor, appends more complete
  lines, and a second read yields exactly the appended records.
- Unit: partial trailing line yields only complete records, leaves the cursor
  before the partial line, and yields that completed record exactly once after
  appending the remainder.
- Unit: a malformed line logs an error and subsequent valid lines still parse.
- Unit: cursor persistence round-trips offsets and tolerates a missing cursor
  file as offset zero.
- Unit: max-line guard skips or errors on an oversized line without advancing
  past it as a valid parsed record.
- Fuzz: line parser never panics and round-trips valid normalized events.
- Integration/static: package grep finds no `bufio.Scanner` usage and no mmap
  API usage.
- CI: build/test the default reader path and the off-by-default
  `proofswe_fastjson` decoder path.
- Long test: synthesized >= 1 GB JSONL stays under a fixed RSS bound and is
  skipped under `testing.Short()` for routine test runs, but is exercised by a
  scheduled/manual CI workflow.

## Manual Verification

- Run `go test ./internal/reader`.
- Run `go test -tags proofswe_fastjson ./internal/reader`.
- Run `go test -short ./...`.
- Run static grep checks for `bufio.Scanner`, `NewScanner`, `mmap`, `Mmap`, and
  `syscall.Mmap`.
- If time and environment permit, run the non-short reader test that synthesizes
  at least 1 GB of JSONL.

# internal/reader — streaming JSONL + resume cursor

[↑ Root AGENTS.md](../../AGENTS.md)

**Single responsibility:** read a transcript JSONL file in **constant memory**,
yielding one raw line at a time, and persist/restore a **byte-offset resume
cursor** so each pass parses only new bytes.

**Dependency direction:** `reader → core` (for error types). Adapters depend on the
reader; the reader does not depend on adapters or the cli.

## Owns

- The streaming line reader over an open `*os.File`.
- The byte-offset cursor: persisted offset of the last **fully parsed** line;
  doubles as the rotation anchor.

## Invariants that bite here (this is the highest-risk package)

- **Use `bufio.Reader.ReadBytes('\n')`** (or `ReadString`). **NEVER `bufio.Scanner`** —
  its 64 KB `MaxScanTokenSize` cap aborts with `bufio.ErrTooLong` on a fat JSONL
  record (Codex rollouts reach 700 MB–2 GB). `ReadBytes` has no fixed line cap.
- **NEVER `mmap` the live file.** It is being appended/truncated while you read →
  **SIGBUS** (unrecoverable abort). Buffered reads only. CI greps for zero `mmap`.
- **Never advance the cursor past a trailing partial line** — a half-written final
  event surfaces as a short read / EOF; leave it to be re-read once complete.
- Reuse a single decode/scratch buffer across lines to amortize allocation.
- Cursor persistence is crash-safe: temp-file + `fsync` + `os.Rename` (same
  filesystem). `json.Decoder.InputOffset()` is the streaming-decoder equivalent if
  you decode that way; with `ReadBytes` you accumulate the offset yourself.
- Constant memory, not throughput, is the differentiator — guard the ≥1 GB test
  with `testing.Short()`.

## Links

- [CAPTURE.md §6.2](../../docs/CAPTURE.md) (reading transcripts at scale)
- Issue: [#4 Streaming JSONL reader with byte-offset resume cursor](https://github.com/Atharva-Kanherkar/proofswe/issues/4)

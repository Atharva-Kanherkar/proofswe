# proofswe

**SWE-bench tells you how models score in the lab. This is proof from real work.**

proofswe is an open benchmark for coding agents, built from real developer
sessions instead of synthetic tasks. The question it answers is the one no
existing benchmark can:

> Oracle benchmarks measure whether a model can close tasks that *have answers*.
> proofswe measures whether a model's work *survives* in tasks that don't.

Status: **design exploration.** No collector, no binary yet. We are nailing the
hard parts on paper before writing a line of code:

- [`docs/METHODOLOGY.md`](docs/METHODOLOGY.md) — how raw real-world sessions
  become a statistically meaningful benchmark (survival analysis, IRT,
  hierarchical Bradley–Terry, learning the metric weights from the OSS merge
  oracle). This is what decides whether proofswe is a benchmark or just telemetry
  with a leaderboard attached.
- [`docs/CAPTURE.md`](docs/CAPTURE.md) — the data-capture pipeline architecture:
  a Go binary that captures Claude Code / Codex / (later) Cursor sessions
  through a harness-agnostic narrow waist, backed by how the best Go CLIs
  (gh, hugo, fzf, GoReleaser, go-git) actually build this.

## Why this category is empty

Every serious coding benchmark today is **oracle-based**: it needs a
machine-checkable definition of success *before* the task runs.

| Benchmark | The question it actually answers |
|---|---|
| SWE-bench | Can the model close historical issues that happen to have test oracles? |
| Terminal-Bench / Aider | Can the model complete synthetic tasks with verifiable end states? |
| Arena-style (Copilot Arena, LMArena) | Which output do strangers prefer at a glance? |
| **proofswe** | **Whose work do informed owners keep, in tasks that have no oracle?** |

The first three are *pre-task* benchmarks. Success must be definable before work
starts, which structurally forbids ambiguity. But ambiguity is the defining
property of real software engineering: "refactor this," "build me a dashboard,"
"make it faster," "figure out why staging is weird." None of those can ever
enter an oracle benchmark, no matter how good the benchmark gets.

proofswe is a *post-task* benchmark. Success is defined by what happened
*afterward*: did the person who owns the codebase and knows the real
requirements **keep** the work, **commit** it, **build on top of it** — or
revert it. That is the most informed judgment available for an unverifiable
task, and it is expressed as a costly action rather than a cheap rating.

## Three properties no existing benchmark has at once

- **Contamination-proof by construction.** Every datapoint is a fresh task in a
  private codebase that did not exist at training time.
- **Ungameable in the usual way.** A lab cannot fine-tune against a metric that
  lives downstream of thousands of uncontrolled real environments. (One real
  attack vector exists — astroturfing the public pool — and the methodology
  addresses it head-on.)
- **The real task distribution.** Including the boring, ambiguous, underspecified
  majority of work that defines what "good at software engineering" means.

## The collection ladder

Ordered by how much the data is worth to research:

1. **Observational outcomes** — privacy-safe, line-hash telemetry. Scales widest,
   weakest for causal claims.
2. **Paired replay** — the same real task attempted by two models, judged by the
   same informed user. One paired comparison is worth 10–100 observational ones.
   The feature users want most is also the data researchers need most.
3. **Open-source transcripts** — when the repo is public, the privacy cost of
   donating the full transcript collapses, and PR merge status becomes an
   external oracle. This is the successor dataset to SWE-bench.

The leaderboard is marketing for the dataset. The dataset — real multi-turn
trajectories with survival-based reward signals, openly licensed and auditable —
is the research contribution the labs sit on privately and nobody else has.

## Current Pipeline

Install the CLI through npm:

```sh
npx proofswe version
```

Submit a real Codex or Claude Code transcript for server-side judging:

```sh
proofswe submit ~/.codex/sessions/.../rollout-*.jsonl --harness=codex
```

`submit` builds the same scrubbed reproducible task as `contribute`, sends it to
the proofswe API, and prints the server scorecard. The contributor does not need
an OpenAI or Anthropic key; the official judge runs on the proofswe server. Use
`PROOFSWE_API_URL` or `--endpoint` to point at a staging server.

Run the staging judge endpoint with server-side credentials:

```sh
OPENAI_API_KEY=... proofswe serve --addr=:8080 --judge-provider=openai
```

## License

MIT. See [`LICENSE`](LICENSE).

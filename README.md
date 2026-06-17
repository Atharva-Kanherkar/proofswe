# proofswe

Open benchmarking for coding agents, built from real developer sessions.

`proofswe` turns a Codex or Claude Code transcript into a reproducible task,
submits it to the hosted judge, and returns an official scorecard. The goal is to
measure whether an agent's work survives real ambiguous software work, not only
whether it passes a pre-written oracle.

## Install

```sh
npx -y proofswe version
```

## Submit A Session

From the repository you worked in:

```sh
npx -y proofswe submit
```

By default, `submit` finds the latest supported transcript for the current git
repo, builds a scrubbed task, sends it to the proofswe API, waits for judging,
and prints the scorecard.

Useful options:

```sh
proofswe submit <transcript.jsonl>
proofswe submit --json
proofswe submit --no-wait
proofswe submit --endpoint https://your-api.example/v1/submissions
```

## Agent Helpers

Install optional chat helpers for Codex and Claude Code:

```sh
npx -y proofswe agent install
```

Then ask the agent to use the `proofswe-benchmark` helper from inside the coding
session.

## Self-Host The API

```sh
OPENAI_API_KEY=... proofswe serve --addr=:8080 --judge-provider=openai
```

Optional runtime configuration:

```sh
DATABASE_URL=postgres://...
PROOFSWE_GITHUB_TOKEN=github_pat_...
PROOFSWE_CORPUS_REPO=owner/proofswe-corpus
PROOFSWE_JUDGE_MODEL=gpt-5.4-mini
```

If `DATABASE_URL` is unset, the server uses an in-memory queue. If
`PROOFSWE_GITHUB_TOKEN` is unset, judged submissions are not published to a
corpus repository.

## Development

```sh
go test ./...
go build ./cmd/proofswe
```

Release builds are produced by GitHub Actions with GoReleaser and published to
npm as the `proofswe` package plus native optional dependencies.

## License

MIT

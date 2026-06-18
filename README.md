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

The npm package installs the bundled Codex and Claude Code benchmark helpers
during install, so agents can run the submission flow from inside chat. When the
install runs in an interactive terminal, it also asks once whether proofswe may
publish captured raw code snippets/patches from public repos in submitted tasks.

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

After install, ask your coding agent to use `/benchmark` or the
`proofswe-benchmark` helper from inside the session. To repair or reinstall the
helpers manually:

```sh
proofswe agent install
```

To accept the public corpus code-publishing prompt later or from automation:

```sh
proofswe agent install --accept-code-publication-agreement
```

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

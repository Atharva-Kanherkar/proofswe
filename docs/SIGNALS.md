# Signal Catalog

> Seeds [#28](https://github.com/Atharva-Kanherkar/proofswe/issues/28) (part of the v1
> benchmark epic [#27](https://github.com/Atharva-Kanherkar/proofswe/issues/27)). Source-of-truth
> list of the signals proofswe extracts from a session. `docs/METHODOLOGY.md` predates the
> multi-signal design and is stale — prefer this file and the v1 issues.

## These signals assume consented, shared data

The benchmark runs on data from participants who **opt in and sign a data-sharing agreement**.
They share the real session — the prompts they wrote, the tool calls the agent made and their
outputs, the code it produced, and the PR. **Reading that content is the product.** The signals
below assume we have it.

(Separately, the capture CLI has an on-by-default *ambient* mode that stores only salted hashes
for users who have **not** opted in — a privacy feature documented in [`CAPTURE.md`](CAPTURE.md).
That mode is not what the benchmark is built on and is intentionally out of scope here. The only
hashing in this catalog is optional pseudonymization of *identities* — `user` and `repo` IDs —
which protects who/where without blinding any signal.)

## Signal roles

Every signal is exactly one of three roles, handled differently:

- **Outcome** — "did it go well?" → **score it**.
- **Cost** — "what did it burn?" → **trade off**, never add to the score.
- **Context** — "what was it up against?" → **condition on**, never score.

**Source:** ✓ already captured · ⊕ needs new/extended capture · 🤖 derived (judge / git / analysis).

---

## Outcome — score these

All read from the transcript at session end — no longitudinal tracking, no waiting on a repo.

| signal | why it matters | source | type |
|---|---|---|---|
| tests / build / lint passed | the agent ran them *in-session* and they passed — the strongest objective in-transcript signal | 🤖 | binary |
| instruction-following | did it actually do what the prompt asked (vs adjacent / ignored) | 🤖 | ordinal |
| code quality | tests written, readable, no obvious vulns/secrets in the produced diff | 🤖 | ordinal |
| satisfaction (inferred) | the ~95% of sessions with no explicit rating still have a felt outcome | 🤖 | ordinal |
| developer accepted | in-session sign-off — "lgtm", ran it, moved on without correction (the calibration label) | 🤖 | binary |
| explicit user rating | most direct satisfaction signal, where it appears in the session | ⊕ | ordinal |
| abandonment | dropped mid-task / rage-quit = bad ending | 🤖 | binary |
| ended with a commit/push | the session reached a commit — an in-transcript *action*, not tracked forward | ✓ | binary |

## Cost — trade off, never add to the score

| signal | why it matters | source | type |
|---|---|---|---|
| $ / task | the buyer's real axis; powers the success-per-dollar frontier | ⊕ (tokens × price) | continuous |
| tokens (in/out/cache) | raw compute; backs $ and efficiency | ✓ | continuous |
| tool calls (total + per tool) | how much machinery it needed | ✓ | count |
| web fetches | did it have to browse — research burden | 🤖 | count |
| wall-clock | latency / time cost | ✓ (`duration_ms`) | continuous |
| turns | round-trips to get there | ✓ (`turn_count`) | count |
| retries / tool errors | self-inflicted thrash | 🤖 | count |
| human corrections | friction — how often the person had to step in | 🤖 | count |
| subagent spawns | orchestration overhead | ✓ (`is_subagent`) | count |

## Context — condition on, never score

| signal | why it matters | source | type |
|---|---|---|---|
| **model + version** | **the treatment — the label we rank by** (cleartext, always) | ✓ | categorical |
| user (pseudonymous id) | cluster / fixed effect — captures task routing | ✓ | key |
| repo (pseudonymous id) | cluster — codebase difficulty | ✓ | key |
| language / stack | difficulty + slicing | ✓ | categorical |
| task type (bug/feature/refactor/Q) | normalize across kinds of work | 🤖 | categorical |
| ambiguity / underspecification | under-specified tasks are genuinely harder | 🤖 | ordinal |
| scope attempted (files/funcs/lines) | normalize ambition — a 5-line fix ≠ a 500-line feature (a *normalizer*, not an outcome) | ✓ / 🤖 | count |
| prompt size / count | spec richness | ✓ (`spec_signals`) | count |
| references external context | screenshots/URLs the agent literally couldn't see | ✓ | binary |
| public + license | unlocks the OSS merge-oracle | ✓ | categorical |
| had failing test at base | was there a reproducible starting point | ✓ (`spec_signals`) | binary |

## What we build on first (priority order)

1. **model · user · repo · language · task type** (context — who / what / which)
2. **instruction-following · code quality · inferred satisfaction** (outcome — the content-read signals that most separate models)
3. **tests passed in-session · ended with a commit · clean termination** (outcome — the objective in-transcript anchors)
4. **$ · tool calls · turns** (cost — the trade-off axis)

## Treat with care

- **tests / build / lint** — only counts when the agent actually *ran* them in the session (visible in tool outputs); absence is "unknown," not failure. Not every session runs tests.
- **developer accepted** — strong but flow-dependent: solo/personal sessions sign off differently than team reviews. It's the calibration label, so its gameability matters (METHODOLOGY §3.2).
- **explicit rating** — the most direct signal but sparse; lean on inferred satisfaction for coverage and use ratings to calibrate it.

## Proposed additions (flagged for review)

1. **self-correction ratio** — when a tool call errored, did the *agent* recover or did the *human* fix it? Separates autonomous from needs-babysitting.
2. **rework / thrash** — same file/line edited repeatedly *within* the session — an in-session "flailing" proxy.

## Related issues

- [#29](https://github.com/Atharva-Kanherkar/proofswe/issues/29) capture the ⊕ signals · [#30](https://github.com/Atharva-Kanherkar/proofswe/issues/30) the 🤖 derived layer · [#32](https://github.com/Atharva-Kanherkar/proofswe/issues/32) how these combine into the score.

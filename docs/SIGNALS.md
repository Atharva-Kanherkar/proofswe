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

| signal | why it matters | source | type |
|---|---|---|---|
| PR merged | the gold oracle on OSS — a human reviewer accepted it | ⊕ | binary |
| PR review outcome | approvals / change-requests / review comments — *graded* human judgment, not just merged y/n | ⊕🤖 | ordinal |
| instruction-following | did it actually do what the prompt asked (vs adjacent / ignored) | 🤖 | ordinal |
| code quality | tests written, readable, no obvious vulns/secrets introduced | 🤖 | ordinal |
| satisfaction (inferred) | the ~95% of sessions with no explicit rating still have a felt outcome | 🤖 | ordinal |
| explicit user rating | most direct satisfaction label, where it exists | ⊕ | ordinal |
| abandonment | dropped mid-task / rage-quit = bad ending | 🤖 | binary |
| committed | agent's lines reached a commit — first "kept" gate | ✓ | binary |
| survival @1h/24h/7d/30d | did the work persist or get ripped out — **one** signal, not the metric | ✓ | fraction |
| reverted | explicit undo of agent work — clean negative | 🤖 | binary |
| churn-after-keep | kept-but-constantly-rewritten is worse than kept-and-stable | ⊕ | fraction |
| tests / build / lint pass | objective correctness — but needs *execution*: expensive, OSS-subset only | ⊕ | binary |
| follow-on build | user continued *from* this work next session → good enough to build on | 🤖 | binary |

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
2. **instruction-following · code quality · PR review outcome · inferred satisfaction** (outcome — the signals that need us to read the content, and the ones that actually separate models)
3. **PR merged · committed · survival** (outcome — objective anchors)
4. **$ · tool calls · turns** (cost — the trade-off axis)

## Treat with care

- **keeprate / survival** — trivial lines (`}`, `return nil`, imports) survive regardless; its validity *as a quality signal* is the still-unverified assumption tracked in [#35](https://github.com/Atharva-Kanherkar/proofswe/issues/35). Keep it; don't headline it.
- **tests / build pass** — the cleanest outcome, but capturing it means *running* code: expensive, invasive, usually impossible on private repos. Treat as an OSS-subset signal, not universal.
- **explicit rating** — the best label but sparse; lean on inferred satisfaction for coverage and use ratings to calibrate it.

## Proposed additions (flagged for review)

1. **follow-on build** — did the user continue *from* this work next session rather than redo it? Cheap, hard to game, "good enough to build on."
2. **self-correction ratio** — when a tool call errored, did the *agent* recover or did the *human* fix it? Separates autonomous from needs-babysitting.
3. **rework / thrash** — same file/line edited repeatedly *within* the session — an in-session "flailing" proxy that doesn't wait 30 days.

## Related issues

- [#29](https://github.com/Atharva-Kanherkar/proofswe/issues/29) capture the ⊕ signals · [#30](https://github.com/Atharva-Kanherkar/proofswe/issues/30) the 🤖 derived layer · [#32](https://github.com/Atharva-Kanherkar/proofswe/issues/32) how these combine into the score.

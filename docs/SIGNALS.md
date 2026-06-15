# Signal Catalog

> Seeds [#28](https://github.com/Atharva-Kanherkar/proofswe/issues/28) (part of the v1
> benchmark epic [#27](https://github.com/Atharva-Kanherkar/proofswe/issues/27)). This is
> the source-of-truth list of signals proofswe extracts from a session and how each is
> treated. `docs/METHODOLOGY.md` predates the multi-signal design and is stale — prefer
> this file and the v1 issues.

## Priority: the benchmark reads the data

The privacy floor (`hashes-only`: salted line-hashes + coarse metadata) **exists and is the
default**, but it is **not the product**. A leaderboard built only on hashed counts and
line-survival is thin and easy to misread. The signals that actually separate models —
*did it do what was asked, was the code any good, what did the PR review say, was the human
satisfied* — **only exist if we read the granted content**: the prompts, the tool calls and
their outputs, the produced code, and the PR.

So the build priority is **rich mode first** (content granted via the `prompts` / `actions` /
`code` / `full` tiers, plus the derived-analysis tier [#31](https://github.com/Atharva-Kanherkar/proofswe/issues/31)).
Hashed mode is a degraded fallback that still yields *a* number, not the target experience.

## Signal roles

Every signal is exactly one of three roles, and each role is handled differently:

- **Outcome** — "did it go well?" → **score it** (merged, satisfied, tests pass, kept).
- **Cost** — "what did it burn?" → **trade off**, never add to the score ($, tool calls, tokens, turns).
- **Context** — "what was it up against?" → **condition on**, never score (language, repo, task type, ambiguity).

## Legend

- Source: ✓ already captured · ⊕ needs new/extended capture · 🤖 derived (judge / git / analysis)
- Mode: 🔓 **needs granted content** (the priority signals) · 🔒 available in hashed/degraded mode
- Tier: lowest consent tier that exposes the signal

---

## Outcome — score these

| signal | why it matters | source | mode | type | tier |
|---|---|---|---|---|---|
| PR merged | the gold oracle on OSS — a human reviewer accepted it | ⊕ | 🔓 | binary | repo-linkage |
| PR review outcome | approvals / change-requests / review comments — *graded* human judgment, not just merged y/n | ⊕🤖 | 🔓 | ordinal | repo-linkage |
| instruction-following | did it actually do what the prompt asked (vs adjacent/ignored) | 🤖 (#30) | 🔓 | ordinal | derived-analysis |
| code quality | tests written, readable, no obvious vulns/secrets introduced | 🤖 | 🔓 | ordinal | code |
| satisfaction (inferred) | the ~95% of sessions with no rating still have a felt outcome | 🤖 (#30) | 🔓 | ordinal | derived-analysis |
| explicit user rating | most direct satisfaction label, where it exists | ⊕ | 🔓 | ordinal | low-sensitivity |
| abandonment | dropped mid-task / rage-quit = bad ending | 🤖 (#30) | 🔓 | binary | derived-analysis |
| committed | agent's lines reached a commit — first real "kept" gate | ✓ (resolve) | 🔒 | binary | hashes-only |
| survival @1h/24h/7d/30d | did the work persist or get ripped out — **one** signal, not the metric | ✓ (line-hash) | 🔒 | fraction | hashes-only |
| reverted | explicit undo of agent work — clean negative | 🤖 (git) | 🔒 | binary | hashes-only |
| churn-after-keep | kept-but-constantly-rewritten is worse than kept-and-stable | ⊕ | 🔒 | fraction | hashes-only |
| tests / build / lint pass | objective correctness — but needs *execution*: expensive, OSS-subset only | ⊕ | 🔓 | binary | actions+ |
| follow-on build | user continued *from* this work next session → good enough to build on | 🤖 | 🔒 | binary | hashes-only |

## Cost — trade off, never add to the score

| signal | why it matters | source | mode | type | tier |
|---|---|---|---|---|---|
| $ / task | the buyer's real axis; powers the success-per-dollar frontier | ⊕ (tokens × price) | 🔒 | continuous | hashes-only |
| tokens (in/out/cache) | raw compute; backs $ and efficiency | ✓ (msg metrics) | 🔒 | continuous | hashes-only |
| tool calls (total + per tool) | how much machinery it needed; *which* tools needs content | ✓ / 🔓 detail | 🔒/🔓 | count | hashes-only |
| web fetches | did it have to browse — research burden | 🤖 (tool names) | 🔓 | count | actions |
| wall-clock | latency / time cost | ✓ (`duration_ms`) | 🔒 | continuous | hashes-only |
| turns | round-trips to get there | ✓ (`turn_count`) | 🔒 | count | hashes-only |
| retries / tool errors | self-inflicted thrash | 🤖 (tool_outputs) | 🔓 | count | actions |
| human corrections | friction — how often the person had to step in | 🤖 (#30) | 🔓 | count | derived-analysis |
| subagent spawns | orchestration overhead | ✓ (`is_subagent`) | 🔒 | count | hashes-only |

## Context — condition on, never score

| signal | why it matters | source | mode | type | tier |
|---|---|---|---|---|---|
| model + version | **the treatment** being benchmarked | ✓ | 🔒 | categorical | hashes-only |
| user (hashed) | cluster / fixed effect — captures task routing | ✓ | 🔒 | key | hashes-only |
| repo (hashed) | cluster — codebase difficulty | ✓ | 🔒 | key | hashes-only |
| language / stack | difficulty + slicing | ✓ (tools/lockfiles) | 🔒 | categorical | hashes-only |
| task type (bug/feature/refactor/Q) | normalize across kinds of work | 🤖 (#30) | 🔓 | categorical | derived-analysis |
| ambiguity / underspecification | under-specified tasks are genuinely harder | 🤖 (#30) | 🔓 | ordinal | derived-analysis |
| scope attempted (files/funcs/lines) | normalize ambition — a 5-line fix ≠ a 500-line feature (a *normalizer*, not an outcome) | ✓ / 🤖 | 🔒 | count | hashes-only |
| prompt size / count | spec richness | ✓ (`spec_signals`) | 🔒 | count | hashes-only |
| references external context | screenshots/URLs the agent literally couldn't see | ✓ | 🔒 | binary | hashes-only |
| public + license | unlocks the OSS merge-oracle + raw-code | ✓ | 🔒 | categorical | repo-linkage |
| had failing test at base | was there a reproducible starting point | ✓ (`spec_signals`) | 🔒 | binary | hashes-only |

## What we build on first (priority order)

Rich-mode core — the signals that make the benchmark *useful*, most of which need content:

1. **model · user · repo · language · task type** (context — who/what/which)
2. **instruction-following · code quality · PR review outcome · inferred satisfaction** (outcome — needs content)
3. **PR merged · committed · survival** (outcome — objective anchors)
4. **$ · tool calls · turns** (cost — the trade-off axis)

Degraded mode (hashed only) keeps #3 + #4 + the 🔒 context — a real but much thinner leaderboard.

## Treat with care

- **keeprate / survival** — trivial lines (`}`, `return nil`, imports) survive regardless; its validity *as a quality signal* is the still-unverified assumption tracked in [#35](https://github.com/Atharva-Kanherkar/proofswe/issues/35). Keep it; don't headline it.
- **tests / build pass** — the cleanest outcome, but capturing it means *running* code: expensive, invasive, usually impossible on private repos. Treat as an OSS-subset signal, not universal.
- **explicit rating** — the best label but sparse and annoying to elicit; lean on inferred satisfaction for coverage and use ratings to calibrate it.

## Proposed additions (flagged for review)

1. **follow-on build** — did the user continue *from* this work next session rather than redo it? Cheap, hard to game, "good enough to build on."
2. **self-correction ratio** — when a tool call errored, did the *agent* recover or did the *human* fix it? Separates autonomous from needs-babysitting.
3. **rework / thrash** — same file/line edited repeatedly *within* the session — an in-session "flailing" proxy that doesn't wait 30 days.

## Related issues

- [#29](https://github.com/Atharva-Kanherkar/proofswe/issues/29) capture the ⊕ signals · [#30](https://github.com/Atharva-Kanherkar/proofswe/issues/30) the 🤖 derived layer · [#31](https://github.com/Atharva-Kanherkar/proofswe/issues/31) the derived-analysis consent tier · [#32](https://github.com/Atharva-Kanherkar/proofswe/issues/32) how these combine.

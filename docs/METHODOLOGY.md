# proofswe — Benchmark Methodology

> **What this is.** The spec for proofswe as a **research benchmark and a corpus**
> — a collective of *real* collaborative coding sessions — in the lineage of
> SWE-bench and Sierra's [τ-bench / τ²-bench](https://sierra.ai/blog/benchmarking-agents-in-collaborative-real-world-scenarios).
> It is **not a product and not a developer tool.** The deliverables are a dataset,
> a methodology, a leaderboard, and a paper. The CLI (`proofswe score`, the judge)
> is the *evaluation harness* that produces those — never the thing itself.
>
> **Everything is computed from the transcript at session end.** No longitudinal
> tracking, no survival horizons, no following a repo for days. If a signal can't
> be read off the session as it closes, it is not in the benchmark.
>
> Judgment calls still open are marked **[OPEN]**.

---

## 0. The thesis

**Synthetic benchmarks trade realism for control. proofswe has realism for free
and must use statistics to buy back the control.**

A synthetic benchmark gets clean comparisons because the task, the difficulty, and
the success criterion are fixed by the author before any model runs — everything
held constant by construction. The price: the author samples from their
*imagination* of the task distribution, and real users do not live there.

Real sessions invert the trade. The task distribution is the true empirical one —
the irreplaceable property — but nothing is held constant: different users,
codebases, difficulties, and a non-random choice of which model gets which task.
The whole job of this methodology is to **recover experimental rigor from
observational data** — to estimate "what does switching model do, holding
everything else fixed" when nothing was held fixed. That is a solved class of
problem in econometrics and psychometrics.

**Where proofswe sits.** SWE-bench freezes an oracle-backed task set so all models
are directly comparable. τ-bench/τ²-bench evaluate *collaborative* multi-turn
tasks but with a *simulated* user and a programmatic reward. proofswe's
contribution is the third corner: **genuine, in-the-wild human–AI collaborative
coding, captured rather than simulated, with no pre-written oracle.**

---

## 1. What the benchmark outputs

So it reads as a benchmark, not a dashboard:

- A **leaderboard**: model → calibrated composite + **credible interval**, with the
  component axes always visible (success, efficiency, autonomy, friction). No
  single number without its parts, and **no ranking of models whose intervals
  overlap**.
- **Per-stratum breakdowns**: by language, task type, repo size, harness.
- **The harness contrast**: the same model across Claude Code / Codex / Cursor —
  how much of "agent performance" is model vs scaffolding, on real work. A paper on
  its own.
- **The lab-vs-field gap**: proofswe rank vs SWE-bench Verified per model. "High on
  SWE-bench, bottom-quartile in the field" is both the launch finding and a
  legitimate result on benchmark validity.
- **Reproducible instances** (§6): a curated, replayable subset others can run new
  models against.
- **Versioned dataset releases** with a datasheet and a documented, criticizable
  scoring function. Academics cite what they can audit.

The shipped CLI primitives map to this: `docs/SIGNALS.md` is the signal catalog,
`proofswe score` computes the per-session axes from transcript signals, and
`internal/judge` is an optional human-reaction supplement for the success axis.
They are the **per-instance evaluation function** — the leaderboard is the
aggregate of running it across the corpus.

---

## 2. The corpus and the signals

The atomic unit is a **session**: one task attempt, by one model, in one harness,
on one repo, by one user. The corpus is the collection of these — the actual
research asset.

Each session yields an **outcome vector**, never a premature scalar, and **every
signal is read from the transcript as the session closes** — there is no waiting
and no repo follow-up. The full catalog lives in [`docs/SIGNALS.md`](SIGNALS.md);
the load-bearing rule is that **every signal has a role**:

- **Outcome** — "did it go well?" → *score it* (tests that ran and passed, a clean end, the developer accepting the work, the session ending in a commit).
- **Cost** — "what did it burn?" → *trade off, never add to the score* ($, tool calls, tokens, turns).
- **Context** — "what was it up against?" → *condition on, never score* (model, user, repo, language, task type, scope).

The signals that most separate models — instruction-following, code quality,
satisfaction — require reading the *consented content* of the session; privacy
tiers are a capture-side concern, not the lens for scoring.

---

## 3. Scoring a session

### 3.1 Objective-first; the judge is a calibrated supplement

τ²-bench scores "without relying on subjective grading or LLM-based evaluation,"
and that is the right instinct. The composite **leans on the verifiable in-session
signals** — tests/build/lint that *ran and passed* in the transcript, a clean
termination, the work committed within the session — never on anything observed
after the session ends. The behavioral **judge** (`internal/judge`, issue #30)
supplies only the *human-experience* axis the verifiable signals can't see, and it
is anchored and calibrated to them — a supplement, not the quality engine.

The judge reads **how the developer reacted**, not the assistant's output (grading
output directly is bias-prone). One **blinded** call per transcript (model identity
never enters the prompt, so a judge cannot favor its own family) yields
`{outcome, corrections, sentiment}` → a 0–100 success score.

### 3.2 Learn the weights — do not guess them

Hand-picked weights ("a clean test run is worth 3× a commit") are arbitrary and
every vendor will argue they were chosen to favor someone. Instead, **learn them
from the subset where the transcript itself shows verified success** — tests passed
in-session *and* the developer accepted the work (lgtm / ran it / no further
correction):

```
P(verified_success | session) = f(in_transcript_signals)
```

The fitted coefficients *are* the weighting function — empirically anchored — and
applying them to the rest yields a **calibrated quality score**. The label is read
off the same transcript; there is no future merge or survival to wait on. This also
answers a real research question: *which observable session features predict that a
developer signed off?*

### 3.3 Two axes, never one number

A naive quality score rewards cowardice (the **timid-model problem**): a model that
makes tiny safe edits draws fewer corrections and looks clean. So quality is
reported **conditional on scope**, with scope/ambition as a separate axis measured
in a hard-to-inflate unit (AST-level edits / functions touched, cross-checked
against an LLM-judged task-size estimate) — **never raw line count**.

**[OPEN]** The scope unit is the single most consequential pre-launch decision; it
defines what the leaderboard pushes the ecosystem toward.

---

## 4. Comparability and ranking

### 4.1 Different tasks → Item Response Theory

"Model X scored 85%" is meaningless when every task differs. Borrow IRT: each task
is an **item** with latent difficulty `b_j`, each model a test-taker with ability
`θ_m`, success a function of the gap `logistic(a_j·(θ_m − b_j))`. Ability is
estimated *after* accounting for the difficulty of the tasks a model happened to
face. Externally validated for sparse, unbalanced eval matrices (tinyBenchmarks,
the IRT-for-NLP line).

### 4.2 Non-random assignment → comparability must be *engineered*

Users route tasks to models non-randomly (hard tasks → the trusted model), so raw
mean scores are confounded. Two defenses:

- **Fixed effects:** `y = α_model + φ_user + ψ_repo + Xβ + ε`; `α_model` is the
  effect *net of who used it and on what*, identified off **within-user-repo model
  switching** — the same person, same codebase, switching models.
- **Release windows as natural experiments:** a new release makes users switch
  mid-stream on one repo — an event-study / difference-in-differences contrast.

Crucially, you **cannot impute** a model's performance on tasks it never attempted
from a small shared probe set — that shortcut is empirically refuted (arXiv
2409.03563), and organic non-overlapping tasks are exactly its failure regime. So
comparability comes from *real engineered overlap* (within-user switches, an opt-in
same-task-two-models mode), logged — never from imputation. The routing bias is
**conservative** (harder tasks → stronger model biases *against* it); naming the
direction of a confound is more credible than claiming none.

### 4.3 One ranking, with honesty about reliability

Convert within-stratum outcomes into pairwise comparisons and fit **hierarchical
Bradley–Terry** (`P(A beats B)=logistic(θ_A − θ_B)`, `θ_m~Normal(μ,τ²)`) — the
Chatbot-Arena math, except the "vote" is a *revealed in-session outcome*, not a
30-second preference. The hierarchical prior fixes cold-start (few-comparison
models shrink toward the mean). τ-bench's central lesson is that models are
**inconsistent across repeated trials** (`pass^k`); so report variance with
**clustered / cluster-bootstrap** standard errors (data is nested: sessions ⊂
users ⊂ repos), and **never rank overlapping intervals**.

**[OPEN]** Pairwise-win definition (threshold vs graded vs the calibrated
composite) and cold-start sample size — both must be **simulated** before any
public leaderboard ships.

---

## 5. Population, validity, and trust

- **Installer bias → post-stratification.** Early adopters skew toward particular
  stacks. Reweight (`weight_k ∝ N_target(k)/N_sample(k)`) so the language /
  task-type / repo-size mix matches a target population, and **publish both** raw
  and reweighted numbers — the gap is itself an honest disclosure.
- **Poisoning.** A public outcome leaderboard invites fake sessions. Defenses
  designed in from day one: identity-weighted contributions (GitHub identity,
  account age, activity — Sybil-resistant), plausibility/fraud checks on the data
  exhaust (real sessions have messy duration/tool-call/timing distributions;
  fabricated ones cluster), published **leave-one-contributor-out** robustness, and
  a transcript-backed verified tier as a trusted anchor the anonymous pool's
  ranking is checked against.
- **Goodhart & judge bias.** Any single headline metric gets gamed; the judge can
  carry model-preference bias. Mitigated by the multi-axis profile, blinding,
  ensemble + calibration of the judge, and leaning on verifiable in-session signals.

---

## 6. Reproducible instances (the citable artifact)

What makes a benchmark *citable* rather than a private dashboard is that others can
re-run new models against it. From the verifiable subset (public repos, real
runnable code) we **mint frozen instances** from real sessions:

```
instance = { repo, base_commit, problem_statement (the prompt), context,
             checklist (the success rubric), reference_diff }
```

Cleaned (redacted, identity-pseudonymized), this is structurally a SWE-bench
instance — except the oracle is a derived **checklist** (+ tests where they exist)
rather than a hidden test set. Any model can be dropped into `base_commit` with the
prompt; its transcript is scored by the **same** transcript-only evaluation against
the **same** checklist. This recovers the direct comparability organic sessions
lack. Necessarily public-repo-only (you can't replay a private repo) — the organic
stream still covers everything else.

Two tiers, then: the **organic stream** (every real session → the live leaderboard,
broad but non-reproducible) and the **curated frozen set** (the reproducible,
re-runnable, publishable artifact — proofswe's "Verified").

---

## 7. Evidence status (what's validated vs our own reasoning)

From the deep-research pass — kept explicit so we don't overclaim:

- **Externally validated:** IRT for difficulty-adjusted ability on sparse matrices
  (§4.1); hierarchical Bradley–Terry for ranking from incomplete comparisons (§4.3);
  contamination-resistance via repo selection (a legal/access *deterrent*, not a
  guarantee).
- **Refuted:** cheaply imputing a new model's per-task outcomes from a small shared
  set (§4.2) — so overlap must be engineered.
- **Designed but NOT yet externally corroborated:** the causal/selection tooling
  (§4.2, §5) and the clustered-variance specifics — these rest on our own reasoning
  and need their own verification pass.

---

## 8. Open questions, collected

1. **Scope unit** (§3.3) — defines what the leaderboard optimizes. Decide before code.
2. **Pairwise-win definition + cold-start size** (§4.3) — simulate before launch.
3. **In-transcript label validity** (§3.2) — does "the developer accepted" generalize across solo vs team flows, and how gameable is it.
4. **Difficulty model** (§4.1) — latent-only vs covariate-anchored hybrid.
5. **Judge calibration** — agreement (κ) vs human labels; ensemble composition.

The honest summary: collecting the data is the easy half. Giving it meaning —
what to condition on, what to weight, what to refuse to collapse, and what to
freeze for reproducibility — is the half that makes this a benchmark.

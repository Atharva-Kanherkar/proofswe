# proofswe — Measurement Methodology

> **Status: design exploration.** This is a working design doc, not a final spec.
> Every section that encodes a real judgment call is marked **[OPEN]**. The point
> of writing it before any code is that the statistics, not the plumbing, decide
> whether proofswe is a benchmark or a pile of telemetry.

---

## 0. The one sentence this whole document expands

**Synthetic benchmarks trade realism for control. proofswe has realism for free
and must use statistics to buy back the control.**

A synthetic benchmark gets clean comparisons because the task, the difficulty,
and the success criterion are fixed by the author before any model runs.
Everything is held constant by construction. The price is that the author is
sampling from their *imagination* of the task distribution, and real users do
not live there.

Real sessions invert the trade. The task distribution is the true empirical one
— that is the irreplaceable property — but nothing is held constant. Different
users, codebases, difficulties, and a non-random choice of which model gets which
task. The entire job of the methodology below is to **recover experimental rigor
from observational data**: to estimate "what does switching model do, holding
everything else fixed" when nothing was actually held fixed. That is a solved
class of problem in econometrics, psychometrics, and survival analysis. We are
not inventing statistics; we are pointing the right existing tools at a dataset
nobody has had before.

---

## 1. The unit of observation and the outcome vector

The atomic unit is a **session**: one task attempt, by one model, in one harness,
on one repo, by one user.

The first discipline: **do not collapse the outcome to a scalar early.** Each
session yields an outcome *vector*. The headline number, if there is one, is a
deliberately chosen and disclosed function of this vector — never the raw input.

```
session = {
  # identity / strata (the things we must condition on, not compare across)
  user_id, repo_id, harness, model, model_version, ts,

  # survival — the core signal, measured at horizons, not once
  survived_1h, survived_24h, survived_7d, survived_30d,   # fraction of lines kept
  committed,           # did any agent line land in a commit (strong, discrete)
  merged,              # PR merged — TRUE ORACLE, only on OSS sessions
  churn_7d, churn_30d, # how much kept code was re-edited afterward

  # scope — what was attempted (so we can normalize ambition)
  files_touched, functions_touched, ast_edits, lines_added,

  # friction — the journey, invisible to oracle benchmarks
  turns, corrections, interruptions, retries, wall_clock_s,

  # optional, low-weight
  user_rating
}
```

Two things in this vector do not exist in any oracle benchmark and are most of
the reason to build proofswe: **churn** (kept-but-constantly-patched is a real,
worse outcome than kept-and-stable) and **friction** (two models with identical
keeprate are not equal if one needed three corrective interventions per task).

---

## 2. Five sources of messiness, five named tools

The way raw data "becomes a benchmark structure like the others" is to map each
form of messiness to a standard, citable statistical instrument. None of these is
exotic. The novelty is the dataset, not the math.

### 2.1 Tasks differ in difficulty → Item Response Theory

The objection that killed the first version was correct: "Fable scored 85%" is
meaningless when every task is different. The field that solved exactly this is
**educational testing**, where students answer different questions of different
difficulty and we still want a comparable ability score.

Borrow Item Response Theory. Treat each session's task as an **item** with a
latent difficulty `b_j`, and each model as a test-taker with latent ability
`θ_m`. The probability the work survives is a function of the gap:

```
P(survive | model m, task j) = logistic( a_j · (θ_m − b_j) )
```

`θ_m` is the model's ability estimate *after* accounting for the difficulty of
the tasks it happened to face. `a_j` is the task's discrimination (how sharply it
separates good from bad models). This is the principled answer to "different
tasks, different users, different codebases": difficulty is **estimated jointly**
with ability rather than assumed away. SWE-bench items have no difficulty model
at all — every solved task counts the same. Difficulty-aware scoring is a genuine
methodological step up and it is directly citable (IRT, 2PL/graded-response).

**[OPEN]** Task difficulty is partly latent and partly observable (diff size,
files touched, language, repo size). Likely a hybrid: observable covariates as a
prior on `b_j`, refined by the survival data. Needs simulation before launch.

### 2.2 The outcome is "time until the code dies" → Survival analysis

The keeprate metaphor *is* a literal statistical method. "Did the work survive,
and for how long" is the textbook setup for **survival analysis**. Use it
properly rather than picking a single arbitrary horizon.

- **Kaplan–Meier curves per model** — the survival function S(t) of agent work,
  with censoring for sessions that have not yet aged to 30 days. This is the
  honest version of "keeprate": a curve, not a point.
- **Cox proportional hazards** for the model contrast, with the codebase and user
  as **frailty (random-effect) terms** so we are comparing within clusters, not
  across them:

```
h_i(t) = h_0(t) · exp( β_model·model_i + β_scope·scope_i + β_X·X_i + u_user + v_repo )
```

The quantity you report is the **hazard ratio** between two models, e.g.
`exp(β_A − β_B) = 1.4`: "code from model A is reverted 1.4× as fast as model B's,
controlling for scope, language, and codebase." That sentence is a clean,
defensible, citable statistic, and it is exactly the kind of finding that defends
against the sycophancy attack — code optimized to *look* acceptable gets kept at
hour one and dies by day 30. Longer horizons are where gaming goes to die, and
survival curves are how you see it.

### 2.3 Models are not assigned at random → fixed effects + natural experiments

This is the deepest threat to validity and must be stated loudly: **users route
tasks to models non-randomly.** Hard tasks go to the model they trust. Raw mean
keeprate per model is therefore confounded garbage.

Two complementary defenses:

**(a) Within-transformation / fixed effects.** Model the outcome with user, repo,
and ideally user×repo fixed effects:

```
y_{i} = α_model + φ_user + ψ_repo + Xβ + ε_i
```

The `α_model` coefficients are the model effect *net of who is using it and on
what*. The effect is identified off **within-user-repo model switching** — the
same person, same codebase, switching models. That is also literally the question
every developer wants answered, which is why per-user heterogeneity is not a bug
to be removed but the signal itself.

**(b) Release windows as natural experiments.** A new model release makes users
switch mid-stream on the same repo. That switch, close in time on one codebase,
is the cleanest contrast available. An **event-study / difference-in-differences**
design around release dates turns the constant churn of the model market into a
stream of quasi-experiments.

Note the direction of the routing bias: harder tasks going to the trusted (often
stronger) model biases *against* that model. So it is a **conservative** bias —
worth saying out loud, because a benchmark that names the direction of its
confounds is more credible than one that claims to have none.

### 2.4 We need one ranking → hierarchical Bradley–Terry

A leaderboard needs a single ordering with uncertainty. Convert within-stratum
outcomes into **pairwise comparisons** and fit Bradley–Terry — the same math as
Chatbot Arena, except the "vote" is a revealed outcome (whose work survived /
committed / merged better), not a 30-second preference.

```
P(A beats B) = logistic( θ_A − θ_B ),   θ_m ~ Normal(μ, τ²)   # partial pooling
```

The hierarchical prior (`θ_m ~ Normal(μ, τ²)`) is not decoration — it is the fix
for the **cold-start problem**. Models with few comparisons shrink toward the
population mean instead of producing wild estimates off three data points. Report
**credible intervals**, and **never rank models whose intervals overlap.** A
leaderboard that launches with unstable rankings burns credibility that does not
come back.

**[OPEN]** What counts as a pairwise "win" when survival is continuous? Options:
threshold (survived@30d), graded/ordinal (Thurstone-style), or the composite from
§3. Leaning toward fitting BT on the *calibrated composite* so the ranking
inherits the learned weights.

**[OPEN]** Cold-start: how many within-user switches before BT is honest? This
must be **simulated** before any public leaderboard ships.

### 2.5 The installer population is biased → post-stratification

Early adopters skew toward particular stacks and languages. To make claims about
"real-world software engineering" broadly, borrow **survey methodology**:
reweight sessions so the language / task-type / repo-size distribution matches a
target population (GitHub's language mix, or a developer-survey distribution).

```
weight_k ∝ N_target(stratum k) / N_sample(stratum k)     # raking / post-stratification
```

Publish **both** the raw and the reweighted numbers. The gap between them is
itself an honest disclosure of who is in the pool.

---

## 3. What to weight — and why you should not guess

This is the question the user asked most directly: *given the outcome vector,
what gets what weight in the headline score?*

The wrong answer is to hand-pick weights ("survival@30d is worth 3× a commit").
Transparent, but arbitrary, and every vendor will argue the weights were chosen
to favor someone.

**The right answer: learn the weights from the subset where a real label exists.**

On **open-source sessions we have a true oracle: PR merge status.** Merged-and-
stayed is ground-truth "good work." So:

1. On the OSS subset, fit a model predicting the oracle from the outcome vector:

```
P(merged_and_survived_30d = 1) = logistic( w · outcome_vector )
```

2. The learned coefficients `w` *are* the weighting function — empirically
   anchored, not asserted.
3. Apply that same `w` to the private / anonymous majority that has no merge
   signal, producing a **calibrated quality score** for sessions where no oracle
   could ever exist.

This converts an arbitrary-weighting argument into a **supervised calibration
problem with a real label**, and it is the statistical version of "the trusted
OSS core calibrates the larger anonymous pool." It is also a research result in
its own right: *which observable session features actually predict good work?*

**[OPEN]** The OSS subset may not be representative of private work (more
open-source-y tasks, different review norms). Calibration likely needs the
post-stratification weights from §2.5 layered on, and a sensitivity analysis
reporting how much the ranking moves under reasonable reweightings.

---

## 4. Scope: two axes, never one number

The **timid-model problem**: pure keeprate rewards cowardice. A model that makes
tiny safe edits gets reverted less. If keeprate is the headline, the leaderboard
optimizes the entire ecosystem toward models that *do less*. This is a real
design decision, not a detail.

The fix is to **refuse to collapse quality and ambition into one number.** Report
two axes:

- **Quality-given-scope** — the survival/merge signal *conditional on* how much
  was attempted (scope enters as a covariate in §2.2/§2.3 and is held fixed).
- **Ambition** — how much scope the model attempts when given a free hand.

A single "surviving work per session" number is gameable by verbosity (more lines
≠ more work). So scope must be measured in a hard-to-inflate unit:

```
scope ≈ f(functions_touched, AST-level edits, files_touched)   # NOT raw line count
```

**[OPEN]** The exact scope unit is the single most important pre-launch decision,
because it defines what the leaderboard pushes the ecosystem toward. Candidate:
AST-edit count cross-checked against a uniform LLM-judged task-size estimate, with
both reported separately so the composite is auditable.

---

## 5. Trust: a public outcome benchmark invites poisoning

The moment the leaderboard matters, a vendor's fans (or the vendor) have an
incentive to feed fake sessions. Oracle benchmarks do not have this problem;
crowd-sourced ones do (LMArena has fought it publicly). Trust cannot be
retrofitted, so it is designed in from day one:

- **Identity-weighted contributions.** Tie contribution to a GitHub identity;
  weight by account age, repo activity, contribution history. Privacy-safe
  outcome data, but Sybil-resistant.
- **Plausibility checks from the data exhaust itself.** Real sessions have messy
  distributions of duration, tool-call count, diff size, timing. Fabricated ones
  cluster. Classic fraud detection on features we already collect.
- **Published robustness.** Every ranking ships with "does this hold if we drop
  the top 1% of contributors / leave-one-contributor-out?" A benchmark that
  publishes its own sensitivity analysis is more credible than one claiming to be
  clean.
- **The OSS tier as anchor.** Transcript-backed, publicly verifiable sessions
  (real repo, real PR, real merge) form a trusted core. If the anonymous pool's
  ranking diverges wildly from the verified core, something is wrong.

---

## 6. What the benchmark actually outputs

So that it reads like a benchmark and not a dashboard:

- A **leaderboard**: model → calibrated composite + credible interval, with the
  component sub-scores always visible (survival curve, commit rate, hazard ratio,
  friction index, ambition). No single number without its parts.
- **Per-stratum breakdowns**: by language, task type, repo size, harness.
- **The harness contrast**: same model across Claude Code / Codex / Cursor. Nobody
  has measured how much of "agent performance" is model vs scaffolding on real
  work. This alone is a paper.
- **The lab-vs-field gap**: proofswe rank vs SWE-bench Verified score per model.
  "74% on SWE-bench, bottom-quartile 30-day survival" is the viral launch finding
  *and* a legitimate result on benchmark validity (cf. the METR study: devs felt
  20% faster, were 19% slower).
- **Versioned dataset releases** with a datasheet and a documented, criticizable
  scoring function. Academics cite what they can audit. "Smaller but open and
  auditable" beat "bigger but closed" before (The Stack vs. internal corpora).

---

## 7. Open questions, collected

1. **Scope unit** (§4) — defines what the leaderboard optimizes. Decide before code.
2. **Cold-start sample size** (§2.4) — simulate how many switches stabilize BT.
3. **Pairwise-win definition** (§2.4) — threshold vs graded vs calibrated composite.
4. **OSS→private transfer** (§3) — is the calibration model representative; how
   much does reweighting move it.
5. **Difficulty model** (§2.1) — latent-only vs covariate-anchored hybrid.
6. **Churn semantics** — reformat/rename inflates churn; needs the same
   normalization care as line-hash survival matching.

The honest summary: collecting the data is the easy half. Giving it meaning —
choosing what to condition on, what to weight, and what to refuse to collapse —
is the half that makes this a benchmark. That is what this document is for.

// Package score turns a single session's measured signals into a transparent,
// provisional scorecard. It is deliberately pure - no IO, no transcript parsing.
// The caller (internal/cli) extracts the signals and hands them in.
//
// What it scores TODAY is session utility: the probability-shaped estimate that
// this transcript represented useful accepted work with tolerable human burden.
// The old axes remain as evidence:
//   - success    (deterministic transcript signals; optional bounded judge nudge)
//   - efficiency (cost + tool calls vs a soft baseline)
//   - autonomy   (tool-error rate)
//   - friction   (user turns vs a soft baseline)
//
// The headline Utility score uses a logistic/sigmoid model over deterministic
// transcript signals. That shape is intentional: the long-run target is
// P(accepted | transcript). Today's coefficients are transparent priors; learned
// weights (issue #32) can replace them once a labeled subset exists. The judge is
// a capped nudge, not the scoring engine.
package score

import (
	"fmt"
	"math"
	"strings"
)

// Signals is the per-session input to scoring. The caller fills what it can
// measure from the transcript; later outcome signals can be added without making
// this package depend on transcript parsing.
type Signals struct {
	Model         string  `json:"model,omitempty"`
	ToolCalls     int     `json:"tool_calls"`
	WebFetches    int     `json:"web_fetches"`
	ToolErrors    int     `json:"tool_errors"`
	Turns         int     `json:"turns"`
	Edits         int     `json:"edits"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	CacheTokens   int64   `json:"cache_tokens"`
	CostUSD       float64 `json:"cost_usd"`
	CostEstimated bool    `json:"cost_estimated"`
	DurationMS    int64   `json:"duration_ms,omitempty"`

	// Deterministic success signals, read straight from the transcript (no LLM):
	// Verification is "passed"/"failed"/"" (none run); Landed is whether the
	// session committed/pushed/opened a PR; Terminated is the clean-vs-abandoned
	// end (nil = unknown). These make the success axis objective-first.
	Verification string `json:"verification,omitempty"`
	Landed       bool   `json:"landed,omitempty"`
	Terminated   *bool  `json:"terminated,omitempty"`

	// Success/SuccessLabel are the behavioral judge's (#30) optional supplement —
	// the human-experience read the deterministic signals can't see. nil when the
	// judge didn't run. score stays judge-agnostic by taking the number.
	Success      *float64 `json:"judge_success,omitempty"`
	SuccessLabel string   `json:"judge_label,omitempty"`

	Extracted *ExtractedSignals `json:"extracted,omitempty"`
}

// Baselines are the soft reference points a single session is scored against.
// They are provisional constants; the population-calibrated version lives in #32.
type Baselines struct {
	CostUSD   float64
	ToolCalls float64
	Turns     float64
}

// DefaultBaselines approximates a "typical" session. Tunable, deliberately central.
var DefaultBaselines = Baselines{CostUSD: 1.0, ToolCalls: 20, Turns: 8}

// Axis is one scored dimension. Present is false for dimensions we cannot score
// yet (e.g. success); such axes carry a Detail explaining what unlocks them.
// Axes are evidence/profile only — the headline is Utility, not an axis mean.
type Axis struct {
	Name    string  `json:"name"`
	Present bool    `json:"present"`
	Score   float64 `json:"score"`
	Detail  string  `json:"detail"`
}

// Utility is the headline per-transcript score. Deterministic is the sigmoid
// score before any judge input; JudgeNudge is capped so a hallucinated judge
// cannot overpower transcript evidence.
type Utility struct {
	Score         float64  `json:"score"`
	Deterministic float64  `json:"deterministic"`
	JudgeNudge    float64  `json:"judge_nudge,omitempty"`
	Confidence    string   `json:"confidence"`
	Logit         float64  `json:"logit"`
	Evidence      []string `json:"evidence,omitempty"`
}

// Result is the scorecard: the per-axis evidence scores plus the headline
// Utility. Composite is kept as a back-compat alias of Utility.Score (it used to
// be the mean over present axes); prefer Utility for the headline number.
type Result struct {
	Model     string  `json:"model,omitempty"`
	Axes      []Axis  `json:"axes"`
	Composite float64 `json:"composite"`
	Utility   Utility `json:"utility"`
	Note      string  `json:"note"`
}

const (
	pendingDetail = "pending — no deterministic success signal or behavioral judge"
	resultNote    = "provisional session utility; sigmoid priors, deterministic-first, judge capped"
	maxJudgeNudge = 12.0
)

// Score scores Signals against the default baselines.
func Score(s Signals) Result { return ScoreWith(s, DefaultBaselines) }

// ScoreWith scores Signals against explicit baselines (used by tests and future
// population-calibrated callers).
func ScoreWith(s Signals, b Baselines) Result {
	axes := []Axis{
		efficiencyAxis(s, b),
		autonomyAxis(s),
		frictionAxis(s, b),
		successAxis(s),
	}
	utility := utilityScore(s, b)

	return Result{Model: s.Model, Axes: axes, Composite: utility.Score, Utility: utility, Note: resultNote}
}

func efficiencyAxis(s Signals, b Baselines) Axis {
	val := round1(100 * (ratio(b.CostUSD, s.CostUSD) + ratio(b.ToolCalls, float64(s.ToolCalls))) / 2)
	cost := fmt.Sprintf("$%.2f", s.CostUSD)
	if s.CostEstimated {
		cost += " (est)"
	}
	return Axis{Name: "efficiency", Present: true, Score: val, Detail: fmt.Sprintf("%s · %d tool calls", cost, s.ToolCalls)}
}

func autonomyAxis(s Signals) Axis {
	rate := 0.0
	if s.ToolCalls > 0 {
		rate = float64(s.ToolErrors) / float64(s.ToolCalls)
	}
	val := round1(100 * (1 - clamp(rate, 0, 1)))
	return Axis{Name: "autonomy", Present: true, Score: val, Detail: fmt.Sprintf("%d error(s) / %d tool calls", s.ToolErrors, s.ToolCalls)}
}

func frictionAxis(s Signals, b Baselines) Axis {
	val := round1(100 * ratio(b.Turns, float64(s.Turns)))
	return Axis{Name: "friction", Present: true, Score: val, Detail: fmt.Sprintf("%d user turns", s.Turns)}
}

func successAxis(s Signals) Axis {
	known := s.Verification != "" || s.Landed || s.Terminated != nil
	if !known && s.Success == nil {
		return Axis{Name: "success", Present: false, Detail: pendingDetail}
	}
	if !known { // only the judge spoke
		detail := s.SuccessLabel
		if detail == "" {
			detail = "judged"
		}
		return Axis{Name: "success", Present: true, Score: clamp(round1(*s.Success), 0, 100), Detail: detail}
	}
	// Objective-first: the deterministic signals set the level; the judge only nudges.
	base, detail := deterministicSuccess(s)
	val := base
	if s.Success != nil {
		val += boundedJudgeNudge(base, *s.Success)
		detail += " + judge"
	}
	return Axis{Name: "success", Present: true, Score: clamp(round1(val), 0, 100), Detail: detail}
}

// deterministicSuccess scores the success axis from transcript-only signals.
// Constants are provisional and transparent; they become learned weights (#32).
func deterministicSuccess(s Signals) (float64, string) {
	score := 55.0 // neutral: work happened and ended, nothing verified either way
	var parts []string
	switch s.Verification {
	case "passed":
		score = 85
		parts = append(parts, "tests passed")
	case "failed":
		score = 30
		parts = append(parts, "tests failed")
	default:
		parts = append(parts, "no tests run")
	}
	if s.Landed {
		score += 10
		parts = append(parts, "committed/PR")
	}
	switch {
	case s.Terminated == nil:
		parts = append(parts, "end unknown")
	case !*s.Terminated:
		score -= 25
		parts = append(parts, "abandoned")
	default:
		parts = append(parts, "clean end")
	}
	return clamp(score, 0, 100), strings.Join(parts, " · ")
}

func utilityScore(s Signals, b Baselines) Utility {
	logit, evidence := deterministicUtilityLogit(s, b)
	deterministic := round1(100 * sigmoid(logit))
	score := deterministic
	var nudge float64
	if s.Success != nil {
		nudge = boundedJudgeNudge(deterministic, *s.Success)
		score = clamp(round1(deterministic+nudge), 0, 100)
		if nudge != 0 {
			evidence = append(evidence, fmt.Sprintf("judge nudge %+0.1f", nudge))
		}
	}
	return Utility{
		Score:         score,
		Deterministic: deterministic,
		JudgeNudge:    round1(nudge),
		Confidence:    utilityConfidence(s),
		Logit:         round1(logit),
		Evidence:      evidence,
	}
}

func deterministicUtilityLogit(s Signals, b Baselines) (float64, []string) {
	logit := -0.35
	evidence := []string{fmt.Sprintf("baseline %+0.2f", logit)}

	// add applies one weighted signal and records its evidence string from the
	// actual delta, so the printed evidence can never drift from the coefficient.
	add := func(delta float64, label string) {
		logit += delta
		evidence = append(evidence, fmt.Sprintf("%s %+0.2f", label, delta))
	}

	switch s.Verification {
	case "passed":
		add(1.6, "verification passed")
	case "failed":
		add(-2.0, "verification failed")
	default:
		add(0, "verification unknown")
	}

	if s.Extracted != nil && s.Extracted.VerifiedAfterEdit {
		add(0.7, "verified after edit")
	}
	if s.Landed {
		add(1.0, "landed action")
	}
	switch {
	case s.Terminated == nil:
		add(0, "termination unknown")
	case *s.Terminated:
		add(0.35, "clean end")
	default:
		add(-1.5, "abandoned")
	}

	if s.Edits > 0 {
		add(0.25, "edits observed")
	}
	if s.Extracted != nil {
		if corrections := capInt(s.Extracted.HumanCorrections, 5); corrections > 0 {
			add(-0.45*float64(corrections), fmt.Sprintf("human corrections %d", corrections))
		}
		if interruptions := capInt(s.Extracted.Interruptions, 4); interruptions > 0 {
			add(-0.25*float64(interruptions), fmt.Sprintf("interruptions %d", interruptions))
		}
		if rework := capInt(s.Extracted.ReworkCount, 5); rework > 0 {
			add(-0.25*float64(rework), fmt.Sprintf("rework %d", rework))
		}
		if s.Extracted.HumanAcceptances > 0 {
			add(0.8, "human acceptance")
		}
	}

	if s.ToolCalls > 0 && s.ToolErrors > 0 {
		rate := float64(s.ToolErrors) / float64(s.ToolCalls)
		add(-1.2*clamp(rate, 0, 1), fmt.Sprintf("tool error rate %.0f%%", 100*rate))
	}
	if s.Turns > int(b.Turns) {
		extra := s.Turns - int(b.Turns)
		add(-0.1*float64(capInt(extra, 12)), fmt.Sprintf("extra human turns %d", extra))
	}

	return logit, evidence
}

func boundedJudgeNudge(base, judgeScore float64) float64 {
	return round1(clamp((judgeScore-base)*0.25, -maxJudgeNudge, maxJudgeNudge))
}

func utilityConfidence(s Signals) string {
	objective := s.Verification != "" || s.Landed || s.Terminated != nil
	friction := s.Extracted != nil && (s.Extracted.HumanCorrections > 0 || s.Extracted.HumanAcceptances > 0 || s.Extracted.Interruptions > 0)
	switch {
	case s.Verification != "" && (s.Landed || friction || s.Terminated != nil):
		return "high"
	case objective || friction || s.Success != nil:
		return "medium"
	default:
		return "low"
	}
}

func sigmoid(x float64) float64 {
	return 1 / (1 + math.Exp(-x))
}

func capInt(n, limit int) int {
	if n < 0 {
		return 0
	}
	if n > limit {
		return limit
	}
	return n
}

// ratio maps a non-negative quantity to (0,1]: 0 -> 1, ref -> 0.5, large -> ~0.
// "Less is better" axes (cost, tool calls, turns) use it so fewer/cheaper scores higher.
func ratio(ref, x float64) float64 {
	if x < 0 {
		x = 0
	}
	if ref <= 0 {
		return 0
	}
	return ref / (ref + x)
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

// price is USD per 1M tokens. Provisional 2026 list-price approximations; the real
// table belongs in config so it can track price changes without a code change.
type price struct{ in, out, cache float64 }

var prices = map[string]price{
	"opus":   {15, 75, 1.5},
	"sonnet": {3, 15, 0.3},
	"haiku":  {0.8, 4, 0.08},
	"gpt":    {2.5, 10, 0.25},
}

var defaultPrice = price{3, 15, 0.3}

// EstimateCostUSD approximates session cost from token counts. The bool is true
// when the model was unrecognized and the default rate was used (cost is a guess).
func EstimateCostUSD(model string, in, out, cache int64) (float64, bool) {
	p, matched := priceFor(model)
	cost := (float64(in)*p.in + float64(out)*p.out + float64(cache)*p.cache) / 1e6
	return math.Round(cost*100) / 100, !matched
}

func priceFor(model string) (price, bool) {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus"):
		return prices["opus"], true
	case strings.Contains(m, "sonnet"):
		return prices["sonnet"], true
	case strings.Contains(m, "haiku"):
		return prices["haiku"], true
	case strings.Contains(m, "gpt"), strings.Contains(m, "codex"), strings.Contains(m, "o1"), strings.Contains(m, "o3"):
		return prices["gpt"], true
	default:
		return defaultPrice, false
	}
}

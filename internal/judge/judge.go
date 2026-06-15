// Package judge derives the quality ("success") axis of a session from the
// transcript by reading how the DEVELOPER reacted to the assistant's work across
// the turns — not by re-evaluating the assistant's output (that is what the
// merged/committed/tests axes are for, and judging output directly is bias-prone).
//
// The whole mechanism is one blinded LLM call per transcript that returns a small
// Verdict, which maps to a 0–100 success score. "Blinded" = the model's identity
// never enters the prompt, so a judge cannot favor its own family and leak that
// into the leaderboard. Implementations: HTTPJudge (real), FakeJudge (offline/tests).
package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Turn is one conversational turn, text only. Tool calls/outputs and the model
// id are intentionally excluded — the developer's reactions already encode them.
type Turn struct {
	Role string // "user" | "assistant"
	Text string
}

// Outcome is how the session resolved for the developer.
type Outcome string

const (
	OutcomeAccepted  Outcome = "accepted"  // approved/used the result
	OutcomeCorrected Outcome = "corrected" // worked only after the developer pushed back
	OutcomeAbandoned Outcome = "abandoned" // gave up / left unresolved
)

// Verdict is the judge's structured read of the developer's reactions.
type Verdict struct {
	Outcome     Outcome `json:"outcome"`
	Corrections int     `json:"corrections"`
	Sentiment   float64 `json:"sentiment"` // -1 (frustrated) .. 1 (delighted)
}

// Judge assesses a (blinded) conversation into a Verdict.
type Judge interface {
	Assess(ctx context.Context, turns []Turn) (Verdict, error)
}

// FakeJudge returns a canned verdict; used for offline tests and as a stand-in
// when no real judge is configured.
type FakeJudge struct {
	V   Verdict
	Err error
}

func (f FakeJudge) Assess(context.Context, []Turn) (Verdict, error) { return f.V, f.Err }

const maxTurnChars = 1500

const instruction = `You are reading a coding session between a developer and an AI assistant (identity hidden).
Judge ONLY how the developer reacted to the assistant's work across the turns — not whether the code looks correct.
Reply with ONLY this JSON: {"outcome":"accepted|corrected|abandoned","corrections":<int>,"sentiment":<number -1..1>}
  accepted  = the developer approved or used the result (e.g. "thanks", "lgtm", moved on)
  corrected = it only worked after the developer pushed back or re-directed
  abandoned = the developer gave up, or the task was left unresolved
  corrections = how many times the developer corrected/redirected the assistant
  sentiment = overall developer tone toward the help, -1 (frustrated) to 1 (delighted)`

// BuildPrompt renders the blinded judging prompt. The model id never appears; the
// assistant is labeled generically.
func BuildPrompt(turns []Turn) string {
	var b strings.Builder
	b.WriteString(instruction)
	b.WriteString("\n\n--- transcript ---")
	for _, t := range turns {
		role := "assistant"
		if t.Role == "user" {
			role = "developer"
		}
		b.WriteString("\n\n")
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(truncate(t.Text, maxTurnChars))
	}
	return b.String()
}

// ParseVerdict tolerantly parses the model's reply (code fences, surrounding
// prose) into a Verdict and validates it.
func ParseVerdict(raw string) (Verdict, error) {
	var v Verdict
	if err := json.Unmarshal([]byte(extractJSON(raw)), &v); err != nil {
		return Verdict{}, fmt.Errorf("judge: decode verdict: %w", err)
	}
	outcome, ok := normalizeOutcome(v.Outcome)
	if !ok {
		return Verdict{}, fmt.Errorf("judge: invalid outcome %q", v.Outcome)
	}
	v.Outcome = outcome
	v.Sentiment = clamp(v.Sentiment, -1, 1)
	if v.Corrections < 0 {
		v.Corrections = 0
	}
	return v, nil
}

// ScoreSuccess maps a Verdict to the 0–100 success axis. The constants are
// provisional and equal-ish; learned weights (issue #32) replace them once a
// labeled subset (PR merge / explicit rating) exists.
func ScoreSuccess(v Verdict) float64 {
	base := map[Outcome]float64{OutcomeAccepted: 100, OutcomeCorrected: 60, OutcomeAbandoned: 15}[v.Outcome]
	return clamp(round1(base-8*float64(v.Corrections)+10*v.Sentiment), 0, 100)
}

// Label is a short human-readable summary of a verdict for the scorecard.
func Label(v Verdict) string {
	return fmt.Sprintf("%s · %d correction(s) · sentiment %+.1f", v.Outcome, v.Corrections, v.Sentiment)
}

func normalizeOutcome(o Outcome) (Outcome, bool) {
	switch Outcome(strings.ToLower(strings.TrimSpace(string(o)))) {
	case OutcomeAccepted:
		return OutcomeAccepted, true
	case OutcomeCorrected:
		return OutcomeCorrected, true
	case OutcomeAbandoned:
		return OutcomeAbandoned, true
	default:
		return "", false
	}
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	if i := strings.IndexByte(s, '{'); i >= 0 {
		if j := strings.LastIndexByte(s, '}'); j > i {
			return s[i : j+1]
		}
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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

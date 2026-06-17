package score

import (
	"math"
	"testing"
)

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.15 {
		t.Errorf("%s = %.2f, want ~%.2f", name, got, want)
	}
}

func axisByName(r Result, name string) (Axis, bool) {
	for _, a := range r.Axes {
		if a.Name == name {
			return a, true
		}
	}
	return Axis{}, false
}

func TestScore_ExecutionAxesRemainAsEvidence(t *testing.T) {
	// 3 tool calls, 1 errored, 2 user turns, ~$1.09 spent.
	s := Signals{Model: "claude-opus-4-7", ToolCalls: 3, ToolErrors: 1, Turns: 2, CostUSD: 1.09}
	r := Score(s)

	eff, ok := axisByName(r, "efficiency")
	if !ok || !eff.Present {
		t.Fatal("efficiency axis missing or not present")
	}
	// (1/(1+1.09) + 20/(20+3)) / 2 * 100 = (0.4785 + 0.8696)/2*100 = 67.4
	approx(t, "efficiency", eff.Score, 67.4)

	auto, _ := axisByName(r, "autonomy")
	approx(t, "autonomy", auto.Score, 66.7) // 1 - 1/3

	fric, _ := axisByName(r, "friction")
	approx(t, "friction", fric.Score, 80.0) // 8/(8+2)

	approx(t, "utility/composite", r.Composite, r.Utility.Score)
	if r.Utility.Confidence != "low" {
		t.Fatalf("confidence = %q, want low for activity-only transcript", r.Utility.Confidence)
	}
}

func TestScore_SuccessIsPendingAndExcluded(t *testing.T) {
	r := Score(Signals{ToolCalls: 1, Turns: 1})
	succ, ok := axisByName(r, "success")
	if !ok {
		t.Fatal("success axis missing")
	}
	if succ.Present {
		t.Error("success axis must be pending (Present=false) until deterministic signals or judge data exist")
	}
	if r.Composite != r.Utility.Score {
		t.Fatalf("composite = %.1f, utility = %.1f; composite should remain the headline score alias", r.Composite, r.Utility.Score)
	}
}

func TestEstimateCostUSD(t *testing.T) {
	cost, est := EstimateCostUSD("claude-opus-4-7", 43000, 5100, 40000)
	if est {
		t.Error("opus should be a recognized model (estimated=false)")
	}
	approx(t, "opus cost", cost, 1.09) // (43000*15 + 5100*75 + 40000*1.5)/1e6

	_, est = EstimateCostUSD("some-unknown-model", 1000, 1000, 0)
	if !est {
		t.Error("unknown model should be flagged as estimated")
	}
}

func boolp(b bool) *bool { return &b }

func TestDeterministicSuccess(t *testing.T) {
	cases := []struct {
		name string
		s    Signals
		want float64
	}{
		{"passed+landed+clean", Signals{Verification: "passed", Landed: true, Terminated: boolp(true)}, 95},
		{"passed+clean", Signals{Verification: "passed", Terminated: boolp(true)}, 85},
		{"failed", Signals{Verification: "failed", Terminated: boolp(true)}, 30},
		{"none+clean", Signals{Terminated: boolp(true)}, 55},
		{"none+abandoned", Signals{Terminated: boolp(false)}, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := axisByName(Score(tc.s), "success")
			if !ok || !a.Present {
				t.Fatal("success should be present from deterministic signals")
			}
			approx(t, "success", a.Score, tc.want)
		})
	}
}

func TestDeterministicSuccess_UnknownTerminationIsNotClean(t *testing.T) {
	a, ok := axisByName(Score(Signals{Verification: "passed"}), "success")
	if !ok || !a.Present {
		t.Fatal("success should be present from verification")
	}
	if a.Detail != "tests passed · end unknown" {
		t.Fatalf("detail = %q, want unknown termination detail", a.Detail)
	}
}

func TestSuccess_JudgeBlendsOntoDeterministic(t *testing.T) {
	j := 100.0
	s := Signals{Verification: "passed", Terminated: boolp(true), Success: &j} // deterministic base 85
	a, _ := axisByName(Score(s), "success")
	approx(t, "bounded nudge", a.Score, 88.8) // 85 + (100-85)*0.25
}

func TestScore_UtilityRewardsVerifiedAcceptedWork(t *testing.T) {
	r := Score(Signals{
		ToolCalls:    5,
		Turns:        3,
		Edits:        2,
		Verification: "passed",
		Landed:       true,
		Terminated:   boolp(true),
		Extracted:    &ExtractedSignals{VerifiedAfterEdit: true, HumanAcceptances: 1},
	})
	if r.Utility.Score < 95 {
		t.Fatalf("utility = %.1f, want high verified utility", r.Utility.Score)
	}
	if r.Utility.Confidence != "high" {
		t.Fatalf("confidence = %q, want high", r.Utility.Confidence)
	}
}

func TestScore_UtilityPenalizesFailureAndFriction(t *testing.T) {
	r := Score(Signals{
		ToolCalls:    10,
		ToolErrors:   5,
		Turns:        16,
		Edits:        1,
		Verification: "failed",
		Terminated:   boolp(false),
		Extracted:    &ExtractedSignals{HumanCorrections: 5, Interruptions: 2, ReworkCount: 3},
	})
	if r.Utility.Score > 5 {
		t.Fatalf("utility = %.1f, want very low utility for failed high-friction session", r.Utility.Score)
	}
}

func TestScore_JudgeIsBoundedNudge(t *testing.T) {
	j := 100.0
	r := Score(Signals{Verification: "failed", Terminated: boolp(false), Success: &j})
	if r.Utility.JudgeNudge != maxJudgeNudge {
		t.Fatalf("judge nudge = %.1f, want capped %.1f", r.Utility.JudgeNudge, maxJudgeNudge)
	}
	if r.Utility.Score > r.Utility.Deterministic+maxJudgeNudge {
		t.Fatalf("utility = %.1f, deterministic = %.1f; judge exceeded cap", r.Utility.Score, r.Utility.Deterministic)
	}
}

func TestScore_UtilityUsesSigmoidShape(t *testing.T) {
	weak := Score(Signals{ToolCalls: 1, Turns: 1})
	medium := Score(Signals{Verification: "passed", Terminated: boolp(true)})
	strong := Score(Signals{Verification: "passed", Landed: true, Terminated: boolp(true), Edits: 1})
	if !(weak.Utility.Score < medium.Utility.Score && medium.Utility.Score < strong.Utility.Score) {
		t.Fatalf("utility should rise monotonically with stronger evidence: weak %.1f medium %.1f strong %.1f",
			weak.Utility.Score, medium.Utility.Score, strong.Utility.Score)
	}
}

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

func TestScore_ExecutionAxes(t *testing.T) {
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

	approx(t, "composite", r.Composite, 71.4)
}

func TestScore_SuccessIsPendingAndExcluded(t *testing.T) {
	r := Score(Signals{ToolCalls: 1, Turns: 1})
	succ, ok := axisByName(r, "success")
	if !ok {
		t.Fatal("success axis missing")
	}
	if succ.Present {
		t.Error("success axis must be pending (Present=false) until the judge lands")
	}
	// Composite must be the mean of the 3 present axes only, never dragged to 0 by pending success.
	var sum float64
	var n int
	for _, a := range r.Axes {
		if a.Present {
			sum += a.Score
			n++
		}
	}
	if n != 3 {
		t.Fatalf("expected 3 present axes, got %d", n)
	}
	approx(t, "composite excludes pending", r.Composite, round1(sum/3))
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

func TestSuccess_JudgeBlendsOntoDeterministic(t *testing.T) {
	j := 100.0
	s := Signals{Verification: "passed", Terminated: boolp(true), Success: &j} // deterministic base 85
	a, _ := axisByName(Score(s), "success")
	approx(t, "blended", a.Score, 0.65*85+0.35*100) // 90.25
}

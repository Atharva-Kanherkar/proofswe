package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/judge"
)

func runScore(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cfg := Config{Stdout: &out, Stderr: &out}
	err := runScoreCommand(cfg, args)
	return out.String(), err
}

func TestScoreCommand_Fixture(t *testing.T) {
	fixture := filepath.Join("testdata", "score", "session.jsonl")
	out, err := runScore(t, "--json", fixture)
	if err != nil {
		t.Fatalf("score: %v", err)
	}

	var got struct {
		Model     string  `json:"model"`
		Composite float64 `json:"composite"`
		Axes      []struct {
			Name    string `json:"name"`
			Present bool   `json:"present"`
		} `json:"axes"`
		Signals struct {
			ToolCalls  int `json:"tool_calls"`
			WebFetches int `json:"web_fetches"`
			ToolErrors int `json:"tool_errors"`
			Turns      int `json:"turns"`
			Edits      int `json:"edits"`
		} `json:"signals"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode score json: %v\n%s", err, out)
	}

	if got.Model != "claude-opus-4-7" {
		t.Errorf("model = %q, want claude-opus-4-7", got.Model)
	}
	for _, tc := range []struct {
		name      string
		got, want int
	}{
		{"tool_calls", got.Signals.ToolCalls, 3},
		{"web_fetches", got.Signals.WebFetches, 1},
		{"tool_errors", got.Signals.ToolErrors, 1},
		{"turns", got.Signals.Turns, 2},
		{"edits", got.Signals.Edits, 1},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}

	if got.Composite <= 0 || got.Composite > 100 {
		t.Errorf("composite = %.1f, want in (0,100]", got.Composite)
	}

	var successPresent bool
	for _, a := range got.Axes {
		if a.Name == "success" {
			successPresent = a.Present
		}
	}
	if !successPresent {
		t.Error("success axis should be present from deterministic signals (clean termination)")
	}
}

func TestScoreCommand_TextOutput(t *testing.T) {
	fixture := filepath.Join("testdata", "score", "session.jsonl")
	out, err := runScore(t, fixture)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	for _, want := range []string{"proofswe score", "claude-opus-4-7", "efficiency", "autonomy", "friction", "execution score"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n%s", want, out)
		}
	}
}

func TestScoreCommand_HTML(t *testing.T) {
	fixture := filepath.Join("testdata", "score", "session.jsonl")
	htmlPath := filepath.Join(t.TempDir(), "card.html")
	out, err := runScore(t, "--html", htmlPath, fixture)
	if err != nil {
		t.Fatalf("score --html: %v", err)
	}
	if !strings.Contains(out, "wrote "+htmlPath) {
		t.Errorf("expected wrote-notice for %s, got %q", htmlPath, out)
	}
}

func TestScoreCommand_Judge(t *testing.T) {
	// Swap in an offline fake judge so the success axis can be exercised without network.
	prev := newScoreJudge
	newScoreJudge = func(Config) (judge.Judge, error) {
		return judge.FakeJudge{V: judge.Verdict{Outcome: judge.OutcomeAccepted, Corrections: 1, Sentiment: 0.8}}, nil
	}
	t.Cleanup(func() { newScoreJudge = prev })

	fixture := filepath.Join("testdata", "score", "session.jsonl")
	out, err := runScore(t, "--judge", "--json", fixture)
	if err != nil {
		t.Fatalf("score --judge: %v", err)
	}

	var got struct {
		Axes []struct {
			Name    string  `json:"name"`
			Present bool    `json:"present"`
			Score   float64 `json:"score"`
		} `json:"axes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	var success *struct {
		Present bool
		Score   float64
	}
	for _, a := range got.Axes {
		if a.Name == "success" {
			success = &struct {
				Present bool
				Score   float64
			}{a.Present, a.Score}
		}
	}
	if success == nil || !success.Present {
		t.Fatalf("success axis should be present once judged; got %+v", got.Axes)
	}
	// blended: deterministic (no tests, clean end → 55) with judge (accepted,
	// 1 correction, +0.8 sentiment → 100): 0.65*55 + 0.35*100 ≈ 70.8.
	if success.Score < 60 || success.Score > 85 {
		t.Errorf("blended success = %.1f, want ~71", success.Score)
	}
}

func TestScoreCommand_Errors(t *testing.T) {
	if _, err := runScore(t); err == nil {
		t.Error("expected error with no transcript path")
	}
	empty := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := writeFileAtomic(empty, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runScore(t, empty); err == nil {
		t.Error("expected error for transcript with no scorable activity")
	}
}

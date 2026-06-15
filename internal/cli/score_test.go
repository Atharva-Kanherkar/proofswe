package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
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

	var success struct{ present bool }
	for _, a := range got.Axes {
		if a.Name == "success" {
			success.present = a.Present
		}
	}
	if success.present {
		t.Error("success axis must be pending until the judge lands")
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

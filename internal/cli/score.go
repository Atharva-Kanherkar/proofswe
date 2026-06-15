package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

// runScoreCommand scores a single captured transcript and prints a scorecard.
//
//	proofswe score <transcript.jsonl> [--harness=claudecode|codex] [--json] [--html out.html]
func runScoreCommand(cfg Config, args []string) error {
	flags := flag.NewFlagSet("score", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var harness, htmlPath string
	var asJSON bool
	flags.StringVar(&harness, "harness", "", "claudecode|codex (auto-detected if empty)")
	flags.BoolVar(&asJSON, "json", false, "emit the scorecard as JSON")
	flags.StringVar(&htmlPath, "html", "", "also write an HTML scorecard to this path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("%w: score requires exactly one transcript path", ErrUsage)
	}
	path := flags.Arg(0)

	if harness == "" {
		harness = detectHarness(path)
	}
	if harness != "claudecode" && harness != "codex" {
		return fmt.Errorf("%w: unknown harness %q", ErrUsage, harness)
	}

	// Hashes are not part of the score; an ephemeral salt keeps extraction
	// self-contained and deterministic without touching the proofswe state dir.
	events, err := parseTranscript(harness, []byte("proofswe-score"), path)
	if err != nil {
		return fmt.Errorf("parse transcript: %w", err)
	}

	sig := signalsFromEvents(events)
	if sig.ToolCalls == 0 && sig.Turns == 0 && sig.InputTokens == 0 {
		return fmt.Errorf("no scorable activity in %s (wrong harness, or empty transcript?)", path)
	}
	result := score.Score(sig)

	if htmlPath != "" {
		if err := os.WriteFile(htmlPath, []byte(renderScoreHTML(result)), 0o644); err != nil {
			return fmt.Errorf("write html: %w", err)
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "wrote %s\n", htmlPath)
	}
	if asJSON {
		return writeScoreJSON(cfg.Stdout, result, sig)
	}
	writeScoreText(cfg.Stdout, result)
	return nil
}

func signalsFromEvents(events []core.NormalizedEvent) score.Signals {
	var s score.Signals
	var minTS, maxTS time.Time
	for _, ev := range events {
		env := eventEnvelope(ev)
		if env.Model.ID != "" {
			s.Model = string(env.Model.ID)
		}
		// Metrics are placed once per assistant turn by the adapter, so summing
		// across all events does not double-count tokens.
		s.InputTokens += env.Metrics.InputTokens
		s.OutputTokens += env.Metrics.OutputTokens
		s.CacheTokens += env.Metrics.CacheCreationInputTokens + env.Metrics.CacheReadInputTokens
		if ts := env.Event.Timestamp; !ts.IsZero() {
			if minTS.IsZero() || ts.Before(minTS) {
				minTS = ts
			}
			if ts.After(maxTS) {
				maxTS = ts
			}
		}
		switch e := ev.(type) {
		case *core.UserPrompt:
			s.Turns++
		case *core.ToolCall:
			s.ToolCalls++
			if isWebTool(e.Name) {
				s.WebFetches++
			}
			if isEditTool(e.Name) {
				s.Edits++
			}
		case *core.ToolResult:
			if toolResultIsError(e) {
				s.ToolErrors++
			}
		}
	}
	if !minTS.IsZero() && maxTS.After(minTS) {
		s.DurationMS = maxTS.Sub(minTS).Milliseconds()
	}
	cost, est := score.EstimateCostUSD(s.Model, s.InputTokens, s.OutputTokens, s.CacheTokens)
	s.CostUSD = cost
	s.CostEstimated = est
	return s
}

// toolResultIsError flags a failed tool call from either the codex exit code or
// the claudecode `is_error` marker the adapter preserves in the sanitized result.
func toolResultIsError(r *core.ToolResult) bool {
	if r.ExitCode != nil && *r.ExitCode != 0 {
		return true
	}
	if len(r.Result) > 0 {
		var p struct {
			IsError bool `json:"is_error"`
		}
		if json.Unmarshal(r.Result, &p) == nil && p.IsError {
			return true
		}
	}
	return false
}

func isWebTool(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "webfetch") || strings.Contains(n, "websearch") ||
		strings.Contains(n, "web_search") || strings.Contains(n, "browser")
}

func isEditTool(name string) bool {
	switch strings.ToLower(name) {
	case "edit", "write", "multiedit", "str_replace", "str_replace_editor", "apply_patch", "create_file":
		return true
	default:
		return false
	}
}

// detectHarness peeks the head of the file: codex rollouts carry a "payload"
// envelope, claudecode transcripts do not.
func detectHarness(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "claudecode"
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 8192)
	n, _ := io.ReadFull(io.LimitReader(f, int64(len(buf))), buf)
	head := string(buf[:n])
	if strings.Contains(head, `"payload"`) || strings.Contains(head, `"turn_context"`) || strings.Contains(head, "rollout") {
		return "codex"
	}
	return "claudecode"
}

func writeScoreText(w io.Writer, r score.Result) {
	model := r.Model
	if model == "" {
		model = "(unknown model)"
	}
	_, _ = fmt.Fprintf(w, "\nproofswe score · %s\n\n", model)
	for _, a := range r.Axes {
		if !a.Present {
			_, _ = fmt.Fprintf(w, "  %-11s %-12s   %s\n", a.Name, "·· pending ··", a.Detail)
			continue
		}
		_, _ = fmt.Fprintf(w, "  %-11s %s %3.0f   %s\n", a.Name, bar(a.Score), a.Score, a.Detail)
	}
	_, _ = fmt.Fprintf(w, "\n  execution score: %.0f / 100   (provisional)\n\n", r.Composite)
}

func bar(scoreVal float64) string {
	filled := int(math.Round(scoreVal / 10))
	if filled < 0 {
		filled = 0
	}
	if filled > 10 {
		filled = 10
	}
	return strings.Repeat("▇", filled) + strings.Repeat("░", 10-filled)
}

func writeScoreJSON(w io.Writer, r score.Result, s score.Signals) error {
	out := struct {
		Model     string        `json:"model"`
		Composite float64       `json:"composite"`
		Axes      []score.Axis  `json:"axes"`
		Signals   score.Signals `json:"signals"`
		Note      string        `json:"note"`
	}{r.Model, r.Composite, r.Axes, s, r.Note}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func renderScoreHTML(r score.Result) string {
	var b strings.Builder
	model := r.Model
	if model == "" {
		model = "unknown model"
	}
	b.WriteString("<!doctype html><meta charset=utf-8><title>proofswe score</title>")
	b.WriteString(`<style>body{font:15px/1.6 -apple-system,system-ui,sans-serif;max-width:560px;margin:40px auto;padding:0 16px;color:#1a1a1a}` +
		`h1{font-size:20px;margin:0 0 2px}.bar{height:10px;border-radius:5px;background:#ececec;overflow:hidden}` +
		`.fill{height:100%;background:#1d9e75}.axis{margin:16px 0}.row{display:flex;justify-content:space-between;margin-bottom:5px}` +
		`.big{font-size:36px;font-weight:600;margin-top:18px}.muted{color:#888;font-size:13px}</style>`)
	_, _ = fmt.Fprintf(&b, "<h1>proofswe score</h1><p class=muted>%s</p>", html.EscapeString(model))
	for _, a := range r.Axes {
		if !a.Present {
			_, _ = fmt.Fprintf(&b, `<div class=axis><div class=row><span>%s</span><span class=muted>pending</span></div><div class=muted>%s</div></div>`,
				html.EscapeString(a.Name), html.EscapeString(a.Detail))
			continue
		}
		_, _ = fmt.Fprintf(&b, `<div class=axis><div class=row><span>%s</span><span>%.0f</span></div><div class=bar><div class=fill style="width:%.0f%%"></div></div><div class=muted>%s</div></div>`,
			html.EscapeString(a.Name), a.Score, a.Score, html.EscapeString(a.Detail))
	}
	_, _ = fmt.Fprintf(&b, `<div class=big>%.0f<span class=muted style="font-size:16px"> / 100</span></div><p class=muted>%s</p>`,
		r.Composite, html.EscapeString(r.Note))
	return b.String()
}

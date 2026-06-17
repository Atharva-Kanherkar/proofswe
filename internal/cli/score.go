package cli

import (
	"bufio"
	"bytes"
	"context"
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
	"github.com/Atharva-Kanherkar/proofswe/internal/judge"
	"github.com/Atharva-Kanherkar/proofswe/internal/reader"
	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

type judgeOptions struct {
	Provider string
	Model    string
}

type scoreMetadata struct {
	ScoreKind             string `json:"score_kind"`
	JudgeStatus           string `json:"judge_status"`
	OfficialScoreRequires string `json:"official_score_requires"`
}

// newScoreJudge builds the preview judge used by `score --local-judge`. It is a
// package var so tests can swap in a judge.FakeJudge and run offline.
var newScoreJudge = func(cfg Config, opts judgeOptions) (judge.Judge, error) {
	getenv := cfg.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	provider := strings.ToLower(strings.TrimSpace(firstNonEmpty(opts.Provider, getenv("PROOFSWE_JUDGE_PROVIDER"), "auto")))
	model := firstNonEmpty(opts.Model, getenv("PROOFSWE_JUDGE_MODEL"))
	switch provider {
	case "auto":
		if getenv("OPENAI_API_KEY") != "" {
			provider = "openai"
		} else {
			provider = "anthropic"
		}
	case "openai", "anthropic":
	default:
		return nil, fmt.Errorf("%w: unknown judge provider %q", ErrUsage, provider)
	}
	switch provider {
	case "openai":
		key := getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("set OPENAI_API_KEY to use --judge-provider=openai")
		}
		return judge.OpenAIJudge{APIKey: key, Model: firstNonEmpty(model, getenv("OPENAI_MODEL")), BaseURL: getenv("OPENAI_BASE_URL")}, nil
	case "anthropic":
		key := getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("set ANTHROPIC_API_KEY to use --judge-provider=anthropic")
		}
		return judge.AnthropicJudge{APIKey: key, Model: firstNonEmpty(model, getenv("ANTHROPIC_MODEL")), BaseURL: getenv("ANTHROPIC_BASE_URL")}, nil
	default:
		panic("unreachable judge provider")
	}
}

// runScoreCommand scores a single captured transcript and prints a scorecard.
//
//	proofswe score <transcript.jsonl> [--harness=claudecode|codex] [--json] [--html out.html]
func runScoreCommand(cfg Config, args []string) error {
	flags := flag.NewFlagSet("score", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var harness, htmlPath string
	var asJSON, localJudge, legacyJudge bool
	var judgeProvider, judgeModel, judgeMode string
	flags.StringVar(&harness, "harness", "", "claudecode|codex (auto-detected if empty)")
	flags.BoolVar(&asJSON, "json", false, "emit the scorecard as JSON")
	flags.StringVar(&htmlPath, "html", "", "also write an HTML scorecard to this path")
	flags.BoolVar(&localJudge, "local-judge", false, "run a local preview judge (not official benchmark scoring)")
	flags.BoolVar(&legacyJudge, "judge", false, "deprecated alias for --local-judge")
	flags.StringVar(&judgeMode, "judge-mode", "", "judge mode: local|none")
	flags.StringVar(&judgeProvider, "judge-provider", "", "judge provider: auto|openai|anthropic")
	flags.StringVar(&judgeModel, "judge-model", "", "judge model override")
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
	useLocalJudge, err := resolveLocalJudge(localJudge, legacyJudge, judgeMode, judgeProvider, judgeModel)
	if err != nil {
		return err
	}

	result, sig, meta, err := scoreTranscript(cfg, harness, path, useLocalJudge, judgeOptions{Provider: judgeProvider, Model: judgeModel})
	if err != nil {
		return err
	}

	if htmlPath != "" {
		if err := os.WriteFile(htmlPath, []byte(renderScoreHTML(result, meta)), 0o644); err != nil {
			return fmt.Errorf("write html: %w", err)
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "wrote %s\n", htmlPath)
	}
	if asJSON {
		return writeScoreJSON(cfg.Stdout, result, sig, meta)
	}
	writeScoreText(cfg.Stdout, result, meta)
	if sig.Extracted != nil && sig.Extracted.SkillAssisted {
		_, _ = fmt.Fprintf(cfg.Stdout, "  ⚠ skill-assisted: %s — model+skill; stratify, don't pool with unaided\n\n", strings.Join(sig.Extracted.SkillsUsed, ", "))
	}
	return nil
}

func resolveLocalJudge(localJudge, legacyJudge bool, judgeMode, judgeProvider, judgeModel string) (bool, error) {
	mode := strings.ToLower(strings.TrimSpace(judgeMode))
	switch mode {
	case "", "none":
	case "local":
		localJudge = true
	default:
		return false, fmt.Errorf("%w: unknown --judge-mode %q", ErrUsage, judgeMode)
	}
	if legacyJudge {
		localJudge = true
	}
	if !localJudge && (strings.TrimSpace(judgeProvider) != "" || strings.TrimSpace(judgeModel) != "") {
		return false, fmt.Errorf("%w: --judge-provider/--judge-model require --local-judge or --judge-mode=local", ErrUsage)
	}
	return localJudge, nil
}

// scoreTranscript turns a transcript into a scorecard plus the raw signals it
// was built from. Shared by `proofswe score` and `proofswe contribute` so both
// read the same deterministic axes (and, with useJudge, the same behavioral
// success axis). Hashes are not part of the score, so an ephemeral salt keeps
// extraction self-contained without touching the proofswe state dir.
func scoreTranscript(cfg Config, harness, path string, useLocalJudge bool, opts judgeOptions) (score.Result, score.Signals, scoreMetadata, error) {
	meta := scoreMetadata{
		ScoreKind:             "local_deterministic",
		JudgeStatus:           "not_run",
		OfficialScoreRequires: "server_judged_submission",
	}
	events, err := parseTranscript(harness, []byte("proofswe-score"), path)
	if err != nil {
		return score.Result{}, score.Signals{}, meta, fmt.Errorf("parse transcript: %w", err)
	}

	sig := signalsFromEvents(events)
	if sig.ToolCalls == 0 && sig.Turns == 0 && sig.InputTokens == 0 {
		return score.Result{}, score.Signals{}, meta, fmt.Errorf("no scorable activity in %s (wrong harness, or empty transcript?)", path)
	}

	// Deterministic success signals (objective-first): tests/build/lint passed,
	// committed/pushed/PR, clean termination, and benchmark signal evidence —
	// all read from the transcript.
	extracted := extractTranscriptSignals(harness, path)
	sig.Verification, sig.Landed, sig.Terminated = successFactsFromExtracted(extracted)
	sig.Extracted = &extracted
	sig.Turns = extracted.HumanTurns // friction uses human turns only — skill injections excluded

	if useLocalJudge {
		j, err := newScoreJudge(cfg, opts)
		if err != nil {
			return score.Result{}, score.Signals{}, meta, err
		}
		// The judge reads only the conversational turns, model identity stripped.
		if v, err := j.Assess(context.Background(), transcriptTurns(harness, path), extracted.SkillsUsed); err != nil {
			meta.JudgeStatus = "local_preview_failed"
			_, _ = fmt.Fprintf(cfg.Stderr, "local judge: %v (official judge still pending server evaluation)\n", err)
		} else {
			meta.ScoreKind = "local_with_judge_preview"
			meta.JudgeStatus = "local_preview"
			s := judge.ScoreSuccess(v)
			sig.Success = &s
			sig.SuccessLabel = judge.Label(v)
		}
	}

	return score.Score(sig), sig, meta, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

// transcriptTurns reads the raw transcript in order and returns the conversational
// turns (developer prompts + assistant text) for the judge. Tool-result records and
// tool-call blocks are skipped; the developer's reactions are what we judge.
func transcriptTurns(harness, path string) []judge.Turn {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var turns []judge.Turn
	_, _ = reader.ReadNewLines(f, 0, reader.Options{}, func(line []byte, _ int64) error {
		if t, ok := turnFromLine(harness, line); ok {
			turns = append(turns, t)
		}
		return nil
	})
	return turns
}

func turnFromLine(harness string, line []byte) (judge.Turn, bool) {
	var raw map[string]any
	if json.Unmarshal(line, &raw) != nil {
		return judge.Turn{}, false
	}
	switch harness {
	case "claudecode":
		typ, _ := raw["type"].(string)
		msg, _ := raw["message"].(map[string]any)
		switch typ {
		case "user":
			if len(toolResults(msg["content"])) > 0 {
				return judge.Turn{}, false // a tool result, not a developer prompt
			}
			text := contentText(msg["content"])
			if text == "" || detectSkill(text) != "" {
				return judge.Turn{}, false // empty, or a skill injection — not the developer's voice
			}
			return judge.Turn{Role: "user", Text: text}, true
		case "assistant":
			if text := contentText(msg["content"]); text != "" {
				return judge.Turn{Role: "assistant", Text: text}, true
			}
		}
	case "codex":
		if typ, _ := raw["type"].(string); typ == "response_item" {
			payload, _ := raw["payload"].(map[string]any)
			if itemType, _ := payload["type"].(string); itemType == "message" {
				role, _ := payload["role"].(string)
				text := contentText(payload["content"])
				if text != "" && (role == "user" || role == "assistant") {
					if role == "user" && detectSkill(text) != "" {
						return judge.Turn{}, false // skill injection, not the developer's voice
					}
					return judge.Turn{Role: role, Text: text}, true
				}
			}
		}
	}
	return judge.Turn{}, false
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

// detectHarness inspects the first records by PARSING them and checking top-level
// keys — never substring-matching raw text, which false-positives on prose (a
// developer's message can contain "rollout"/"payload"). Codex rollout records
// carry a top-level "payload" object; claudecode records carry a top-level "message".
func detectHarness(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "claudecode"
	}
	defer func() { _ = f.Close() }()
	br := bufio.NewReaderSize(f, 1<<20)
	for i := 0; i < 20; i++ {
		line, readErr := br.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			var m map[string]json.RawMessage
			if json.Unmarshal(trimmed, &m) == nil {
				if _, ok := m["payload"]; ok {
					return "codex"
				}
				if _, ok := m["message"]; ok {
					return "claudecode"
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	return "claudecode"
}

func writeScoreText(w io.Writer, r score.Result, meta scoreMetadata) {
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
	_, _ = fmt.Fprintf(w, "\n  session utility: %.0f / 100   confidence: %s\n", r.Utility.Score, r.Utility.Confidence)
	if r.Utility.JudgeNudge != 0 {
		_, _ = fmt.Fprintf(w, "  local judge nudge: %+0.1f (preview, capped)\n", r.Utility.JudgeNudge)
	}
	_, _ = fmt.Fprintf(w, "  score kind: %s\n", meta.ScoreKind)
	_, _ = fmt.Fprintf(w, "  judge status: %s\n", meta.JudgeStatus)
	_, _ = fmt.Fprintf(w, "  official judge pending server evaluation\n")
	_, _ = fmt.Fprintln(w)
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

func writeScoreJSON(w io.Writer, r score.Result, s score.Signals, meta scoreMetadata) error {
	out := struct {
		Model                 string        `json:"model"`
		Composite             float64       `json:"composite"`
		Utility               score.Utility `json:"utility"`
		ScoreKind             string        `json:"score_kind"`
		JudgeStatus           string        `json:"judge_status"`
		OfficialScoreRequires string        `json:"official_score_requires"`
		Axes                  []score.Axis  `json:"axes"`
		Signals               score.Signals `json:"signals"`
		Note                  string        `json:"note"`
	}{r.Model, r.Composite, r.Utility, meta.ScoreKind, meta.JudgeStatus, meta.OfficialScoreRequires, r.Axes, s, r.Note}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func renderScoreHTML(r score.Result, meta scoreMetadata) string {
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
	_, _ = fmt.Fprintf(&b, `<div class=big>%.0f<span class=muted style="font-size:16px"> / 100</span></div><p class=muted>session utility · confidence %s</p><p class=muted>%s</p>`,
		r.Utility.Score, html.EscapeString(r.Utility.Confidence), html.EscapeString(r.Note))
	_, _ = fmt.Fprintf(&b, `<p class=muted>score kind: %s · judge status: %s · official judge pending server evaluation</p>`,
		html.EscapeString(meta.ScoreKind), html.EscapeString(meta.JudgeStatus))
	return b.String()
}

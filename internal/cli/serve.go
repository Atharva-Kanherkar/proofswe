package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
	"github.com/Atharva-Kanherkar/proofswe/internal/judge"
	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

const httpShutdownTimeout = 5 * time.Second

var newServerJudge = func(cfg Config, opts judgeOptions) (judge.Judge, error) {
	return newScoreJudge(cfg, opts)
}

func runServeCommand(ctx context.Context, cfg Config, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var addr, judgeProvider, judgeModel string
	flags.StringVar(&addr, "addr", ":8080", "address to listen on")
	flags.StringVar(&judgeProvider, "judge-provider", "auto", "judge provider: auto|openai|anthropic")
	flags.StringVar(&judgeModel, "judge-model", "", "judge model override")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%w: serve takes no positional arguments", ErrUsage)
	}

	handler, err := newSubmissionHandler(cfg, judgeOptions{Provider: judgeProvider, Model: judgeModel})
	if err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: handler}
	errCh := make(chan error, 1)
	go func() {
		_, _ = fmt.Fprintf(cfg.Stdout, "proofswe judge server listening on %s\n", addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func newSubmissionHandler(cfg Config, opts judgeOptions) (http.Handler, error) {
	j, err := newServerJudge(cfg, opts)
	if err != nil {
		return nil, err
	}
	apiToken := strings.TrimSpace(getenvOrEmpty(cfg, "PROOFSWE_API_TOKEN"))
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("allow", http.MethodGet+", "+http.MethodHead)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`+"\n")
	})
	mux.HandleFunc("/v1/submissions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if apiToken != "" && r.Header.Get("authorization") != "Bearer "+apiToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req submitRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid submission json: "+err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := judgeSubmission(r.Context(), j, req)
		if err != nil {
			http.Error(w, "judge submission: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("content-type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp)
	})
	return mux, nil
}

func judgeSubmission(ctx context.Context, j judge.Judge, req submitRequest) (submitResponse, error) {
	task := req.Task
	verdict, err := j.Assess(ctx, taskJudgeTurns(task), task.Outcome.SkillsUsed)
	if err != nil {
		return submitResponse{}, err
	}
	success := judge.ScoreSuccess(verdict)
	signals := signalsFromSubmittedTask(task, success, judge.Label(verdict))
	result := score.Score(signals)
	return submitResponse{
		SubmissionID: shortTaskID(task.TaskID),
		TaskID:       task.TaskID,
		Status:       "judged",
		Judge:        submitJudge{Status: "server_judged", Model: serverJudgeModel(j), Version: "judge/1"},
		Scorecard:    scorecardForSubmit(result),
	}, nil
}

func signalsFromSubmittedTask(task corpus.Task, judgeSuccess float64, judgeLabel string) score.Signals {
	var terminated *bool
	switch task.Outcome.Termination {
	case "clean":
		v := true
		terminated = &v
	case "abandoned":
		v := false
		terminated = &v
	}
	extracted := score.ExtractedSignals{
		Verification:     task.Outcome.Verification,
		LandingQuality:   task.Outcome.LandingQuality,
		Termination:      task.Outcome.Termination,
		HumanTurns:       task.Outcome.HumanTurns,
		HumanCorrections: task.Outcome.HumanCorrections,
		HumanAcceptances: task.Outcome.HumanAcceptances,
		ReworkCount:      task.Outcome.ReworkCount,
		Interruptions:    task.Outcome.Interruptions,
		SkillsUsed:       task.Outcome.SkillsUsed,
		SkillAssisted:    task.Outcome.SkillAssisted,
		Scope: score.ScopeSignals{
			FilesTouched:     task.Outcome.FilesTouched,
			TestFilesTouched: task.Outcome.TestFilesTouched,
		},
	}
	return score.Signals{
		Model:        task.Model,
		ToolCalls:    len(task.Transcript.ToolCalls),
		Turns:        firstPositive(task.Outcome.HumanTurns, len(task.Prompts)),
		Edits:        len(task.Code.Files),
		Verification: task.Outcome.Verification,
		Landed:       task.Outcome.Landed,
		Terminated:   terminated,
		Success:      &judgeSuccess,
		SuccessLabel: judgeLabel,
		Extracted:    &extracted,
	}
}

func taskJudgeTurns(task corpus.Task) []judge.Turn {
	type orderedTurn struct {
		idx  int
		role string
		text string
		seq  int
	}
	var turns []orderedTurn
	seq := 0
	for _, p := range task.Prompts {
		turns = append(turns, orderedTurn{idx: p.TurnIndex, role: "user", text: p.Text, seq: seq})
		seq++
	}
	for _, m := range task.Transcript.AssistantMessages {
		turns = append(turns, orderedTurn{idx: m.TurnIndex, role: "assistant", text: m.Text, seq: seq})
		seq++
	}
	sort.SliceStable(turns, func(i, j int) bool {
		if turns[i].idx == turns[j].idx {
			return turns[i].seq < turns[j].seq
		}
		return turns[i].idx < turns[j].idx
	})
	out := make([]judge.Turn, 0, len(turns))
	for _, t := range turns {
		if strings.TrimSpace(t.text) != "" {
			out = append(out, judge.Turn{Role: t.role, Text: t.text})
		}
	}
	return out
}

func scorecardForSubmit(r score.Result) *submitScorecard {
	card := &submitScorecard{Composite: r.Composite, Utility: r.Utility, Note: r.Note}
	for _, axis := range r.Axes {
		if axis.Present {
			card.Axes = append(card.Axes, submitAxis{Name: axis.Name, Present: axis.Present, Score: axis.Score, Detail: axis.Detail})
		}
	}
	return card
}

func serverJudgeModel(j judge.Judge) string {
	switch v := j.(type) {
	case judge.OpenAIJudge:
		return firstNonEmpty(v.Model, judge.DefaultOpenAIModel)
	case judge.AnthropicJudge:
		return firstNonEmpty(v.Model, judge.DefaultAnthropicModel)
	default:
		return ""
	}
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

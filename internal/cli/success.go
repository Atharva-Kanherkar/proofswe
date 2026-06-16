package cli

import (
	"encoding/json"
	"os"
	"regexp"

	"github.com/Atharva-Kanherkar/proofswe/internal/reader"
)

// Deterministic success signals, read straight from the raw transcript — no LLM.
// We match the agent's own commands and their results: did it run tests/build/lint
// and pass, did it commit/push/open a PR, and did the session end cleanly.
var (
	verifyCmdRe     = regexp.MustCompile(`(?i)(go test|go build|go vet|gotestsum|pytest|py\.test|npm (run )?(test|build)|yarn (test|build)|pnpm (test|build)|jest|vitest|mocha|cargo (test|build|clippy)|make( |$)|tsc(\s|$)|eslint|ruff|flake8|mypy|golangci-lint|rubocop|phpunit|gradle (test|build)|mvn (test|verify)|dotnet test|ctest)`)
	landCmdRe       = regexp.MustCompile(`(?i)(git commit|git push|gh pr create)`)
	nonZeroExitRe   = regexp.MustCompile(`(?i)(exit status|exit code|exited with code) [1-9]`)
	zeroExitRe      = regexp.MustCompile(`(?i)(exit status|exit code|exited with code) 0`)
	failureSignalRe = regexp.MustCompile(`(?im)(^|\n)FAIL(\s|$)|(^|\n)FAILED(\s|$)|[1-9][0-9]*\s+(failed|failures?|failing)|Traceback|panic:|\berror:`)
)

type toolResultFact struct {
	isError bool
	text    string
}

type successScanState struct {
	verifyIDs        []string
	landIDs          []string
	results          map[string]toolResultFact
	pendingToolCalls map[string]struct{}
	prLinked         bool
	lastResultErr    *bool
	terminal         *bool
}

func newSuccessScanState() successScanState {
	return successScanState{
		results:          map[string]toolResultFact{},
		pendingToolCalls: map[string]struct{}{},
	}
}

// successFactsFromTranscript returns the deterministic success signals:
// verification ("passed"/"failed"/""), whether work landed (commit/push/PR), and
// the clean-vs-abandoned end (nil = unknown).
func successFactsFromTranscript(harness, path string) (verification string, landed bool, terminated *bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, nil
	}
	defer func() { _ = f.Close() }()

	state := newSuccessScanState()

	_, _ = reader.ReadNewLines(f, 0, reader.Options{}, func(line []byte, _ int64) error {
		var raw map[string]any
		if json.Unmarshal(line, &raw) != nil {
			return nil
		}
		switch harness {
		case "claudecode":
			scanClaudeSuccess(raw, &state)
		case "codex":
			scanCodexSuccess(raw, &state)
		}
		return nil
	})

	verification = verifyOutcome(state.verifyIDs, state.results)
	landed = state.prLinked || landOutcome(state.landIDs, state.results)
	switch {
	case state.terminal != nil:
		terminated = state.terminal
	case len(state.pendingToolCalls) > 0:
		clean := false
		terminated = &clean
	case state.lastResultErr != nil:
		clean := !*state.lastResultErr
		terminated = &clean
	}
	return verification, landed, terminated
}

func scanClaudeSuccess(raw map[string]any, state *successScanState) {
	switch typ, _ := raw["type"].(string); typ {
	case "pr-link":
		state.prLinked = true
		return
	case "result":
		if sub, _ := raw["subtype"].(string); sub != "" {
			clean := sub == "success"
			state.terminal = &clean
		}
		return
	}
	msg, _ := raw["message"].(map[string]any)
	items, _ := msg["content"].([]any)
	for _, item := range items {
		block, _ := item.(map[string]any)
		switch block["type"] {
		case "tool_use":
			id, _ := block["id"].(string)
			cmd, _ := block["name"].(string)
			cmd += " " + stringifyJSON(block["input"])
			if id != "" {
				state.pendingToolCalls[id] = struct{}{}
			}
			if verifyCmdRe.MatchString(cmd) {
				state.verifyIDs = append(state.verifyIDs, id)
			}
			if landCmdRe.MatchString(cmd) {
				state.landIDs = append(state.landIDs, id)
			}
		case "tool_result":
			id, _ := block["tool_use_id"].(string)
			isErr, _ := block["is_error"].(bool)
			state.results[id] = toolResultFact{isError: isErr, text: contentText(block["content"])}
			delete(state.pendingToolCalls, id)
			e := isErr
			state.lastResultErr = &e
		}
	}
}

func scanCodexSuccess(raw map[string]any, state *successScanState) {
	if typ, _ := raw["type"].(string); typ != "response_item" {
		return
	}
	payload, _ := raw["payload"].(map[string]any)
	switch payload["type"] {
	case "function_call", "custom_tool_call", "web_search_call", "tool_search_call":
		id, _ := payload["call_id"].(string)
		cmd, _ := payload["name"].(string)
		cmd += " " + stringifyJSON(payload["arguments"])
		if id != "" {
			state.pendingToolCalls[id] = struct{}{}
		}
		if verifyCmdRe.MatchString(cmd) {
			state.verifyIDs = append(state.verifyIDs, id)
		}
		if landCmdRe.MatchString(cmd) {
			state.landIDs = append(state.landIDs, id)
		}
	case "function_call_output", "custom_tool_call_output", "web_search_output", "tool_search_output":
		id, _ := payload["call_id"].(string)
		text := stringifyJSON(payload["output"])
		isErr := toolOutputFailed(text)
		state.results[id] = toolResultFact{isError: isErr, text: text}
		delete(state.pendingToolCalls, id)
		e := isErr
		state.lastResultErr = &e
	}
}

// verifyOutcome reports the final verification state: the LAST verification
// command's result (agents iterate fix→test→fix→test, so the final run is what counts).
func verifyOutcome(verifyIDs []string, results map[string]toolResultFact) string {
	if len(verifyIDs) == 0 {
		return ""
	}
	last := verifyIDs[len(verifyIDs)-1]
	r, ok := results[last]
	if !ok {
		return "" // ran but no captured result → unknown
	}
	if r.isError || toolOutputFailed(r.text) {
		return "failed"
	}
	return "passed"
}

func landOutcome(landIDs []string, results map[string]toolResultFact) bool {
	for _, id := range landIDs {
		r, ok := results[id]
		if ok && !r.isError && !toolOutputFailed(r.text) {
			return true
		}
	}
	return false
}

func toolOutputFailed(text string) bool {
	if nonZeroExitRe.MatchString(text) {
		return true
	}
	if zeroExitRe.MatchString(text) {
		return false
	}
	return failureSignalRe.MatchString(text)
}

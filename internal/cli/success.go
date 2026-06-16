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
	verifyCmdRe = regexp.MustCompile(`(?i)(go test|go build|go vet|gotestsum|pytest|py\.test|npm (run )?(test|build)|yarn (test|build)|pnpm (test|build)|jest|vitest|mocha|cargo (test|build|clippy)|make( |$)|tsc(\s|$)|eslint|ruff|flake8|mypy|golangci-lint|rubocop|phpunit|gradle (test|build)|mvn (test|verify)|dotnet test|ctest)`)
	landCmdRe   = regexp.MustCompile(`(?i)(git commit|git push|gh pr create)`)
	failureRe   = regexp.MustCompile(`(?i)(\bFAIL\b|FAILED|failing|tests? failed|exit status [1-9]|exit code [1-9]|Traceback|panic:|\berror:)`)
)

type toolResultFact struct {
	isError bool
	text    string
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

	var verifyIDs []string
	results := map[string]toolResultFact{}
	var lastResultErr *bool
	var terminal *bool

	_, _ = reader.ReadNewLines(f, 0, reader.Options{}, func(line []byte, _ int64) error {
		var raw map[string]any
		if json.Unmarshal(line, &raw) != nil {
			return nil
		}
		switch harness {
		case "claudecode":
			scanClaudeSuccess(raw, &verifyIDs, results, &landed, &lastResultErr, &terminal)
		case "codex":
			scanCodexSuccess(raw, &verifyIDs, results, &landed, &lastResultErr)
		}
		return nil
	})

	verification = verifyOutcome(verifyIDs, results)
	switch {
	case terminal != nil:
		terminated = terminal
	case lastResultErr != nil:
		clean := !*lastResultErr
		terminated = &clean
	}
	return verification, landed, terminated
}

func scanClaudeSuccess(raw map[string]any, verifyIDs *[]string, results map[string]toolResultFact, landed *bool, lastResultErr **bool, terminal **bool) {
	switch typ, _ := raw["type"].(string); typ {
	case "pr-link":
		*landed = true
		return
	case "result":
		if sub, _ := raw["subtype"].(string); sub != "" {
			clean := sub == "success"
			*terminal = &clean
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
			if verifyCmdRe.MatchString(cmd) {
				*verifyIDs = append(*verifyIDs, id)
			}
			if landCmdRe.MatchString(cmd) {
				*landed = true
			}
		case "tool_result":
			id, _ := block["tool_use_id"].(string)
			isErr, _ := block["is_error"].(bool)
			results[id] = toolResultFact{isError: isErr, text: contentText(block["content"])}
			e := isErr
			*lastResultErr = &e
		}
	}
}

func scanCodexSuccess(raw map[string]any, verifyIDs *[]string, results map[string]toolResultFact, landed *bool, lastResultErr **bool) {
	if typ, _ := raw["type"].(string); typ != "response_item" {
		return
	}
	payload, _ := raw["payload"].(map[string]any)
	switch payload["type"] {
	case "function_call":
		id, _ := payload["call_id"].(string)
		cmd, _ := payload["name"].(string)
		cmd += " " + stringifyJSON(payload["arguments"])
		if verifyCmdRe.MatchString(cmd) {
			*verifyIDs = append(*verifyIDs, id)
		}
		if landCmdRe.MatchString(cmd) {
			*landed = true
		}
	case "function_call_output":
		id, _ := payload["call_id"].(string)
		text := stringifyJSON(payload["output"])
		isErr := failureRe.MatchString(text)
		results[id] = toolResultFact{isError: isErr, text: text}
		e := isErr
		*lastResultErr = &e
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
	if r.isError || failureRe.MatchString(r.text) {
		return "failed"
	}
	return "passed"
}

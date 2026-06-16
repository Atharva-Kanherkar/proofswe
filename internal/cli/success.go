package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Atharva-Kanherkar/proofswe/internal/reader"
	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

// Deterministic success signals, read straight from the raw transcript — no LLM.
// We match the agent's own commands and their results: did it run tests/build/lint
// and pass, did it commit/push/open a PR, and did the session end cleanly.
var (
	verifyCmdRe       = regexp.MustCompile(`(?i)(go test|go build|go vet|gotestsum|pytest|py\.test|npm (run )?(test|build)|yarn (test|build)|pnpm (test|build)|jest|vitest|mocha|cargo (test|build|clippy)|make( |$)|tsc(\s|$)|eslint|ruff|flake8|mypy|golangci-lint|rubocop|phpunit|gradle (test|build)|mvn (test|verify)|dotnet test|ctest)`)
	landCmdRe         = regexp.MustCompile(`(?i)(git commit|git push|gh pr create)`)
	nonZeroExitRe     = regexp.MustCompile(`(?i)(exit status|exit code|exited with code) [1-9]`)
	zeroExitRe        = regexp.MustCompile(`(?i)(exit status|exit code|exited with code) 0`)
	failureSignalRe   = regexp.MustCompile(`(?im)(^|\n)FAIL(\s|$)|(^|\n)FAILED(\s|$)|[1-9][0-9]*\s+(failed|failures?|failing)|Traceback|panic:|\berror:`)
	diffHunkRe        = regexp.MustCompile(`(?m)^@@ `)
	patchFileRe       = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$`)
	correctionRe      = regexp.MustCompile(`(?i)\b(wrong|try again|fix|failed|failing|error|broken|revert|undo|not what|doesn'?t|didn'?t|actually)\b`)
	acceptanceRe      = regexp.MustCompile(`(?i)\b(lgtm|looks good|works|thank|thanks|great|perfect|ship it|nice|done)\b`)
	editShellSignalRe = regexp.MustCompile(`(?i)\b(apply_patch|str_replace|cat >|tee .*>|python .*write_text|go fmt|gofmt -w|prettier --write)\b`)
	// skillRe detects a Claude Code skill injection ("Base directory for this
	// skill: …/.claude/skills/<name>"). Skill content is human-authored scaffolding,
	// not the developer's own voice.
	skillRe = regexp.MustCompile(`(?i)base directory for this skill:\s*[^\r\n]*[\\/]skills[\\/]([A-Za-z0-9._-]+)\b`)
	// interruptRe detects the marker Claude Code records when the developer cuts
	// the agent off mid-action — a frustration signal, not a typed prompt.
	interruptRe = regexp.MustCompile(`(?i)\[request interrupted by user`)
)

type toolResultFact struct {
	isError bool
	text    string
	offset  int64
}

type verifyAttempt struct {
	id        string
	afterEdit bool
}

type successScanState struct {
	verifyIDs        []string
	verifyAttempts   []verifyAttempt
	landIDs          []string
	results          map[string]toolResultFact
	pendingToolCalls map[string]struct{}
	editCallIDs      map[string]struct{}
	editFiles        map[string]int
	editFileOffsets  map[string][]int64
	evidence         []score.SignalEvidence

	prLinked        bool
	prLinkOffset    int64
	lastResultErr   *bool
	lastResultOff   int64
	lastCallID      string
	lastCallPending bool
	terminal        *bool
	terminalOffset  int64
	seenAssistant   bool
	sawEdit         bool
	editCount       int
	diffHunks       int
	humanTurns      int
	interruptions   int
	skills          []string
	skillSet        map[string]bool
}

func newSuccessScanState() successScanState {
	return successScanState{
		results:          map[string]toolResultFact{},
		pendingToolCalls: map[string]struct{}{},
		editCallIDs:      map[string]struct{}{},
		editFiles:        map[string]int{},
		editFileOffsets:  map[string][]int64{},
		skillSet:         map[string]bool{},
	}
}

// successFactsFromTranscript returns the deterministic success signals:
// verification ("passed"/"failed"/""), whether work landed (commit/push/PR), and
// the clean-vs-abandoned end (nil = unknown).
func successFactsFromTranscript(harness, path string) (verification string, landed bool, terminated *bool) {
	return successFactsFromExtracted(extractTranscriptSignals(harness, path))
}

func successFactsFromExtracted(extracted score.ExtractedSignals) (verification string, landed bool, terminated *bool) {
	verification = extracted.Verification
	landed = extracted.LandingQuality == "succeeded" || extracted.LandingQuality == "pr_link"
	switch extracted.Termination {
	case "clean":
		clean := true
		terminated = &clean
	case "abandoned":
		clean := false
		terminated = &clean
	}
	return verification, landed, terminated
}

func extractTranscriptSignals(harness, path string) score.ExtractedSignals {
	f, err := os.Open(path)
	if err != nil {
		return score.ExtractedSignals{Version: score.ExtractedSignalsVersion, LandingQuality: "none", Termination: "unknown"}
	}
	defer func() { _ = f.Close() }()

	state := newSuccessScanState()
	_, _ = reader.ReadNewLines(f, 0, reader.Options{}, func(line []byte, offset int64) error {
		var raw map[string]any
		if json.Unmarshal(line, &raw) != nil {
			return nil
		}
		switch harness {
		case "claudecode":
			scanClaudeSuccess(raw, offset, &state)
		case "codex":
			scanCodexSuccess(raw, offset, &state)
		}
		return nil
	})
	return finalizeExtractedSignals(state)
}

func scanClaudeSuccess(raw map[string]any, offset int64, state *successScanState) {
	switch typ, _ := raw["type"].(string); typ {
	case "pr-link":
		state.lastCallPending = false
		state.prLinked = true
		state.prLinkOffset = offset
		state.addEvidence("landing_quality", "pr_link", offset, "pr-link record")
		return
	case "result":
		state.lastCallPending = false
		if sub, _ := raw["subtype"].(string); sub != "" {
			clean := sub == "success"
			state.terminal = &clean
			state.terminalOffset = offset
		}
		return
	}

	msg, _ := raw["message"].(map[string]any)
	items, _ := msg["content"].([]any)
	if msg != nil {
		state.lastCallPending = false
	}
	if typ, _ := raw["type"].(string); typ == "user" && len(toolResults(msg["content"])) == 0 {
		state.recordUserMessage(contentText(msg["content"]), offset)
	}
	if typ, _ := raw["type"].(string); typ == "assistant" {
		state.seenAssistant = true
	}
	for _, item := range items {
		block, _ := item.(map[string]any)
		switch block["type"] {
		case "tool_use":
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			input := block["input"]
			state.recordToolCall(id, name, input, offset)
		case "tool_result":
			id, _ := block["tool_use_id"].(string)
			isErr, _ := block["is_error"].(bool)
			text := contentText(block["content"])
			state.recordToolResult(id, isErr, text, offset)
		}
	}
}

func scanCodexSuccess(raw map[string]any, offset int64, state *successScanState) {
	if typ, _ := raw["type"].(string); typ != "response_item" {
		return
	}
	payload, _ := raw["payload"].(map[string]any)
	switch payload["type"] {
	case "message":
		state.lastCallPending = false
		role, _ := payload["role"].(string)
		text := contentText(payload["content"])
		if role == "assistant" {
			state.seenAssistant = true
		}
		if role == "user" {
			state.recordUserMessage(text, offset)
		}
	case "function_call", "custom_tool_call", "web_search_call", "tool_search_call":
		id, _ := payload["call_id"].(string)
		name, _ := payload["name"].(string)
		args := payload["arguments"]
		state.recordToolCall(id, name, args, offset)
	case "function_call_output", "custom_tool_call_output", "web_search_output", "tool_search_output":
		id, _ := payload["call_id"].(string)
		text := stringifyJSON(payload["output"])
		state.recordToolResult(id, toolOutputFailed(text), text, offset)
	}
}

func (s *successScanState) recordToolCall(id, name string, input any, offset int64) {
	if id != "" {
		s.pendingToolCalls[id] = struct{}{}
		s.lastCallID = id
		s.lastCallPending = true
	}
	command, isExec := executedCommand(name, input)
	if isExec {
		if verifyCmdRe.MatchString(command) {
			s.verifyIDs = append(s.verifyIDs, id)
			s.verifyAttempts = append(s.verifyAttempts, verifyAttempt{id: id, afterEdit: s.sawEdit})
			s.addEvidence("verification", "ran", offset, name)
		}
		if landCmdRe.MatchString(command) {
			s.landIDs = append(s.landIDs, id)
			s.addEvidence("landing_quality", "attempted", offset, name)
		}
	}
	if isEditTool(name) || (isExec && editShellSignalRe.MatchString(command)) {
		s.recordEdit(id, input, command, offset)
	}
}

func executedCommand(name string, input any) (string, bool) {
	if !isExecTool(name) {
		return "", false
	}
	command := commandField(input)
	if command == "" {
		return "", false
	}
	return command, true
}

func isExecTool(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "bash", "shell", "sh", "zsh", "powershell", "exec", "exec_command", "run_command", "command":
		return true
	default:
		return strings.HasSuffix(n, ".exec_command") || strings.HasSuffix(n, ".run_command")
	}
}

func commandField(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"command", "cmd", "script"} {
			if text, _ := v[key].(string); strings.TrimSpace(text) != "" {
				return text
			}
		}
	case string:
		var decoded any
		if json.Unmarshal([]byte(v), &decoded) == nil {
			return commandField(decoded)
		}
	}
	return ""
}

func (s *successScanState) recordToolResult(id string, isErr bool, text string, offset int64) {
	s.results[id] = toolResultFact{isError: isErr, text: text, offset: offset}
	delete(s.pendingToolCalls, id)
	s.lastCallPending = false
	if _, ok := s.editCallIDs[id]; ok {
		s.diffHunks += countDiffHunks(text)
	}
	e := isErr
	s.lastResultErr = &e
	s.lastResultOff = offset
}

func (s *successScanState) recordEdit(id string, input any, command string, offset int64) {
	s.sawEdit = true
	s.editCount++
	if id != "" {
		s.editCallIDs[id] = struct{}{}
	}
	files := extractFilePaths(input)
	if len(files) == 0 {
		files = extractFilePaths(command)
	}
	if len(files) == 0 {
		s.addEvidence("scope", "edit", offset, "file unknown")
		return
	}
	for _, file := range files {
		s.editFiles[file]++
		s.editFileOffsets[file] = append(s.editFileOffsets[file], offset)
		s.addEvidence("scope", "edit", offset, "file")
	}
}

// recordUserMessage classifies a user message: a skill injection is recorded as
// a skill (context, not the developer's voice) and excluded from human turns and
// reactions; a real prompt counts as a human turn and is scanned for corrections.
func (s *successScanState) recordUserMessage(text string, offset int64) {
	if skill := detectSkill(text); skill != "" {
		s.recordSkill(skill, offset)
		return
	}
	if interruptRe.MatchString(text) {
		s.interruptions++
		s.addEvidence("interruption", "true", offset, "developer interrupted the agent")
		return
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	s.humanTurns++
	s.scanUserReaction(text, offset)
}

func detectSkill(text string) string {
	if m := skillRe.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	return ""
}

func (s *successScanState) recordSkill(name string, offset int64) {
	if s.skillSet[name] {
		return
	}
	s.skillSet[name] = true
	s.skills = append(s.skills, name)
	s.addEvidence("skill_used", name, offset, "skill injection")
}

func (s *successScanState) scanUserReaction(text string, offset int64) {
	if !s.seenAssistant || strings.TrimSpace(text) == "" {
		return
	}
	if correctionRe.MatchString(text) {
		s.addEvidence("human_correction", "true", offset, "matched correction phrase")
		return
	}
	if acceptanceRe.MatchString(text) {
		s.addEvidence("human_acceptance", "true", offset, "matched acceptance phrase")
	}
}

func (s *successScanState) addEvidence(signal, value string, offset int64, detail string) {
	s.evidence = append(s.evidence, score.SignalEvidence{Signal: signal, Value: value, Offset: offset, Detail: detail})
}

func finalizeExtractedSignals(state successScanState) score.ExtractedSignals {
	verification := verifyOutcome(state.verifyIDs, state.results)
	landingQuality, landingOffset := landingQuality(state)
	termination, terminationOffset := terminationQuality(state)
	rework := reworkCount(state.editFiles)
	verifiedAfterEdit, verifiedAfterEditOffset := verifiedAfterEdit(state.verifyAttempts, state.results)
	scope := score.ScopeSignals{
		FilesTouched:     len(state.editFiles),
		TestFilesTouched: testFilesTouched(state.editFiles),
		EditCount:        state.editCount,
		DiffHunks:        state.diffHunks,
	}

	out := score.ExtractedSignals{
		Version:           score.ExtractedSignalsVersion,
		Verification:      verification,
		LandingQuality:    landingQuality,
		Termination:       termination,
		HumanTurns:        state.humanTurns,
		Interruptions:     state.interruptions,
		HumanCorrections:  evidenceCount(state.evidence, "human_correction"),
		HumanAcceptances:  evidenceCount(state.evidence, "human_acceptance"),
		ReworkCount:       rework,
		VerifiedAfterEdit: verifiedAfterEdit,
		SkillsUsed:        append([]string(nil), state.skills...),
		SkillAssisted:     len(state.skills) > 0,
		Scope:             scope,
		Evidence:          append([]score.SignalEvidence(nil), state.evidence...),
	}
	if verification != "" {
		if r, ok := state.results[lastString(state.verifyIDs)]; ok {
			out.Evidence = append(out.Evidence, score.SignalEvidence{Signal: "verification", Value: verification, Offset: r.offset, Detail: "last verification result"})
		}
	}
	if landingQuality != "none" {
		out.Evidence = append(out.Evidence, score.SignalEvidence{Signal: "landing_quality", Value: landingQuality, Offset: landingOffset})
	}
	if termination != "unknown" {
		out.Evidence = append(out.Evidence, score.SignalEvidence{Signal: "termination", Value: termination, Offset: terminationOffset})
	}
	if rework > 0 {
		out.Evidence = append(out.Evidence, score.SignalEvidence{Signal: "rework", Value: "true", Offset: reworkEvidenceOffset(state.editFileOffsets), Detail: "same file edited repeatedly"})
	}
	if verifiedAfterEdit {
		out.Evidence = append(out.Evidence, score.SignalEvidence{Signal: "verified_after_edit", Value: "true", Offset: verifiedAfterEditOffset})
	}
	return out
}

// verifyOutcome reports the final verification state: the LAST verification
// command's result (agents iterate fix->test->fix->test, so the final run is what counts).
func verifyOutcome(verifyIDs []string, results map[string]toolResultFact) string {
	if len(verifyIDs) == 0 {
		return ""
	}
	last := verifyIDs[len(verifyIDs)-1]
	r, ok := results[last]
	if !ok {
		return "" // ran but no captured result -> unknown
	}
	if r.isError || toolOutputFailed(r.text) {
		return "failed"
	}
	return "passed"
}

func landingQuality(s successScanState) (string, int64) {
	if s.prLinked {
		return "pr_link", s.prLinkOffset
	}
	if len(s.landIDs) == 0 {
		return "none", 0
	}
	var sawUnknown bool
	var firstOffset int64
	for _, id := range s.landIDs {
		r, ok := s.results[id]
		if !ok {
			sawUnknown = true
			continue
		}
		if firstOffset == 0 {
			firstOffset = r.offset
		}
		if !r.isError && !toolOutputFailed(r.text) {
			return "succeeded", r.offset
		}
	}
	if sawUnknown {
		return "attempted_unknown", firstOffset
	}
	return "failed", firstOffset
}

func terminationQuality(s successScanState) (string, int64) {
	if s.terminal != nil {
		if *s.terminal {
			return "clean", s.terminalOffset
		}
		return "abandoned", s.terminalOffset
	}
	if s.lastCallPending && s.lastCallID != "" {
		if _, ok := s.pendingToolCalls[s.lastCallID]; ok {
			return "abandoned", s.lastResultOff
		}
	}
	switch {
	case s.lastResultErr != nil && *s.lastResultErr:
		return "abandoned", s.lastResultOff
	case s.lastResultErr != nil:
		return "clean", s.lastResultOff
	default:
		return "unknown", 0
	}
}

func verifiedAfterEdit(attempts []verifyAttempt, results map[string]toolResultFact) (bool, int64) {
	for _, attempt := range attempts {
		if !attempt.afterEdit {
			continue
		}
		r, ok := results[attempt.id]
		if ok && !r.isError && !toolOutputFailed(r.text) {
			return true, r.offset
		}
	}
	return false, 0
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

func extractFilePaths(value any) []string {
	seen := map[string]bool{}
	var out []string
	add := func(path string) {
		path = cleanEvidencePath(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		out = append(out, path)
	}
	var walk func(any, string)
	walk = func(v any, key string) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				walk(child, strings.ToLower(k))
			}
		case []any:
			for _, child := range x {
				walk(child, key)
			}
		case string:
			switch key {
			case "file", "path", "file_path", "filename":
				add(x)
			}
			for _, match := range patchFileRe.FindAllStringSubmatch(x, -1) {
				if len(match) > 1 {
					add(match[1])
				}
			}
		}
	}
	walk(value, "")
	return out
}

func cleanEvidencePath(path string) string {
	path = strings.Trim(path, " \t\r\n\"'")
	if path == "" || strings.Contains(path, "://") {
		return ""
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return filepath.Clean(path)
}

func countDiffHunks(text string) int {
	return len(diffHunkRe.FindAllStringIndex(text, -1))
}

func reworkCount(files map[string]int) int {
	var n int
	for _, edits := range files {
		if edits > 1 {
			n += edits - 1
		}
	}
	return n
}

func reworkEvidenceOffset(files map[string][]int64) int64 {
	for _, offsets := range files {
		if len(offsets) > 1 {
			return offsets[1]
		}
	}
	return 0
}

func testFilesTouched(files map[string]int) int {
	var n int
	for file := range files {
		lower := strings.ToLower(file)
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") {
			n++
		}
	}
	return n
}

func evidenceCount(evidence []score.SignalEvidence, signal string) int {
	var n int
	for _, ev := range evidence {
		if ev.Signal == signal {
			n++
		}
	}
	return n
}

func lastString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

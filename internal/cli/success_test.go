package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

func TestSuccessFacts_Verified(t *testing.T) {
	fixture := filepath.Join("testdata", "score", "verified.jsonl")
	ver, landed, term := successFactsFromTranscript("claudecode", fixture)
	if ver != "passed" {
		t.Errorf("verification = %q, want passed", ver)
	}
	if !landed {
		t.Error("landed = false, want true (git commit ran)")
	}
	if term == nil || !*term {
		t.Errorf("terminated = %v, want clean (true)", term)
	}
}

func TestSuccessFacts_PlainSession(t *testing.T) {
	// The base fixture runs no tests, no commit, ends with result:success.
	fixture := filepath.Join("testdata", "score", "session.jsonl")
	ver, landed, term := successFactsFromTranscript("claudecode", fixture)
	if ver != "" {
		t.Errorf("verification = %q, want \"\" (no test command)", ver)
	}
	if landed {
		t.Error("landed = true, want false (no commit/PR)")
	}
	if term == nil || !*term {
		t.Errorf("terminated = %v, want clean (true)", term)
	}
}

func TestSuccessFacts_LandRequiresSuccessfulCommand(t *testing.T) {
	fixture := writeTranscript(t,
		`{"type":"started","uuid":"s","sessionId":"failed-land","timestamp":"2026-06-01T00:00:00Z"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"failed-land","timestamp":"2026-06-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"Bash","input":{"command":"git commit -am 'ship it'"}}]}}`,
		`{"type":"user","uuid":"r1","sessionId":"failed-land","timestamp":"2026-06-01T00:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","is_error":true,"content":"nothing to commit, working tree clean"}]}}`,
	)

	_, landed, term := successFactsFromTranscript("claudecode", fixture)
	if landed {
		t.Error("landed = true, want false when the commit command failed")
	}
	if term == nil || *term {
		t.Errorf("terminated = %v, want abandoned (false) after the final failed tool result", term)
	}
}

func TestSuccessFacts_PendingToolCallIsAbandoned(t *testing.T) {
	fixture := writeTranscript(t,
		`{"type":"started","uuid":"s","sessionId":"pending-tool","timestamp":"2026-06-01T00:00:00Z"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"pending-tool","timestamp":"2026-06-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"v1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
	)

	ver, landed, term := successFactsFromTranscript("claudecode", fixture)
	if ver != "" {
		t.Errorf("verification = %q, want unknown when the test result is missing", ver)
	}
	if landed {
		t.Error("landed = true, want false")
	}
	if term == nil || *term {
		t.Errorf("terminated = %v, want abandoned (false) with an outstanding tool call", term)
	}
}

func TestSuccessFacts_CodexVerifiedAndLanded(t *testing.T) {
	fixture := writeTranscript(t,
		`{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"v1","arguments":"{\"cmd\":\"go test ./...\"}"}}`,
		`{"timestamp":"2026-06-01T00:00:01Z","type":"response_item","payload":{"type":"function_call_output","call_id":"v1","output":"Chunk ID: test\nProcess exited with code 0\nOutput:\nok github.com/acme/app 0.1s"}}`,
		`{"timestamp":"2026-06-01T00:00:02Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"c1","arguments":"{\"cmd\":\"git commit -am success\"}"}}`,
		`{"timestamp":"2026-06-01T00:00:03Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"Chunk ID: commit\nProcess exited with code 0\nOutput:\n[main abc123] success"}}`,
	)

	ver, landed, term := successFactsFromTranscript("codex", fixture)
	if ver != "passed" {
		t.Errorf("verification = %q, want passed", ver)
	}
	if !landed {
		t.Error("landed = false, want true after successful commit")
	}
	if term == nil || !*term {
		t.Errorf("terminated = %v, want clean (true)", term)
	}
}

func TestSuccessFacts_CodexFailedCommitDoesNotLand(t *testing.T) {
	fixture := writeTranscript(t,
		`{"timestamp":"2026-06-01T00:00:00Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"c1","arguments":"{\"cmd\":\"git commit -am nope\"}"}}`,
		`{"timestamp":"2026-06-01T00:00:01Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"Chunk ID: commit\nProcess exited with code 1\nOutput:\nnothing to commit"}}`,
	)

	_, landed, term := successFactsFromTranscript("codex", fixture)
	if landed {
		t.Error("landed = true, want false when Codex commit output has exit code 1")
	}
	if term == nil || *term {
		t.Errorf("terminated = %v, want abandoned (false)", term)
	}
}

func TestExtractTranscriptSignals_VerifiedAfterEditAndScope(t *testing.T) {
	fixture := writeTranscript(t,
		`{"type":"started","uuid":"s","sessionId":"scope","timestamp":"2026-06-01T00:00:00Z"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"scope","timestamp":"2026-06-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"e1","name":"Edit","input":{"file":"internal/parser/parser_test.go","old_string":"old","new_string":"new"}}]}}`,
		`{"type":"user","uuid":"r1","sessionId":"scope","timestamp":"2026-06-01T00:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"e1","is_error":false,"content":"@@ -1 +1 @@\n-old\n+new"}]}}`,
		`{"type":"assistant","uuid":"a2","sessionId":"scope","timestamp":"2026-06-01T00:00:03Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"v1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","uuid":"r2","sessionId":"scope","timestamp":"2026-06-01T00:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"v1","is_error":false,"content":"ok github.com/acme/app 0.1s"}]}}`,
		`{"type":"result","uuid":"res","sessionId":"scope","timestamp":"2026-06-01T00:00:05Z","subtype":"success"}`,
	)

	got := extractTranscriptSignals("claudecode", fixture)
	if got.Version != 1 {
		t.Fatalf("version = %d, want 1", got.Version)
	}
	if got.Verification != "passed" || !got.VerifiedAfterEdit {
		t.Fatalf("verification = %q, verified_after_edit = %t", got.Verification, got.VerifiedAfterEdit)
	}
	if got.Scope.FilesTouched != 1 || got.Scope.TestFilesTouched != 1 || got.Scope.EditCount != 1 || got.Scope.DiffHunks != 1 {
		t.Fatalf("scope = %+v, want one touched test file/edit/hunk", got.Scope)
	}
	if !hasEvidence(got.Evidence, "verification", "passed") || !hasEvidence(got.Evidence, "scope", "edit") {
		t.Fatalf("missing expected evidence: %+v", got.Evidence)
	}
}

func TestExtractTranscriptSignals_NonExecToolInputDoesNotVerify(t *testing.T) {
	fixture := writeTranscript(t,
		`{"type":"started","uuid":"s","sessionId":"non-exec","timestamp":"2026-06-01T00:00:00Z"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"non-exec","timestamp":"2026-06-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"w1","name":"Write","input":{"file":"notes.md","content":"Remember to run go test ./... and gh pr create later."}}]}}`,
		`{"type":"user","uuid":"r1","sessionId":"non-exec","timestamp":"2026-06-01T00:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"w1","is_error":false,"content":"wrote notes.md"}]}}`,
		`{"type":"assistant","uuid":"a2","sessionId":"non-exec","timestamp":"2026-06-01T00:00:03Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"p1","name":"ExitPlanMode","input":{"plan":"Run make test, then git push if everything passes."}}]}}`,
		`{"type":"user","uuid":"r2","sessionId":"non-exec","timestamp":"2026-06-01T00:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"p1","is_error":false,"content":"plan accepted"}]}}`,
		`{"type":"result","uuid":"res","sessionId":"non-exec","timestamp":"2026-06-01T00:00:05Z","subtype":"success"}`,
	)

	got := extractTranscriptSignals("claudecode", fixture)
	if got.Verification != "" {
		t.Fatalf("verification = %q, want none from non-exec tool input", got.Verification)
	}
	if got.LandingQuality != "none" {
		t.Fatalf("landing_quality = %q, want none from non-exec tool input", got.LandingQuality)
	}
	if got.VerifiedAfterEdit {
		t.Fatal("verified_after_edit = true, want false without an executed verification command")
	}
	if hasEvidence(got.Evidence, "verification", "ran") || hasEvidence(got.Evidence, "landing_quality", "attempted") {
		t.Fatalf("non-exec input created command evidence: %+v", got.Evidence)
	}
}

func TestSuccessFacts_EarlierPendingToolCallDoesNotAbandonPRLink(t *testing.T) {
	fixture := writeTranscript(t,
		`{"type":"started","uuid":"s","sessionId":"pending-pr","timestamp":"2026-06-01T00:00:00Z"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"pending-pr","timestamp":"2026-06-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"v1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"pr-link","uuid":"pr","sessionId":"pending-pr","timestamp":"2026-06-01T00:00:02Z","url":"https://github.com/acme/app/pull/7"}`,
	)

	ver, landed, term := successFactsFromTranscript("claudecode", fixture)
	if ver != "" {
		t.Fatalf("verification = %q, want unknown when the test result is missing", ver)
	}
	if !landed {
		t.Fatal("landed = false, want true from PR link")
	}
	if term != nil {
		t.Fatalf("terminated = %v, want unknown when an older call is orphaned", term)
	}
}

func TestExtractTranscriptSignals_HumanCorrectionAndAcceptance(t *testing.T) {
	fixture := writeTranscript(t,
		`{"type":"started","uuid":"s","sessionId":"reaction","timestamp":"2026-06-01T00:00:00Z"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"reaction","timestamp":"2026-06-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"I changed it."}]}}`,
		`{"type":"user","uuid":"u1","sessionId":"reaction","timestamp":"2026-06-01T00:00:02Z","message":{"role":"user","content":"That failed, fix it."}}`,
		`{"type":"assistant","uuid":"a2","sessionId":"reaction","timestamp":"2026-06-01T00:00:03Z","message":{"role":"assistant","content":[{"type":"text","text":"Fixed."}]}}`,
		`{"type":"user","uuid":"u2","sessionId":"reaction","timestamp":"2026-06-01T00:00:04Z","message":{"role":"user","content":"Looks good, thanks."}}`,
	)

	got := extractTranscriptSignals("claudecode", fixture)
	if got.HumanCorrections != 1 || got.HumanAcceptances != 1 {
		t.Fatalf("corrections=%d acceptances=%d, want 1/1", got.HumanCorrections, got.HumanAcceptances)
	}
	if !hasEvidence(got.Evidence, "human_correction", "true") || !hasEvidence(got.Evidence, "human_acceptance", "true") {
		t.Fatalf("missing reaction evidence: %+v", got.Evidence)
	}
}

func TestExtractTranscriptSignals_ReworkAndLandingQuality(t *testing.T) {
	fixture := writeTranscript(t,
		`{"type":"started","uuid":"s","sessionId":"rework","timestamp":"2026-06-01T00:00:00Z"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"rework","timestamp":"2026-06-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"e1","name":"Edit","input":{"file":"internal/parser/parser.go"}}]}}`,
		`{"type":"user","uuid":"r1","sessionId":"rework","timestamp":"2026-06-01T00:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"e1","is_error":false,"content":"applied"}]}}`,
		`{"type":"assistant","uuid":"a2","sessionId":"rework","timestamp":"2026-06-01T00:00:03Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"e2","name":"Edit","input":{"file":"internal/parser/parser.go"}}]}}`,
		`{"type":"user","uuid":"r2","sessionId":"rework","timestamp":"2026-06-01T00:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"e2","is_error":false,"content":"applied"}]}}`,
		`{"type":"assistant","uuid":"a3","sessionId":"rework","timestamp":"2026-06-01T00:00:05Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"Bash","input":{"command":"git commit -am nope"}}]}}`,
		`{"type":"user","uuid":"r3","sessionId":"rework","timestamp":"2026-06-01T00:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","is_error":true,"content":"nothing to commit"}]}}`,
	)

	got := extractTranscriptSignals("claudecode", fixture)
	if got.ReworkCount != 1 {
		t.Fatalf("rework = %d, want 1", got.ReworkCount)
	}
	if got.LandingQuality != "failed" {
		t.Fatalf("landing_quality = %q, want failed", got.LandingQuality)
	}
}

func TestScoreCommand_JSONIncludesExtractedSignals(t *testing.T) {
	fixture := filepath.Join("testdata", "score", "verified.jsonl")
	out, err := runScore(t, "--json", fixture)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	var got struct {
		Signals struct {
			Extracted struct {
				Version        int    `json:"version"`
				Verification   string `json:"verification"`
				LandingQuality string `json:"landing_quality"`
				Evidence       []struct {
					Signal string `json:"signal"`
					Offset int64  `json:"offset"`
				} `json:"evidence"`
			} `json:"extracted"`
		} `json:"signals"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode score json: %v\n%s", err, out)
	}
	if got.Signals.Extracted.Version != 1 || got.Signals.Extracted.Verification != "passed" || got.Signals.Extracted.LandingQuality != "succeeded" {
		t.Fatalf("extracted = %+v", got.Signals.Extracted)
	}
	if len(got.Signals.Extracted.Evidence) == 0 {
		t.Fatal("expected extracted evidence")
	}
}

func TestVerifyOutcome_LastRunWins(t *testing.T) {
	// fail then a later pass → final state is passed.
	ids := []string{"a", "b"}
	results := map[string]toolResultFact{
		"a": {isError: true, text: "FAIL"},
		"b": {isError: false, text: "ok"},
	}
	if got := verifyOutcome(ids, results); got != "passed" {
		t.Errorf("verifyOutcome = %q, want passed", got)
	}
	// last run failed → failed
	results["b"] = toolResultFact{isError: true, text: "exit status 1"}
	if got := verifyOutcome(ids, results); got != "failed" {
		t.Errorf("verifyOutcome = %q, want failed", got)
	}
	// none ran
	if got := verifyOutcome(nil, results); got != "" {
		t.Errorf("verifyOutcome(none) = %q, want \"\"", got)
	}
}

func TestVerifyOutcome_ZeroFailSummaryIsPassing(t *testing.T) {
	ids := []string{"bun"}
	results := map[string]toolResultFact{
		"bun": {isError: false, text: "Process exited with code 0\n10 pass\n0 fail"},
	}
	if got := verifyOutcome(ids, results); got != "passed" {
		t.Errorf("verifyOutcome = %q, want passed", got)
	}
}

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := strings.Join(lines, "\n") + "\n"
	if err := writeFileAtomic(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path
}

func hasEvidence(evidence []score.SignalEvidence, signal, value string) bool {
	for _, ev := range evidence {
		if ev.Signal == signal && ev.Value == value {
			return true
		}
	}
	return false
}

package cli

import (
	"path/filepath"
	"strings"
	"testing"
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

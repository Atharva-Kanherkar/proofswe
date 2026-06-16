package cli

import (
	"path/filepath"
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

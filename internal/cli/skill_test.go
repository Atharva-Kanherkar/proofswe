package cli

import (
	"encoding/json"
	"testing"
)

func TestExtractTranscriptSignals_SkillDetectedAndExcluded(t *testing.T) {
	fixture := writeTranscript(t,
		`{"type":"started","uuid":"s","sessionId":"skill","timestamp":"2026-06-01T00:00:00Z"}`,
		`{"type":"assistant","uuid":"a0","sessionId":"skill","timestamp":"2026-06-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"on it"}]}}`,
		`{"type":"user","uuid":"u1","sessionId":"skill","timestamp":"2026-06-01T00:00:02Z","message":{"role":"user","content":"Base directory for this skill: /Users/x/.claude/skills/review-checkpoint\n# Review Checkpoint\nremember to run go test and gh pr create"}}`,
		`{"type":"user","uuid":"u2","sessionId":"skill","timestamp":"2026-06-01T00:00:03Z","message":{"role":"user","content":"thanks, looks good"}}`,
		`{"type":"result","uuid":"res","sessionId":"skill","timestamp":"2026-06-01T00:00:04Z","subtype":"success"}`,
	)

	got := extractTranscriptSignals("claudecode", fixture)

	if len(got.SkillsUsed) != 1 || got.SkillsUsed[0] != "review-checkpoint" {
		t.Fatalf("skills_used = %v, want [review-checkpoint]", got.SkillsUsed)
	}
	if !got.SkillAssisted {
		t.Error("skill_assisted = false, want true")
	}
	// Only "thanks, looks good" is a real human turn; the skill injection is not.
	if got.HumanTurns != 1 {
		t.Errorf("human_turns = %d, want 1 (skill injection excluded)", got.HumanTurns)
	}
	// "go test"/"gh pr create" appear in the skill TEXT, not an exec tool → no leak.
	if got.Verification != "" || got.LandingQuality != "none" {
		t.Errorf("skill text leaked into signals: verification=%q landing=%q", got.Verification, got.LandingQuality)
	}
	if !hasEvidence(got.Evidence, "skill_used", "review-checkpoint") {
		t.Fatalf("missing skill_used evidence: %+v", got.Evidence)
	}
}

func TestExtractTranscriptSignals_SkillDetectedWithWindowsPath(t *testing.T) {
	fixture := writeTranscript(t,
		`{"type":"started","uuid":"s","sessionId":"skill-win","timestamp":"2026-06-01T00:00:00Z"}`,
		`{"type":"user","uuid":"u1","sessionId":"skill-win","timestamp":"2026-06-01T00:00:01Z","message":{"role":"user","content":"Base directory for this skill: C:\\Users\\x\\.claude\\skills\\review-checkpoint\n# Review Checkpoint"}}`,
	)

	got := extractTranscriptSignals("claudecode", fixture)
	if len(got.SkillsUsed) != 1 || got.SkillsUsed[0] != "review-checkpoint" {
		t.Fatalf("skills_used = %v, want [review-checkpoint]", got.SkillsUsed)
	}
	if got.HumanTurns != 0 {
		t.Fatalf("human_turns = %d, want 0 for skill-only transcript", got.HumanTurns)
	}
}

func TestScoreCommand_SkillOnlyTranscriptHasZeroHumanTurns(t *testing.T) {
	fixture := writeTranscript(t,
		`{"type":"started","uuid":"s","sessionId":"skill-score","timestamp":"2026-06-01T00:00:00Z"}`,
		`{"type":"user","uuid":"u1","sessionId":"skill-score","timestamp":"2026-06-01T00:00:01Z","message":{"role":"user","content":"Base directory for this skill: /Users/x/.claude/skills/review-checkpoint\n# Review Checkpoint"}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"skill-score","timestamp":"2026-06-01T00:00:02Z","message":{"role":"assistant","model":"claude-opus-4-7","usage":{"input_tokens":100,"output_tokens":20},"content":[{"type":"text","text":"ready"}]}}`,
		`{"type":"result","uuid":"res","sessionId":"skill-score","timestamp":"2026-06-01T00:00:03Z","subtype":"success"}`,
	)

	out, err := runScore(t, "--json", fixture)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	var got struct {
		Signals struct {
			Turns     int `json:"turns"`
			Extracted struct {
				SkillAssisted bool `json:"skill_assisted"`
				HumanTurns    int  `json:"human_turns"`
			} `json:"extracted"`
		} `json:"signals"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode score json: %v\n%s", err, out)
	}
	if !got.Signals.Extracted.SkillAssisted {
		t.Fatalf("skill_assisted = false, want true")
	}
	if got.Signals.Turns != 0 || got.Signals.Extracted.HumanTurns != 0 {
		t.Fatalf("turns=%d human_turns=%d, want 0/0 for skill-only transcript", got.Signals.Turns, got.Signals.Extracted.HumanTurns)
	}
}

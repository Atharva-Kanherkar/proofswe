package cli

import "testing"

func TestJudgePromptVersionTracksRubric(t *testing.T) {
	if judgePromptVersion != "judge-prompt/2" {
		t.Fatalf("judgePromptVersion = %q, want judge-prompt/2", judgePromptVersion)
	}
}

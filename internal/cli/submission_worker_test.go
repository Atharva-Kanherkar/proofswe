package cli

import "testing"

func TestJudgePromptVersionTracksRubric(t *testing.T) {
	if judgePromptVersion != "judge-prompt/3" {
		t.Fatalf("judgePromptVersion = %q, want judge-prompt/3", judgePromptVersion)
	}
}

package cli

import (
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
)

func TestDeriveTitleSkipsInstructionBlobs(t *testing.T) {
	task := corpus.Task{Prompts: []corpus.Prompt{
		{TurnIndex: 0, Role: "user", Text: "# AGENTS.md instructions for /repo\n<INSTRUCTIONS>do things</INSTRUCTIONS>"},
		{TurnIndex: 1, Role: "user", Text: "Add a leaderboard detail page showing the full transcript"},
	}}
	got := deriveTitle(task)
	if got != "Add a leaderboard detail page showing the full transcript" {
		t.Fatalf("title = %q, want the real ask (instruction blob should be skipped)", got)
	}
}

func TestBuildConversationOrdersByTurnThenKind(t *testing.T) {
	task := corpus.Task{
		Prompts: []corpus.Prompt{{TurnIndex: 0, Role: "user", Text: "fix the bug"}},
		Transcript: corpus.Transcript{
			AssistantMessages: []corpus.Message{{TurnIndex: 0, Text: "on it"}},
			ToolCalls:         []corpus.Message{{TurnIndex: 0, Name: "bash", Text: "go test ./..."}},
			ToolOutputs:       []corpus.Message{{TurnIndex: 0, Name: "bash", Text: "ok"}},
		},
	}
	conv := buildConversation(task)
	gotRoles := make([]string, len(conv))
	for i, c := range conv {
		gotRoles[i] = c.Role
	}
	want := []string{"developer", "assistant", "tool_call", "tool_output"}
	if len(gotRoles) != len(want) {
		t.Fatalf("conversation roles = %v, want %v", gotRoles, want)
	}
	for i := range want {
		if gotRoles[i] != want[i] {
			t.Fatalf("conversation order = %v, want %v", gotRoles, want)
		}
	}
}

func TestBuildLeaderboardDetailIncludesConversationAndTitle(t *testing.T) {
	task := corpus.Task{
		Harness: "codex",
		Model:   "gpt-5",
		Repo:    corpus.Repo{RemoteURL: "https://github.com/owner/repo"},
		Prompts: []corpus.Prompt{{TurnIndex: 0, Role: "user", Text: "Improve the docs"}},
		Outcome: corpus.Outcome{Verification: "passed", Termination: "clean"},
		Transcript: corpus.Transcript{
			AssistantMessages: []corpus.Message{{TurnIndex: 0, Text: "done"}},
		},
	}
	rec := submissionRecord{SubmissionID: "sub_x", TaskID: "t1", Scorecard: &submitScorecard{Composite: 88, Axes: []submitAxis{{Name: "success", Present: true, Score: 90, Detail: "tests passed"}}}}
	detail := buildLeaderboardDetail(publishedCorpusRecord{Submission: rec, Harness: task.Harness, Model: task.Model, RepoURL: task.Repo.RemoteURL, Task: task})

	if detail.Title != "Improve the docs" {
		t.Fatalf("title = %q", detail.Title)
	}
	if detail.Repo != "owner/repo" {
		t.Fatalf("repo = %q", detail.Repo)
	}
	if detail.Summary == "" || len(detail.Axes) == 0 {
		t.Fatalf("detail missing summary/axes: %+v", detail.leaderboardSubmission)
	}
	if len(detail.Conversation) != 2 {
		t.Fatalf("conversation = %+v, want developer + assistant", detail.Conversation)
	}
}

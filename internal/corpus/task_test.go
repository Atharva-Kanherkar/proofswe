package corpus

import (
	"testing"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

func reproducibleCapture() core.Task {
	return core.Task{
		Harness: "claudecode",
		Model:   core.TaskModel{ID: "claude-opus-4-8"},
		Repo: core.TaskRepo{
			RemoteURL:   "https://github.com/owner/repo.git",
			BaseCommit:  "abc123",
			Branch:      "main",
			LicenseSPDX: "MIT",
			IsPublic:    true,
		},
		Prompts: []core.TaskPrompt{
			{TurnIndex: 0, Role: "user", Text: "add a retry to the client"},
		},
		Trajectory: core.TaskTrajectory{
			AssistantMessages: []core.TaskText{{TurnIndex: 0, Text: "on it"}},
			ToolCalls:         []core.TaskText{{TurnIndex: 0, Name: "Edit", Text: "..."}},
		},
		Code: core.TaskCode{
			Patch: "+++ b/client.go\n+retry()\n",
			Files: []core.TaskFile{{Path: "client.go", Role: core.FileRoleSolution}},
		},
		RedactionReport: core.RedactionReport{ScrubberVersion: "redact/1", SpansRedacted: 3, BestEffortNotice: "best-effort"},
	}
}

func TestFromCaptureMapsReproducibleFields(t *testing.T) {
	ex := score.ExtractedSignals{
		Verification:     "passed",
		LandingQuality:   "pr_link",
		Termination:      "clean",
		HumanTurns:       4,
		HumanCorrections: 2,
		ReworkCount:      7,
		SkillsUsed:       []string{"browse"},
		SkillAssisted:    true,
		Scope:            score.ScopeSignals{FilesTouched: 1},
	}
	card := &score.Result{
		Composite:    71,
		ScoreVersion: score.ScoreVersion,
		Axes: []score.Axis{
			{Name: "efficiency", Present: true, Score: 60},
			{Name: "success", Present: false, Score: 0}, // not present → dropped
		},
		Note: "provisional",
	}
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)

	task := FromCapture(reproducibleCapture(), ex, true, card, "sha256:deadbeef", "@me", now)

	if task.CorpusSchemaVersion != SchemaVersion {
		t.Errorf("schema version = %d, want %d", task.CorpusSchemaVersion, SchemaVersion)
	}
	if task.Repo.RemoteURL != "https://github.com/owner/repo.git" || task.Repo.BaseCommit != "abc123" {
		t.Errorf("repo not carried through: %+v", task.Repo)
	}
	if task.Contributor != "@me" || task.Model != "claude-opus-4-8" {
		t.Errorf("attribution/model wrong: %q / %q", task.Contributor, task.Model)
	}
	if len(task.Prompts) != 1 || task.Prompts[0].Text != "add a retry to the client" {
		t.Errorf("prompts not mapped: %+v", task.Prompts)
	}
	if task.Outcome.Verification != "passed" || !task.Outcome.Landed || task.Outcome.HumanCorrections != 2 {
		t.Errorf("outcome not mapped: %+v", task.Outcome)
	}
	if !task.Outcome.SkillAssisted || len(task.Outcome.SkillsUsed) != 1 {
		t.Errorf("skill signals not mapped: %+v", task.Outcome)
	}
	if task.Scrub.SpansRedacted != 3 || task.Scrub.ScrubberVersion != "redact/1" {
		t.Errorf("scrub report not carried: %+v", task.Scrub)
	}
	if task.Scorecard == nil || task.Scorecard.Composite != 71 {
		t.Fatalf("scorecard missing: %+v", task.Scorecard)
	}
	if task.Scorecard.ScoreVersion != score.ScoreVersion {
		t.Fatalf("score_version = %q, want %q", task.Scorecard.ScoreVersion, score.ScoreVersion)
	}
	if len(task.Scorecard.Axes) != 1 || task.Scorecard.Axes[0].Name != "efficiency" {
		t.Errorf("non-present axes should be dropped: %+v", task.Scorecard.Axes)
	}
}

func TestReproducibilityProblems(t *testing.T) {
	if probs := ReproducibilityProblems(FromCapture(reproducibleCapture(), score.ExtractedSignals{}, true, nil, "id", "", time.Now())); len(probs) != 0 {
		t.Fatalf("reproducible task flagged: %v", probs)
	}

	bad := reproducibleCapture()
	bad.Repo.RemoteURL = ""
	bad.Repo.IsPublic = false
	bad.Repo.BaseCommit = ""
	bad.Repo.LicenseSPDX = ""
	bad.Prompts = nil
	// remote, public, base commit, prompt. License and code-patch are no longer gated.
	probs := ReproducibilityProblems(FromCapture(bad, score.ExtractedSignals{}, false, nil, "id", "", time.Now()))
	if len(probs) != 4 {
		t.Fatalf("expected 4 problems, got %d: %v", len(probs), probs)
	}
}

func TestReproducibilityAllowsAnyLicenseAndPatchlessHistory(t *testing.T) {
	// A non-allowlisted (or absent) license no longer blocks: a public repo is
	// re-runnable regardless of its LICENSE file.
	for _, spdx := range []string{"GPL-3.0", "AGPL-3.0", ""} {
		task := FromCapture(reproducibleCapture(), score.ExtractedSignals{}, true, nil, "id", "", time.Now())
		task.Repo.LicenseSPDX = spdx
		if probs := ReproducibilityProblems(task); len(probs) != 0 {
			t.Fatalf("license %q flagged as non-reproducible: %v", spdx, probs)
		}
	}

	// A historical session whose work is already committed (clean tree -> no
	// patch) is still reproducible from remote + base commit + prompt.
	task := FromCapture(reproducibleCapture(), score.ExtractedSignals{}, true, nil, "id", "", time.Now())
	task.Code = Code{}
	if probs := ReproducibilityProblems(task); len(probs) != 0 {
		t.Fatalf("patchless historical task flagged: %v", probs)
	}
}

func TestPermitsCodeRedistribution(t *testing.T) {
	for _, spdx := range []string{"MIT", "BSD-2-Clause", "BSD-3-Clause", "Apache-2.0", "ISC", "Unlicense", "0BSD"} {
		if !PermitsCodeRedistribution(spdx) {
			t.Errorf("PermitsCodeRedistribution(%q) = false, want true", spdx)
		}
	}
	for _, spdx := range []string{"GPL-3.0", "AGPL-3.0", "MPL-2.0", ""} {
		if PermitsCodeRedistribution(spdx) {
			t.Errorf("PermitsCodeRedistribution(%q) = true, want false", spdx)
		}
	}
}

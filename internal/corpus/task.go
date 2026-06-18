// Package corpus defines the public, reproducible benchmark task — the artifact
// a contributor publishes to the proofswe corpus. It is deliberately distinct
// from internal/core.Task (the local capture/storage record): the corpus task
// drops the salted hashes, consent bookkeeping, and internal ids, and keeps only
// what makes a session a re-runnable, displayable benchmark item:
//
//   - the starting repo state (remote + base commit) so anyone can
//     `git clone && git checkout` into the exact conditions the session began,
//   - the developer's prompts (the ambiguous, oracle-less ask),
//   - the agent's trajectory (assistant turns + tool calls/outputs),
//   - the resulting code,
//   - the deterministic outcome (what survived / verified / landed),
//   - and an optional scorecard.
//
// The schema is additive-only and read by tolerant consumers: never remove a
// field, never repurpose one, bump SchemaVersion only for breaking changes.
package corpus

import (
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

// SchemaVersion is the corpus task contract version. Bump only on a breaking
// change; additions are backward-compatible and do not require a bump.
const SchemaVersion = 1

const (
	BaseCommitSourceHead            = core.BaseCommitSourceHead
	BaseCommitSourceTranscriptStart = core.BaseCommitSourceTranscriptStart

	CodePublicationAgreementVersion = "code-publication/1"
)

// Task is one reproducible benchmark task as published to the corpus.
type Task struct {
	CorpusSchemaVersion             int        `json:"corpus_schema_version"`
	TaskID                          string     `json:"task_id"`
	ContributedAt                   time.Time  `json:"contributed_at"`
	Contributor                     string     `json:"contributor,omitempty"`
	Harness                         string     `json:"harness"`
	HarnessCLIVersion               string     `json:"harness_cli_version,omitempty"`
	Model                           string     `json:"model"`
	Repo                            Repo       `json:"repo"`
	CodePublicationAgreementVersion string     `json:"code_publication_agreement_version,omitempty"`
	Prompts                         []Prompt   `json:"prompts"`
	Transcript                      Transcript `json:"transcript"`
	Code                            Code       `json:"code,omitzero"`
	Outcome                         Outcome    `json:"outcome"`
	Scorecard                       *Scorecard `json:"scorecard,omitempty"`
	Scrub                           Scrub      `json:"scrub"`
}

// Repo is the starting state needed to reproduce the task. A task is
// reproducible when RemoteURL and BaseCommit are present and the remote is
// public — see ReproducibilityProblems. LicenseSPDX is recorded for provenance
// but is not required (many public repos ship no LICENSE file).
type Repo struct {
	RemoteURL        string `json:"remote_url"`
	BaseCommit       string `json:"base_commit"`
	BaseCommitSource string `json:"base_commit_source,omitempty"`
	Branch           string `json:"branch,omitempty"`
	LicenseSPDX      string `json:"license_spdx"`
	IsPublic         bool   `json:"is_public"`
}

// Prompt is one developer turn — the task statement and any follow-up redirects.
type Prompt struct {
	TurnIndex int    `json:"turn_index"`
	Role      string `json:"role"`
	Text      string `json:"text"`
}

// Message is one assistant turn, tool call, or tool output.
type Message struct {
	TurnIndex int    `json:"turn_index"`
	Name      string `json:"name,omitempty"`
	Text      string `json:"text"`
}

// Transcript is the agent's side of the session, for display and analysis.
type Transcript struct {
	AssistantMessages []Message `json:"assistant_messages,omitempty"`
	ToolCalls         []Message `json:"tool_calls,omitempty"`
	ToolOutputs       []Message `json:"tool_outputs,omitempty"`
}

// Code is the work product: the added lines, split by solution vs test, plus the
// touched-file list. Captured for public repos when the working tree has a diff;
// optional — a historical session from a clean tree still submits (the work is
// preserved in the trajectory). See repoAllowsRawCode / ReproducibilityProblems.
type Code struct {
	Patch     string `json:"patch,omitempty"`
	TestPatch string `json:"test_patch,omitempty"`
	Files     []File `json:"files,omitempty"`
}

// File is one touched path and its classified role (solution/test/config/...).
type File struct {
	Path string `json:"path"`
	Role string `json:"role,omitempty"`
}

// Outcome is the deterministic, transcript-derived result — proofswe's
// oracle-substitute. No LLM is involved in producing these.
type Outcome struct {
	Verification     string   `json:"verification,omitempty"` // passed | failed | "" (none run)
	Landed           bool     `json:"landed"`                 // committed / pushed / PR opened
	LandingQuality   string   `json:"landing_quality,omitempty"`
	Termination      string   `json:"termination,omitempty"` // clean | abandoned | ""
	HumanTurns       int      `json:"human_turns,omitempty"`
	HumanCorrections int      `json:"human_corrections,omitempty"`
	HumanAcceptances int      `json:"human_acceptances,omitempty"`
	ReworkCount      int      `json:"rework_count,omitempty"`
	Interruptions    int      `json:"interruptions,omitempty"`
	FilesTouched     int      `json:"files_touched,omitempty"`
	TestFilesTouched int      `json:"test_files_touched,omitempty"`
	SkillsUsed       []string `json:"skills_used,omitempty"`
	SkillAssisted    bool     `json:"skill_assisted,omitempty"`
}

// Scorecard is the optional execution score (deterministic axes, plus the judge
// axis when --judge ran). Provisional; consumers may re-score from the data.
type Scorecard struct {
	Composite    float64 `json:"composite"`
	ScoreVersion string  `json:"score_version,omitempty"`
	Axes         []Axis  `json:"axes"`
	Note         string  `json:"note,omitempty"`
}

// Axis is one scored dimension.
type Axis struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// Scrub records the secret-scrubbing pass applied before publish. It is the one
// filter the corpus keeps: stripping live credentials protects the dataset's
// survival and contributors, not anyone's privacy.
type Scrub struct {
	ScrubberVersion string `json:"scrubber_version"`
	SpansRedacted   int    `json:"spans_redacted"`
	Notice          string `json:"notice"`
}

// FromCapture maps a full-content (already-scrubbed) core.Task plus its
// deterministic signals and optional scorecard into a publishable corpus task.
// The text in `task` is expected to already be scrubbed by the capture builders;
// FromCapture copies it verbatim and carries the redaction report forward.
func FromCapture(task core.Task, ex score.ExtractedSignals, landed bool, card *score.Result, taskID, contributor string, now time.Time) Task {
	out := Task{
		CorpusSchemaVersion: SchemaVersion,
		TaskID:              taskID,
		ContributedAt:       now.UTC(),
		Contributor:         contributor,
		Harness:             string(task.Harness),
		HarnessCLIVersion:   task.HarnessCLIVersion,
		Model:               string(task.Model.ID),
		Repo: Repo{
			RemoteURL:        task.Repo.RemoteURL,
			BaseCommit:       task.Repo.BaseCommit,
			BaseCommitSource: task.Repo.BaseCommitSource,
			Branch:           task.Repo.Branch,
			LicenseSPDX:      task.Repo.LicenseSPDX,
			IsPublic:         task.Repo.IsPublic,
		},
		Prompts:    promptsFrom(task.Prompts),
		Transcript: transcriptFrom(task.Trajectory),
		Code:       codeFrom(task.Code),
		Outcome: Outcome{
			Verification:     ex.Verification,
			Landed:           landed,
			LandingQuality:   ex.LandingQuality,
			Termination:      ex.Termination,
			HumanTurns:       ex.HumanTurns,
			HumanCorrections: ex.HumanCorrections,
			HumanAcceptances: ex.HumanAcceptances,
			ReworkCount:      ex.ReworkCount,
			Interruptions:    ex.Interruptions,
			FilesTouched:     ex.Scope.FilesTouched,
			TestFilesTouched: ex.Scope.TestFilesTouched,
			SkillsUsed:       ex.SkillsUsed,
			SkillAssisted:    ex.SkillAssisted,
		},
		Scrub: Scrub{
			ScrubberVersion: task.RedactionReport.ScrubberVersion,
			SpansRedacted:   task.RedactionReport.SpansRedacted,
			Notice:          task.RedactionReport.BestEffortNotice,
		},
	}
	if card != nil {
		out.Scorecard = scorecardFrom(*card)
	}
	return out
}

func promptsFrom(in []core.TaskPrompt) []Prompt {
	out := make([]Prompt, 0, len(in))
	for _, p := range in {
		out = append(out, Prompt{TurnIndex: p.TurnIndex, Role: p.Role, Text: p.Text})
	}
	return out
}

func transcriptFrom(t core.TaskTrajectory) Transcript {
	return Transcript{
		AssistantMessages: messagesFrom(t.AssistantMessages),
		ToolCalls:         messagesFrom(t.ToolCalls),
		ToolOutputs:       messagesFrom(t.ToolOutputs),
	}
}

func messagesFrom(in []core.TaskText) []Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]Message, 0, len(in))
	for _, m := range in {
		out = append(out, Message{TurnIndex: m.TurnIndex, Name: m.Name, Text: m.Text})
	}
	return out
}

func codeFrom(c core.TaskCode) Code {
	out := Code{Patch: c.Patch, TestPatch: c.TestPatch}
	for _, f := range c.Files {
		out.Files = append(out.Files, File{Path: f.Path, Role: string(f.Role)})
	}
	return out
}

func scorecardFrom(r score.Result) *Scorecard {
	card := &Scorecard{Composite: r.Composite, ScoreVersion: r.ScoreVersion, Note: r.Note}
	for _, a := range r.Axes {
		if a.Present {
			card.Axes = append(card.Axes, Axis{Name: a.Name, Score: a.Score})
		}
	}
	return card
}

// PermitsCodeRedistribution reports whether the detected license is acceptable
// for publishing raw code patches into the public corpus.
func PermitsCodeRedistribution(spdx string) bool {
	switch spdx {
	case "MIT", "BSD-2-Clause", "BSD-3-Clause", "Apache-2.0", "ISC", "Unlicense", "0BSD":
		return true
	default:
		return false
	}
}

func RequiresCodePublicationAgreement(t Task) bool {
	return hasCodePatch(t) && !PermitsCodeRedistribution(t.Repo.LicenseSPDX)
}

func HasCodePublicationAgreement(t Task) bool {
	return t.CodePublicationAgreementVersion == CodePublicationAgreementVersion
}

// ReproducibilityProblems returns the reasons a task cannot be reproduced by a
// third party. An empty slice means the task is a valid reproducible benchmark
// item. This is the gate `proofswe contribute` enforces before publishing.
func ReproducibilityProblems(t Task) []string {
	var problems []string
	if t.Repo.RemoteURL == "" {
		problems = append(problems, "no git remote (origin) — nothing to clone")
	}
	if !t.Repo.IsPublic {
		problems = append(problems, "remote is not a recognized public host (github.com / gitlab.com / codeberg.org)")
	}
	if t.Repo.BaseCommit == "" {
		problems = append(problems, "no base commit — the starting state is unknown")
	}
	if len(t.Prompts) == 0 {
		problems = append(problems, "no developer prompt — there is no task statement")
	}
	if !hasCodePatch(t) {
		if t.Repo.BaseCommitSource != BaseCommitSourceTranscriptStart {
			problems = append(problems, "no code patch and base commit was not inferred from the transcript start")
		}
		if t.Outcome.FilesTouched == 0 {
			problems = append(problems, "no code patch and no transcript edit evidence")
		}
	}
	// License is recorded for provenance but not gated: a public repo without a
	// LICENSE file is still re-runnable (clone + checkout + prompt). And a code
	// patch is no longer required when a historical session has transcript-start
	// base provenance and edit evidence. The patch is captured when present.
	return problems
}

func hasCodePatch(t Task) bool {
	return strings.TrimSpace(t.Code.Patch) != "" || strings.TrimSpace(t.Code.TestPatch) != ""
}

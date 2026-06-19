package cli

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
)

const (
	defaultLeaderboardLimit = 50
	maxLeaderboardLimit     = 250
)

type leaderboardResponse struct {
	GeneratedAt time.Time               `json:"generated_at"`
	Recent      []leaderboardSubmission `json:"recent"`
	Models      []leaderboardModel      `json:"models"`
}

type leaderboardSubmission struct {
	SubmissionID    string    `json:"submission_id"`
	TaskID          string    `json:"task_id"`
	Harness         string    `json:"harness"`
	Model           string    `json:"model"`
	Contributor     string    `json:"contributor,omitempty"`
	Repo            string    `json:"repo,omitempty"`
	Title           string    `json:"title,omitempty"`
	Score           float64   `json:"score"`
	ScoreVersion    string    `json:"score_version,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	GitHubPath      string    `json:"github_path"`
	GitHubURL       string    `json:"github_url,omitempty"`
	GitHubPRURL     string    `json:"github_pr_url,omitempty"`
	GitHubCommitSHA string    `json:"github_commit_sha,omitempty"`
	SubmittedAt     time.Time `json:"submitted_at"`
	PublishedAt     time.Time `json:"published_at"`

	// Detail powers the expandable per-task view: what the session was asked to
	// do, how it deterministically resolved, and the full scored breakdown with
	// the logit evidence behind the headline number.
	TaskStatement string              `json:"task_statement,omitempty"`
	FollowUps     int                 `json:"follow_ups,omitempty"`
	Outcome       *leaderboardOutcome `json:"outcome,omitempty"`
	Axes          []leaderboardAxis   `json:"axes,omitempty"`
	Utility       any                 `json:"utility,omitempty"`
	Note          string              `json:"note,omitempty"`
}

// leaderboardOutcome is the deterministic, transcript-derived result surfaced in
// the detail view — the "what happened / what failed" half of the story.
type leaderboardOutcome struct {
	Verification     string   `json:"verification,omitempty"` // passed | failed | "" (none run)
	Landed           bool     `json:"landed"`
	Termination      string   `json:"termination,omitempty"` // clean | abandoned | ""
	HumanCorrections int      `json:"human_corrections,omitempty"`
	HumanAcceptances int      `json:"human_acceptances,omitempty"`
	ReworkCount      int      `json:"rework_count,omitempty"`
	Interruptions    int      `json:"interruptions,omitempty"`
	FilesTouched     int      `json:"files_touched,omitempty"`
	TestFilesTouched int      `json:"test_files_touched,omitempty"`
	SkillsUsed       []string `json:"skills_used,omitempty"`
}

// leaderboardAxis is one scored dimension with its human-readable detail.
type leaderboardAxis struct {
	Name   string  `json:"name"`
	Score  float64 `json:"score"`
	Detail string  `json:"detail,omitempty"`
}

const maxTaskStatementChars = 1600

type leaderboardModel struct {
	Harness           string    `json:"harness"`
	Model             string    `json:"model"`
	SubmissionCount   int       `json:"submission_count"`
	AverageScore      float64   `json:"average_score"`
	BestScore         float64   `json:"best_score"`
	LatestScore       float64   `json:"latest_score"`
	LatestPublishedAt time.Time `json:"latest_published_at"`
}

func buildLeaderboardResponse(records []publishedCorpusRecord, modelRecords []publishedModelRecord, now time.Time) leaderboardResponse {
	resp := leaderboardResponse{GeneratedAt: now.UTC()}

	for _, record := range records {
		if record.Submission.Scorecard == nil {
			continue
		}
		resp.Recent = append(resp.Recent, buildLeaderboardItem(record))
	}

	for _, item := range modelRecords {
		resp.Models = append(resp.Models, leaderboardModel{
			Harness:           item.Harness,
			Model:             item.Model,
			SubmissionCount:   item.SubmissionCount,
			AverageScore:      roundScore(item.AverageScore),
			BestScore:         roundScore(item.BestScore),
			LatestScore:       roundScore(item.LatestScore),
			LatestPublishedAt: item.LatestPublishedAt.UTC(),
		})
	}
	sort.Slice(resp.Models, func(i, j int) bool {
		if resp.Models[i].AverageScore != resp.Models[j].AverageScore {
			return resp.Models[i].AverageScore > resp.Models[j].AverageScore
		}
		if resp.Models[i].SubmissionCount != resp.Models[j].SubmissionCount {
			return resp.Models[i].SubmissionCount > resp.Models[j].SubmissionCount
		}
		if resp.Models[i].Model != resp.Models[j].Model {
			return resp.Models[i].Model < resp.Models[j].Model
		}
		return resp.Models[i].Harness < resp.Models[j].Harness
	})
	return resp
}

func parseLeaderboardLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultLeaderboardLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxLeaderboardLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maxLeaderboardLimit)
	}
	return n, nil
}

func publicRepoName(remote string) string {
	remote = strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	if remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, "git@") {
		_, tail, ok := strings.Cut(remote, ":")
		if ok {
			return tail
		}
	}
	for _, marker := range []string{"github.com/", "gitlab.com/", "codeberg.org/"} {
		if _, tail, ok := strings.Cut(remote, marker); ok {
			return tail
		}
	}
	return remote
}

func githubCorpusURL(prURL, commit, path string) string {
	parsed, err := url.Parse(strings.TrimSpace(prURL))
	if err != nil || parsed.Host != "github.com" || commit == "" || path == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return "https://github.com/" + parts[0] + "/" + parts[1] + "/blob/" + url.PathEscape(commit) + "/" + strings.TrimLeft(path, "/")
}

func publicScorecardSummary(card *submitScorecard) string {
	if card == nil {
		return ""
	}
	byName := make(map[string]string, len(card.Axes))
	for _, axis := range card.Axes {
		if detail := strings.TrimSpace(axis.Detail); axis.Present && detail != "" {
			byName[axis.Name] = detail
		}
	}
	var parts []string
	for _, name := range []string{"success", "efficiency", "autonomy"} {
		if detail := byName[name]; detail != "" {
			parts = append(parts, detail)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "; ")
	}
	return strings.TrimSpace(card.Note)
}

// leaderboardDetail is the full per-transcript view: the leaderboard item plus
// the entire ordered conversation (developer prompts, assistant messages, tool
// calls and their outputs) so the page can render the whole session.
type leaderboardDetail struct {
	leaderboardSubmission
	Conversation []conversationTurn `json:"conversation,omitempty"`
}

// conversationTurn is one rendered line of the session, in chronological order.
type conversationTurn struct {
	Role string `json:"role"` // developer | assistant | tool_call | tool_output
	Name string `json:"name,omitempty"`
	Text string `json:"text"`
}

// buildLeaderboardItem maps one published record into the list/detail shape,
// shared by the feed and the single-transcript endpoint so they never diverge.
func buildLeaderboardItem(record publishedCorpusRecord) leaderboardSubmission {
	rec := record.Submission
	item := leaderboardSubmission{
		SubmissionID:    rec.SubmissionID,
		TaskID:          rec.TaskID,
		Harness:         record.Harness,
		Model:           record.Model,
		Contributor:     rec.Contributor,
		Repo:            publicRepoName(record.RepoURL),
		Title:           deriveTitle(record.Task),
		GitHubPath:      rec.GitHubPath,
		GitHubURL:       githubCorpusURL(rec.GitHubPRURL, rec.GitHubCommit, rec.GitHubPath),
		GitHubPRURL:     rec.GitHubPRURL,
		GitHubCommitSHA: rec.GitHubCommit,
		SubmittedAt:     rec.CreatedAt.UTC(),
		PublishedAt:     rec.UpdatedAt.UTC(),
		TaskStatement:   taskStatement(record.Task),
		FollowUps:       followUpCount(record.Task),
		Outcome:         leaderboardOutcomeFrom(record.Task),
	}
	if rec.Scorecard != nil {
		item.Score = roundScore(rec.Scorecard.Composite)
		item.ScoreVersion = rec.Scorecard.ScoreVersion
		item.Summary = outcomeSummary(record.Task, rec.Scorecard)
		item.Axes = leaderboardAxesFrom(rec.Scorecard)
		item.Utility = rec.Scorecard.Utility
		item.Note = rec.Scorecard.Note
	}
	return item
}

// buildLeaderboardDetail adds the full ordered conversation to a list item.
func buildLeaderboardDetail(record publishedCorpusRecord) leaderboardDetail {
	return leaderboardDetail{
		leaderboardSubmission: buildLeaderboardItem(record),
		Conversation:          buildConversation(record.Task),
	}
}

const (
	maxTurnTextChars     = 16000
	maxToolOutputChars   = 6000
	maxConversationTurns = 600
)

// buildConversation merges prompts, assistant messages, tool calls and outputs
// into one chronological stream, ordered by turn index then by kind so a turn
// reads prompt → assistant → tool call → tool output.
func buildConversation(task corpus.Task) []conversationTurn {
	type ordered struct {
		idx, kind, seq int
		role, name     string
		text           string
	}
	var rows []ordered
	seq := 0
	add := func(idx, kind int, role, name, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		rows = append(rows, ordered{idx: idx, kind: kind, seq: seq, role: role, name: name, text: text})
		seq++
	}
	for _, p := range task.Prompts {
		add(p.TurnIndex, 0, "developer", "", p.Text)
	}
	for _, m := range task.Transcript.AssistantMessages {
		add(m.TurnIndex, 1, "assistant", m.Name, m.Text)
	}
	for _, m := range task.Transcript.ToolCalls {
		add(m.TurnIndex, 2, "tool_call", m.Name, m.Text)
	}
	for _, m := range task.Transcript.ToolOutputs {
		add(m.TurnIndex, 3, "tool_output", m.Name, m.Text)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].idx != rows[j].idx {
			return rows[i].idx < rows[j].idx
		}
		if rows[i].kind != rows[j].kind {
			return rows[i].kind < rows[j].kind
		}
		return rows[i].seq < rows[j].seq
	})
	out := make([]conversationTurn, 0, len(rows))
	for _, r := range rows {
		limit := maxTurnTextChars
		if r.kind == 3 {
			limit = maxToolOutputChars
		}
		out = append(out, conversationTurn{Role: r.role, Name: r.name, Text: truncateRunes(r.text, limit)})
		if len(out) >= maxConversationTurns {
			break
		}
	}
	return out
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n… [truncated]"
}

// wrapperBlockRe strips harness-injected context/instruction blocks (codex wraps
// its first turn in <environment_context>/<user_instructions>; Claude Code injects
// <system-reminder>) so the real developer ask underneath can surface as the title.
var wrapperBlockRe = regexp.MustCompile(`(?is)<(environment_context|user_instructions|environment_details|system-reminder|instructions)>.*?</(environment_context|user_instructions|environment_details|system-reminder|instructions)>`)

func cleanPromptText(s string) string {
	return strings.TrimSpace(wrapperBlockRe.ReplaceAllString(s, " "))
}

// firstRealPrompt returns the first developer turn that is an actual ask rather
// than an injected instructions/agent/context blob, with wrapper blocks stripped.
func firstRealPrompt(task corpus.Task) string {
	for _, p := range task.Prompts {
		t := cleanPromptText(p.Text)
		if t != "" && !looksLikeInstructions(t) {
			return t
		}
	}
	if len(task.Prompts) > 0 {
		return cleanPromptText(task.Prompts[0].Text)
	}
	return ""
}

// deriveTitle picks a short, human title for the session from its real ask.
func deriveTitle(task corpus.Task) string {
	t := firstRealPrompt(task)
	if t == "" {
		return "Untitled session"
	}
	return firstLine(t, 90)
}

func looksLikeInstructions(t string) bool {
	lower := strings.ToLower(strings.TrimSpace(t))
	for _, prefix := range []string{"<environment_context", "<user_instructions", "<environment_details", "<instructions", "<system-reminder", "# agents.md", "agents.md"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, marker := range []string{"agents.md instructions", "base directory for this skill", "claude.md instructions", "you are claude"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func firstLine(t string, max int) string {
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[:i]
	}
	t = strings.TrimSpace(strings.TrimLeft(t, "#> -*"))
	if r := []rune(t); len(r) > max {
		return strings.TrimSpace(string(r[:max])) + "…"
	}
	return t
}

// outcomeSummary is a one-line, human read of what happened in the session.
func outcomeSummary(task corpus.Task, card *submitScorecard) string {
	o := task.Outcome
	var parts []string
	switch o.Verification {
	case "passed":
		parts = append(parts, "tests passed")
	case "failed":
		parts = append(parts, "tests failed")
	default:
		parts = append(parts, "no tests run")
	}
	if o.Landed {
		parts = append(parts, "changes landed")
	}
	switch o.Termination {
	case "clean":
		parts = append(parts, "clean finish")
	case "abandoned":
		parts = append(parts, "abandoned")
	}
	if o.HumanCorrections > 0 {
		parts = append(parts, fmt.Sprintf("%d correction(s)", o.HumanCorrections))
	}
	if len(parts) == 0 && card != nil {
		return publicScorecardSummary(card)
	}
	return strings.Join(parts, " · ")
}

// taskStatement returns the developer's opening ask — what the session set out
// to do — truncated so the leaderboard payload stays lean.
func taskStatement(task corpus.Task) string {
	text := firstRealPrompt(task)
	if text == "" {
		return ""
	}
	if r := []rune(text); len(r) > maxTaskStatementChars {
		return strings.TrimSpace(string(r[:maxTaskStatementChars])) + "…"
	}
	return text
}

// followUpCount is how many developer turns followed the opening ask — a quick
// read on how much steering the session needed.
func followUpCount(task corpus.Task) int {
	if len(task.Prompts) <= 1 {
		return 0
	}
	return len(task.Prompts) - 1
}

func leaderboardOutcomeFrom(task corpus.Task) *leaderboardOutcome {
	o := task.Outcome
	out := &leaderboardOutcome{
		Verification:     o.Verification,
		Landed:           o.Landed,
		Termination:      o.Termination,
		HumanCorrections: o.HumanCorrections,
		HumanAcceptances: o.HumanAcceptances,
		ReworkCount:      o.ReworkCount,
		Interruptions:    o.Interruptions,
		FilesTouched:     o.FilesTouched,
		TestFilesTouched: o.TestFilesTouched,
		SkillsUsed:       o.SkillsUsed,
	}
	return out
}

func leaderboardAxesFrom(card *submitScorecard) []leaderboardAxis {
	if card == nil {
		return nil
	}
	out := make([]leaderboardAxis, 0, len(card.Axes))
	for _, axis := range card.Axes {
		if !axis.Present {
			continue
		}
		out = append(out, leaderboardAxis{Name: axis.Name, Score: roundScore(axis.Score), Detail: axis.Detail})
	}
	return out
}

func roundScore(v float64) float64 {
	return math.Round(v*10) / 10
}

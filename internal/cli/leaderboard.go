package cli

import (
	"fmt"
	"math"
	"net/url"
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
		rec := record.Submission
		if rec.Scorecard == nil {
			continue
		}
		item := leaderboardSubmission{
			SubmissionID:    rec.SubmissionID,
			TaskID:          rec.TaskID,
			Harness:         record.Harness,
			Model:           record.Model,
			Contributor:     rec.Contributor,
			Repo:            publicRepoName(record.RepoURL),
			Score:           roundScore(rec.Scorecard.Composite),
			ScoreVersion:    rec.Scorecard.ScoreVersion,
			Summary:         publicScorecardSummary(rec.Scorecard),
			GitHubPath:      rec.GitHubPath,
			GitHubURL:       githubCorpusURL(rec.GitHubPRURL, rec.GitHubCommit, rec.GitHubPath),
			GitHubPRURL:     rec.GitHubPRURL,
			GitHubCommitSHA: rec.GitHubCommit,
			SubmittedAt:     rec.CreatedAt.UTC(),
			PublishedAt:     rec.UpdatedAt.UTC(),
			TaskStatement:   taskStatement(record.Task),
			FollowUps:       followUpCount(record.Task),
			Outcome:         leaderboardOutcomeFrom(record.Task),
			Axes:            leaderboardAxesFrom(rec.Scorecard),
			Utility:         rec.Scorecard.Utility,
			Note:            rec.Scorecard.Note,
		}
		resp.Recent = append(resp.Recent, item)
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

// taskStatement returns the developer's opening ask — what the session set out
// to do — truncated so the leaderboard payload stays lean.
func taskStatement(task corpus.Task) string {
	if len(task.Prompts) == 0 {
		return ""
	}
	text := strings.TrimSpace(task.Prompts[0].Text)
	if len(text) <= maxTaskStatementChars {
		return text
	}
	return strings.TrimSpace(text[:maxTaskStatementChars]) + "…"
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

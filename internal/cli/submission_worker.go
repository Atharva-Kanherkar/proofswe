package cli

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/judge"
	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

const (
	judgeVersion       = "judge/1"
	judgePromptVersion = "judge-prompt/1"
	workerIdleDelay    = 500 * time.Millisecond
)

type submissionWorker struct {
	store    submissionStore
	judge    judge.Judge
	workerID string
	logger   *slog.Logger
}

func (w submissionWorker) Run(ctx context.Context) {
	if w.workerID == "" {
		w.workerID = "proofswe-worker"
	}
	if w.logger == nil {
		w.logger = slog.Default()
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		claimed, ok, err := w.store.ClaimJudgeJob(ctx, w.workerID, time.Now().UTC())
		if err != nil {
			w.logger.Warn("claim judge job failed", "error", err)
			resetTimer(timer, workerIdleDelay)
			continue
		}
		if !ok {
			resetTimer(timer, workerIdleDelay)
			continue
		}
		w.process(ctx, claimed)
		resetTimer(timer, 0)
	}
}

func (w submissionWorker) process(ctx context.Context, job judgeJobRecord) {
	started := time.Now().UTC()
	verdict, err := w.judge.Assess(ctx, taskJudgeTurns(job.Task), job.Task.Outcome.SkillsUsed)
	if err != nil {
		if failErr := w.store.FailJudgeJob(ctx, job, err.Error(), time.Now().UTC()); failErr != nil {
			w.logger.Warn("record judge failure failed", "submission_id", job.SubmissionID, "error", failErr)
		}
		return
	}
	success := judge.ScoreSuccess(verdict)
	signals := signalsFromSubmittedTask(job.Task, success, judge.Label(verdict))
	result := score.Score(signals)
	card := scorecardForSubmit(result)
	completed := time.Now().UTC()
	run := judgeRunRecord{
		SubmissionID:  job.SubmissionID,
		JudgeModel:    serverJudgeModel(w.judge),
		JudgeVersion:  judgeVersion,
		PromptVersion: judgePromptVersion,
		Verdict:       verdict,
		Scorecard:     card,
		Status:        submissionStatusJudged,
		StartedAt:     started,
		CompletedAt:   completed,
	}
	if err := w.store.CompleteJudgeJob(ctx, job, run); err != nil {
		w.logger.Warn("complete judge job failed", "submission_id", job.SubmissionID, "error", err)
	}
}

func resetTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}

func submissionURL(basePath, submissionID string) string {
	if basePath == "" {
		return "/v1/submissions/" + submissionID
	}
	return fmt.Sprintf("%s/%s", trimTrailingSlash(basePath), submissionID)
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

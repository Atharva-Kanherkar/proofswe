package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/judge"
	"github.com/Atharva-Kanherkar/proofswe/internal/score"
)

const (
	judgeVersion       = "judge/2"
	judgePromptVersion = "judge-prompt/3"
	workerIdleDelay    = 500 * time.Millisecond
	workerPersistLimit = 10 * time.Second
)

type submissionWorker struct {
	store     submissionStore
	judge     judge.Judge
	publisher corpusPublisher
	workerID  string
	logger    *slog.Logger
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
			if w.publisher != nil {
				claimed, ok, err = w.store.ClaimPublishJob(ctx, w.workerID, time.Now().UTC())
				if err != nil {
					w.logger.Warn("claim publish job failed", "error", err)
					resetTimer(timer, workerIdleDelay)
					continue
				}
			}
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
	card := job.Scorecard
	if card == nil {
		started := time.Now().UTC()
		verdict, err := w.judge.Assess(ctx, taskJudgeTurns(job.Task), job.Task.Outcome.SkillsUsed)
		if err != nil {
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerPersistLimit)
			defer cancel()
			if failErr := w.store.FailJudgeJob(persistCtx, job, err.Error(), time.Now().UTC(), isPermanentJudgeFailure(err)); failErr != nil {
				w.logger.Warn("record judge failure failed", "submission_id", job.SubmissionID, "error", failErr)
			}
			return
		}
		if verdict.TaskType == judge.TaskTypeNoise {
			completed := time.Now().UTC()
			run := judgeRunRecord{
				SubmissionID:  job.SubmissionID,
				JudgeModel:    serverJudgeModel(w.judge),
				JudgeVersion:  judgeVersion,
				PromptVersion: judgePromptVersion,
				Verdict:       verdict,
				Status:        submissionStatusFiltered,
				StartedAt:     started,
				CompletedAt:   completed,
			}
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerPersistLimit)
			defer cancel()
			if err := w.store.CompleteJudgeJob(persistCtx, job, run); err != nil {
				w.logger.Warn("complete noise filter job failed", "submission_id", job.SubmissionID, "error", err)
			}
			return
		}
		success := judge.ScoreSuccess(verdict)
		signals := signalsFromSubmittedTask(job.Task, success, judge.Label(verdict))
		result := score.Score(signals)
		card = scorecardForSubmit(result)
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
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerPersistLimit)
		if err := w.store.CompleteJudgeJob(persistCtx, job, run); err != nil {
			cancel()
			w.logger.Warn("complete judge job failed", "submission_id", job.SubmissionID, "error", err)
			return
		}
		cancel()
	}

	if w.publisher == nil {
		return
	}
	if job.Scorecard == nil {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerPersistLimit)
		publishJob, ok, err := w.store.ClaimPublishJob(persistCtx, w.workerID, time.Now().UTC())
		cancel()
		if err != nil {
			w.logger.Warn("claim publish job failed", "submission_id", job.SubmissionID, "error", err)
			return
		}
		if !ok {
			return
		}
		job = publishJob
		card = publishJob.Scorecard
	}
	w.publish(ctx, job, card)
}

func (w submissionWorker) publish(ctx context.Context, job judgeJobRecord, card *submitScorecard) {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerPersistLimit)
	mapping, ok, err := w.store.GetTaskMapping(persistCtx, job.Task)
	cancel()
	if err != nil {
		persistCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), workerPersistLimit)
		defer cancel()
		if failErr := w.store.FailPublishJob(persistCtx, job, err.Error(), time.Now().UTC(), false); failErr != nil {
			w.logger.Warn("record mapping failure failed", "submission_id", job.SubmissionID, "error", failErr)
		}
		return
	}
	if !ok {
		var publishErr error
		mapping, publishErr = w.publisher.Publish(ctx, job.Task, card)
		if publishErr != nil {
			persistCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), workerPersistLimit)
			defer cancel()
			if failErr := w.store.FailPublishJob(persistCtx, job, publishErr.Error(), time.Now().UTC(), isPermanentPublishFailure(publishErr)); failErr != nil {
				w.logger.Warn("record publish failure failed", "submission_id", job.SubmissionID, "error", failErr)
			}
			return
		}
	}
	persistCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), workerPersistLimit)
	defer cancel()
	if err := w.store.CompletePublishJob(persistCtx, job, mapping); err != nil {
		w.logger.Warn("complete publish job failed", "submission_id", job.SubmissionID, "error", err)
	}
}

func isPermanentPublishFailure(err error) bool {
	var apiErr githubAPIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusBadRequest
	}
	return errors.Is(err, errTaskIDConflict)
}

func isPermanentJudgeFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errPermanentJudgeFailure) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"missing api key",
		"api status 400",
		"api status 401",
		"api status 403",
		"invalid outcome",
		"decode verdict",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
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

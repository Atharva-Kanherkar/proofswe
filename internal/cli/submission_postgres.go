package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const postgresMigrations = `
CREATE TABLE IF NOT EXISTS tasks (
	task_id TEXT PRIMARY KEY,
	task_json JSONB NOT NULL,
	repo_url TEXT,
	base_commit TEXT,
	model TEXT,
	harness TEXT,
	github_path TEXT,
	github_pr_url TEXT,
	github_commit_sha TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS submissions (
	submission_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL REFERENCES tasks(task_id),
	status TEXT NOT NULL,
	client_version TEXT,
	contributor TEXT,
	payload_sha256 TEXT NOT NULL UNIQUE,
	scorecard_json JSONB,
	error_code TEXT,
	error_message TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS judge_jobs (
	id BIGSERIAL PRIMARY KEY,
	submission_id TEXT NOT NULL REFERENCES submissions(submission_id),
	task_json JSONB NOT NULL,
	status TEXT NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	run_after TIMESTAMPTZ NOT NULL DEFAULT now(),
	locked_at TIMESTAMPTZ,
	locked_by TEXT,
	last_error TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS judge_jobs_claim_idx ON judge_jobs(status, run_after, id);

CREATE TABLE IF NOT EXISTS judge_runs (
	id BIGSERIAL PRIMARY KEY,
	submission_id TEXT NOT NULL REFERENCES submissions(submission_id),
	judge_model TEXT,
	judge_version TEXT,
	prompt_version TEXT,
	verdict_json JSONB NOT NULL,
	scorecard_json JSONB,
	status TEXT NOT NULL,
	started_at TIMESTAMPTZ NOT NULL,
	completed_at TIMESTAMPTZ
);
`

type postgresSubmissionStore struct {
	db *sql.DB
}

func newPostgresSubmissionStore(ctx context.Context, databaseURL string) (*postgresSubmissionStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	if _, err := db.ExecContext(ctx, postgresMigrations); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply postgres migrations: %w", err)
	}
	return &postgresSubmissionStore{db: db}, nil
}

func (s *postgresSubmissionStore) CreateSubmission(ctx context.Context, req submitRequest) (submissionRecord, error) {
	if err := validateSubmittedTask(req.Task); err != nil {
		return submissionRecord{}, err
	}
	taskJSON, err := json.Marshal(req.Task)
	if err != nil {
		return submissionRecord{}, fmt.Errorf("encode task: %w", err)
	}
	payloadHash, err := payloadSHA256(req)
	if err != nil {
		return submissionRecord{}, fmt.Errorf("hash payload: %w", err)
	}
	submissionID, err := newSubmissionID()
	if err != nil {
		return submissionRecord{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return submissionRecord{}, err
	}
	defer rollbackAfterDone(tx)

	_, err = tx.ExecContext(ctx, `
INSERT INTO tasks (task_id, task_json, repo_url, base_commit, model, harness, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (task_id) DO NOTHING
`, req.Task.TaskID, taskJSON, req.Task.Repo.RemoteURL, req.Task.Repo.BaseCommit, req.Task.Model, req.Task.Harness)
	if err != nil {
		return submissionRecord{}, fmt.Errorf("upsert task: %w", err)
	}

	var rec submissionRecord
	err = tx.QueryRowContext(ctx, `
INSERT INTO submissions (submission_id, task_id, status, client_version, contributor, payload_sha256)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (payload_sha256) DO UPDATE SET updated_at = submissions.updated_at
RETURNING submission_id, task_id, status, COALESCE(client_version, ''), COALESCE(contributor, ''), payload_sha256, created_at, updated_at
`, submissionID, req.Task.TaskID, submissionStatusQueued, req.ClientVersion, req.Task.Contributor, payloadHash).
		Scan(&rec.SubmissionID, &rec.TaskID, &rec.Status, &rec.ClientVersion, &rec.Contributor, &rec.PayloadSHA256, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return submissionRecord{}, fmt.Errorf("create submission: %w", err)
	}
	if rec.SubmissionID != submissionID {
		if err := tx.Commit(); err != nil {
			return submissionRecord{}, err
		}
		return rec, nil
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO judge_jobs (submission_id, task_json, status, run_after)
VALUES ($1, $2, $3, now())
`, submissionID, taskJSON, judgeJobStatusQueued); err != nil {
		return submissionRecord{}, fmt.Errorf("enqueue judge job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return submissionRecord{}, err
	}
	return rec, nil
}

func (s *postgresSubmissionStore) GetSubmission(ctx context.Context, submissionID string) (submissionRecord, bool, error) {
	var rec submissionRecord
	var scorecardJSON string
	var errorCode, errorMessage sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT submission_id, task_id, status, COALESCE(client_version, ''), COALESCE(contributor, ''),
       payload_sha256, COALESCE(scorecard_json::text, ''), error_code, error_message,
       COALESCE(t.github_path, ''), COALESCE(t.github_pr_url, ''), COALESCE(t.github_commit_sha, ''),
       s.created_at, s.updated_at
FROM submissions s
JOIN tasks t ON t.task_id = s.task_id
WHERE submission_id = $1
`, submissionID).Scan(&rec.SubmissionID, &rec.TaskID, &rec.Status, &rec.ClientVersion, &rec.Contributor, &rec.PayloadSHA256, &scorecardJSON, &errorCode, &errorMessage, &rec.GitHubPath, &rec.GitHubPRURL, &rec.GitHubCommit, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return submissionRecord{}, false, nil
	}
	if err != nil {
		return submissionRecord{}, false, err
	}
	if scorecardJSON != "" {
		var card submitScorecard
		if err := json.Unmarshal([]byte(scorecardJSON), &card); err != nil {
			return submissionRecord{}, false, fmt.Errorf("decode scorecard: %w", err)
		}
		rec.Scorecard = &card
	}
	rec.ErrorCode = errorCode.String
	rec.ErrorMessage = errorMessage.String
	return rec, true, nil
}

func (s *postgresSubmissionStore) ClaimJudgeJob(ctx context.Context, workerID string, now time.Time) (judgeJobRecord, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return judgeJobRecord{}, false, err
	}
	defer rollbackAfterDone(tx)

	var job judgeJobRecord
	var rawTask []byte
	if _, err := tx.ExecContext(ctx, `
WITH stale AS (
	UPDATE judge_jobs
	SET status = $1, run_after = $2, locked_at = NULL, locked_by = NULL, updated_at = $2
	WHERE status = $3 AND locked_at IS NOT NULL AND locked_at <= $4
	RETURNING submission_id
)
UPDATE submissions
SET status = $1, updated_at = $2
WHERE submission_id IN (SELECT submission_id FROM stale)
`, judgeJobStatusQueued, now, judgeJobStatusJudging, now.Add(-judgeVisibilityTimeout)); err != nil {
		return judgeJobRecord{}, false, err
	}

	err = tx.QueryRowContext(ctx, `
WITH next_job AS (
	SELECT id
	FROM judge_jobs
	WHERE status = $1 AND run_after <= $2
	ORDER BY run_after, id
	FOR UPDATE SKIP LOCKED
	LIMIT 1
), claimed AS (
	UPDATE judge_jobs
	SET status = $3, attempts = attempts + 1, locked_at = $2, locked_by = $4, updated_at = $2
	WHERE id = (SELECT id FROM next_job)
	RETURNING id, submission_id, attempts, task_json
)
SELECT claimed.id, claimed.submission_id, claimed.attempts, claimed.task_json
FROM claimed
`, judgeJobStatusQueued, now, judgeJobStatusJudging, workerID).Scan(&job.ID, &job.SubmissionID, &job.Attempts, &rawTask)
	if errors.Is(err, sql.ErrNoRows) {
		return judgeJobRecord{}, false, nil
	}
	if err != nil {
		return judgeJobRecord{}, false, err
	}
	job.Task, err = scanTask(rawTask)
	if err != nil {
		return judgeJobRecord{}, false, fmt.Errorf("decode claimed task: %w", err)
	}
	var scorecardJSON string
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(scorecard_json::text, '') FROM submissions WHERE submission_id = $1
`, job.SubmissionID).Scan(&scorecardJSON); err != nil {
		return judgeJobRecord{}, false, err
	}
	if scorecardJSON != "" {
		var card submitScorecard
		if err := json.Unmarshal([]byte(scorecardJSON), &card); err != nil {
			return judgeJobRecord{}, false, fmt.Errorf("decode claimed scorecard: %w", err)
		}
		job.Scorecard = &card
	}
	status := submissionStatusJudging
	if job.Scorecard != nil {
		status = submissionStatusPublish
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE submissions SET status = $1, updated_at = $2 WHERE submission_id = $3
`, status, now, job.SubmissionID); err != nil {
		return judgeJobRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return judgeJobRecord{}, false, err
	}
	return job, true, nil
}

func (s *postgresSubmissionStore) GetTaskMapping(ctx context.Context, taskID string) (corpusMapping, bool, error) {
	var mapping corpusMapping
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(github_path, ''), COALESCE(github_pr_url, ''), COALESCE(github_commit_sha, '')
FROM tasks
WHERE task_id = $1
`, taskID).Scan(&mapping.Path, &mapping.PRURL, &mapping.CommitSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return corpusMapping{}, false, nil
	}
	if err != nil {
		return corpusMapping{}, false, err
	}
	if mapping.Path == "" && mapping.PRURL == "" && mapping.CommitSHA == "" {
		return corpusMapping{}, false, nil
	}
	return mapping, true, nil
}

func (s *postgresSubmissionStore) CompleteJudgeJob(ctx context.Context, job judgeJobRecord, run judgeRunRecord) error {
	verdictJSON, err := json.Marshal(run.Verdict)
	if err != nil {
		return err
	}
	scorecardJSON, err := json.Marshal(run.Scorecard)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackAfterDone(tx)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO judge_runs (submission_id, judge_model, judge_version, prompt_version, verdict_json, scorecard_json, status, started_at, completed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`, job.SubmissionID, run.JudgeModel, run.JudgeVersion, run.PromptVersion, verdictJSON, scorecardJSON, run.Status, run.StartedAt, run.CompletedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE submissions
SET status = $1, scorecard_json = $2, error_code = NULL, error_message = NULL, updated_at = $3
WHERE submission_id = $4
`, submissionStatusJudged, scorecardJSON, run.CompletedAt, job.SubmissionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE judge_jobs
SET status = $1, updated_at = $2, locked_at = NULL, locked_by = NULL
WHERE id = $3
`, judgeJobStatusJudged, run.CompletedAt, job.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *postgresSubmissionStore) CompletePublishJob(ctx context.Context, job judgeJobRecord, mapping corpusMapping) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackAfterDone(tx)
	if _, err := tx.ExecContext(ctx, `
UPDATE tasks
SET github_path = $1, github_pr_url = $2, github_commit_sha = $3, updated_at = $4
WHERE task_id = $5
`, mapping.Path, mapping.PRURL, mapping.CommitSHA, now, job.Task.TaskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE submissions
SET status = $1, error_code = NULL, error_message = NULL, updated_at = $2
WHERE submission_id = $3
`, submissionStatusPubDone, now, job.SubmissionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE judge_jobs
SET status = $1, updated_at = $2, locked_at = NULL, locked_by = NULL
WHERE id = $3
`, judgeJobStatusPubDone, now, job.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *postgresSubmissionStore) FailPublishJob(ctx context.Context, job judgeJobRecord, errMessage string, now time.Time, permanent bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackAfterDone(tx)
	if permanent || job.Attempts >= maxJudgeAttempts {
		if _, err := tx.ExecContext(ctx, `
UPDATE judge_jobs
SET status = $1, last_error = $2, updated_at = $3, locked_at = NULL, locked_by = NULL
WHERE id = $4
`, judgeJobStatusFailed, errMessage, now, job.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE submissions
SET status = $1, error_code = 'publish_failed', error_message = $2, updated_at = $3
WHERE submission_id = $4
`, submissionStatusFailed, errMessage, now, job.SubmissionID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE judge_jobs
SET status = $1, run_after = $2, last_error = $3, updated_at = $4, locked_at = NULL, locked_by = NULL
WHERE id = $5
`, judgeJobStatusQueued, now.Add(time.Duration(job.Attempts)*judgeRetryBackoff), errMessage, now, job.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE submissions
SET status = $1, error_message = $2, updated_at = $3
WHERE submission_id = $4
`, submissionStatusJudged, errMessage, now, job.SubmissionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *postgresSubmissionStore) FailJudgeJob(ctx context.Context, job judgeJobRecord, errMessage string, now time.Time, permanent bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackAfterDone(tx)
	if permanent || job.Attempts >= maxJudgeAttempts {
		if _, err := tx.ExecContext(ctx, `
UPDATE judge_jobs
SET status = $1, last_error = $2, updated_at = $3, locked_at = NULL, locked_by = NULL
WHERE id = $4
`, judgeJobStatusFailed, errMessage, now, job.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE submissions
SET status = $1, error_code = 'judge_failed', error_message = $2, updated_at = $3
WHERE submission_id = $4
`, submissionStatusFailed, errMessage, now, job.SubmissionID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE judge_jobs
SET status = $1, run_after = $2, last_error = $3, updated_at = $4, locked_at = NULL, locked_by = NULL
WHERE id = $5
`, judgeJobStatusQueued, now.Add(time.Duration(job.Attempts)*judgeRetryBackoff), errMessage, now, job.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE submissions
SET status = $1, error_message = $2, updated_at = $3
WHERE submission_id = $4
`, submissionStatusQueued, errMessage, now, job.SubmissionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *postgresSubmissionStore) Close() error { return s.db.Close() }

func rollbackAfterDone(tx *sql.Tx) {
	_ = tx.Rollback()
}

func scanTask(raw []byte) (corpus.Task, error) {
	var task corpus.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return corpus.Task{}, err
	}
	return task, nil
}

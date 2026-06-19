package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

const postgresGetSubmissionQuery = `
SELECT s.submission_id, s.task_id, s.status, COALESCE(s.client_version, ''), COALESCE(s.contributor, ''),
       s.payload_sha256, COALESCE(s.scorecard_json::text, ''), s.error_code, s.error_message,
       COALESCE(t.github_path, ''), COALESCE(t.github_pr_url, ''), COALESCE(t.github_commit_sha, ''),
       s.created_at, s.updated_at
FROM submissions s
JOIN tasks t ON t.task_id = s.task_id
WHERE s.submission_id = $1
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
	err := s.db.QueryRowContext(ctx, postgresGetSubmissionQuery, submissionID).Scan(&rec.SubmissionID, &rec.TaskID, &rec.Status, &rec.ClientVersion, &rec.Contributor, &rec.PayloadSHA256, &scorecardJSON, &errorCode, &errorMessage, &rec.GitHubPath, &rec.GitHubPRURL, &rec.GitHubCommit, &rec.CreatedAt, &rec.UpdatedAt)
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

func (s *postgresSubmissionStore) GetTaskByID(ctx context.Context, taskID string) (corpus.Task, bool, error) {
	var taskJSON string
	err := s.db.QueryRowContext(ctx, `SELECT task_json::text FROM tasks WHERE task_id = $1`, taskID).Scan(&taskJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return corpus.Task{}, false, nil
	}
	if err != nil {
		return corpus.Task{}, false, err
	}
	var task corpus.Task
	if err := json.Unmarshal([]byte(taskJSON), &task); err != nil {
		return corpus.Task{}, false, fmt.Errorf("decode task: %w", err)
	}
	return task, true, nil
}

func (s *postgresSubmissionStore) ListPublishedCorpus(ctx context.Context, q publishedCorpusQuery) ([]publishedCorpusRecord, error) {
	args := []any{submissionStatusPubDone}
	var filters []string
	if q.Harness != "" {
		args = append(args, q.Harness)
		filters = append(filters, fmt.Sprintf("t.harness = $%d", len(args)))
	}
	if q.Model != "" {
		args = append(args, q.Model)
		filters = append(filters, fmt.Sprintf("t.model = $%d", len(args)))
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLeaderboardLimit
	}
	args = append(args, limit)

	where := `
WHERE s.status = $1
  AND s.scorecard_json IS NOT NULL
  AND COALESCE(t.github_path, '') <> ''
`
	if len(filters) > 0 {
		where += "  AND " + strings.Join(filters, "\n  AND ") + "\n"
	}
	rows, err := s.db.QueryContext(ctx, `
WITH published AS (
	SELECT DISTINCT ON (s.task_id)
	       s.submission_id, s.task_id, s.status, s.client_version, s.contributor, s.payload_sha256,
	       s.scorecard_json, s.created_at, s.updated_at, t.github_path, t.github_pr_url,
	       t.github_commit_sha, t.harness, t.model, t.repo_url, t.task_json
	FROM submissions s
	JOIN tasks t ON t.task_id = s.task_id
`+where+`
	ORDER BY s.task_id, s.updated_at DESC, s.submission_id DESC
)
SELECT p.submission_id, p.task_id, p.status, COALESCE(p.client_version, ''), COALESCE(p.contributor, ''),
       p.payload_sha256, p.scorecard_json::text, COALESCE(p.github_path, ''), COALESCE(p.github_pr_url, ''),
       COALESCE(p.github_commit_sha, ''), p.created_at, p.updated_at, COALESCE(p.harness, ''),
       COALESCE(p.model, ''), COALESCE(p.repo_url, ''), p.task_json::text
FROM published p
ORDER BY p.updated_at DESC
LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []publishedCorpusRecord
	for rows.Next() {
		var rec submissionRecord
		var scorecardJSON, taskJSON string
		var item publishedCorpusRecord
		if err := rows.Scan(&rec.SubmissionID, &rec.TaskID, &rec.Status, &rec.ClientVersion, &rec.Contributor, &rec.PayloadSHA256, &scorecardJSON, &rec.GitHubPath, &rec.GitHubPRURL, &rec.GitHubCommit, &rec.CreatedAt, &rec.UpdatedAt, &item.Harness, &item.Model, &item.RepoURL, &taskJSON); err != nil {
			return nil, err
		}
		var card submitScorecard
		if err := json.Unmarshal([]byte(scorecardJSON), &card); err != nil {
			return nil, fmt.Errorf("decode leaderboard scorecard: %w", err)
		}
		rec.Scorecard = &card
		// task_json carries the prompts + deterministic outcome the detail view
		// expands; a malformed row should not sink the whole feed, so decode
		// best-effort and fall through with whatever scalar columns we have.
		_ = json.Unmarshal([]byte(taskJSON), &item.Task)
		item.Submission = rec
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *postgresSubmissionStore) ListPublishedModelStats(ctx context.Context, q publishedCorpusQuery) ([]publishedModelRecord, error) {
	args := []any{submissionStatusPubDone}
	var filters []string
	if q.Harness != "" {
		args = append(args, q.Harness)
		filters = append(filters, fmt.Sprintf("t.harness = $%d", len(args)))
	}
	if q.Model != "" {
		args = append(args, q.Model)
		filters = append(filters, fmt.Sprintf("t.model = $%d", len(args)))
	}
	where := `
WHERE s.status = $1
  AND s.scorecard_json IS NOT NULL
  AND COALESCE(t.github_path, '') <> ''
`
	if len(filters) > 0 {
		where += "  AND " + strings.Join(filters, "\n  AND ") + "\n"
	}
	rows, err := s.db.QueryContext(ctx, `
WITH published AS (
	SELECT DISTINCT ON (s.task_id)
	       COALESCE(t.harness, '') AS harness,
	       COALESCE(t.model, '') AS model,
	       (s.scorecard_json->>'composite')::double precision AS score,
	       s.updated_at
	FROM submissions s
	JOIN tasks t ON t.task_id = s.task_id
`+where+`
	ORDER BY s.task_id, s.updated_at DESC, s.submission_id DESC
)
SELECT harness, model, COUNT(*), AVG(score), MAX(score),
       (ARRAY_AGG(score ORDER BY updated_at DESC))[1], MAX(updated_at)
FROM published
GROUP BY harness, model
`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []publishedModelRecord
	for rows.Next() {
		var item publishedModelRecord
		if err := rows.Scan(&item.Harness, &item.Model, &item.SubmissionCount, &item.AverageScore, &item.BestScore, &item.LatestScore, &item.LatestPublishedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
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
	WHERE status = $3
	  AND locked_at IS NOT NULL
	  AND locked_at <= $4
	  AND EXISTS (
		SELECT 1 FROM submissions s
		WHERE s.submission_id = judge_jobs.submission_id
		  AND s.scorecard_json IS NULL
	  )
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

func (s *postgresSubmissionStore) ClaimPublishJob(ctx context.Context, workerID string, now time.Time) (judgeJobRecord, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return judgeJobRecord{}, false, err
	}
	defer rollbackAfterDone(tx)

	if _, err := tx.ExecContext(ctx, `
WITH stale AS (
	UPDATE judge_jobs
	SET status = $1, run_after = $2, locked_at = NULL, locked_by = NULL, updated_at = $2
	WHERE status = $3
	  AND locked_at IS NOT NULL
	  AND locked_at <= $4
	  AND EXISTS (
		SELECT 1 FROM submissions s
		WHERE s.submission_id = judge_jobs.submission_id
		  AND s.scorecard_json IS NOT NULL
	  )
	RETURNING submission_id
)
UPDATE submissions
SET status = $5, updated_at = $2
WHERE submission_id IN (SELECT submission_id FROM stale)
`, judgeJobStatusJudged, now, judgeJobStatusJudging, now.Add(-judgeVisibilityTimeout), submissionStatusJudged); err != nil {
		return judgeJobRecord{}, false, err
	}

	var job judgeJobRecord
	var rawTask []byte
	var scorecardJSON string
	err = tx.QueryRowContext(ctx, `
WITH next_job AS (
	SELECT j.id
	FROM judge_jobs j
	JOIN submissions s ON s.submission_id = j.submission_id
	WHERE j.status = $1 AND j.run_after <= $2 AND s.scorecard_json IS NOT NULL
	ORDER BY j.run_after, j.id
	FOR UPDATE OF j SKIP LOCKED
	LIMIT 1
), claimed AS (
	UPDATE judge_jobs
	SET status = $3, attempts = attempts + 1, locked_at = $2, locked_by = $4, updated_at = $2
	WHERE id = (SELECT id FROM next_job)
	RETURNING id, submission_id, attempts, task_json
)
SELECT claimed.id, claimed.submission_id, claimed.attempts, claimed.task_json, s.scorecard_json::text
FROM claimed
JOIN submissions s ON s.submission_id = claimed.submission_id
`, judgeJobStatusJudged, now, judgeJobStatusJudging, workerID).Scan(&job.ID, &job.SubmissionID, &job.Attempts, &rawTask, &scorecardJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return judgeJobRecord{}, false, nil
	}
	if err != nil {
		return judgeJobRecord{}, false, err
	}
	job.Task, err = scanTask(rawTask)
	if err != nil {
		return judgeJobRecord{}, false, fmt.Errorf("decode publish task: %w", err)
	}
	var card submitScorecard
	if err := json.Unmarshal([]byte(scorecardJSON), &card); err != nil {
		return judgeJobRecord{}, false, fmt.Errorf("decode publish scorecard: %w", err)
	}
	job.Scorecard = &card
	if _, err := tx.ExecContext(ctx, `
UPDATE submissions SET status = $1, updated_at = $2 WHERE submission_id = $3
`, submissionStatusPublish, now, job.SubmissionID); err != nil {
		return judgeJobRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return judgeJobRecord{}, false, err
	}
	return job, true, nil
}

func (s *postgresSubmissionStore) GetTaskMapping(ctx context.Context, task corpus.Task) (corpusMapping, bool, error) {
	var mapping corpusMapping
	var rawTask []byte
	err := s.db.QueryRowContext(ctx, `
SELECT task_json, COALESCE(github_path, ''), COALESCE(github_pr_url, ''), COALESCE(github_commit_sha, '')
FROM tasks
WHERE task_id = $1
`, task.TaskID).Scan(&rawTask, &mapping.Path, &mapping.PRURL, &mapping.CommitSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return corpusMapping{}, false, nil
	}
	if err != nil {
		return corpusMapping{}, false, err
	}
	stored, err := scanTask(rawTask)
	if err != nil {
		return corpusMapping{}, false, err
	}
	if !sameCorpusTask(stored, task) {
		return corpusMapping{}, false, errTaskIDConflict
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
SET status = $1, attempts = 0, updated_at = $2, locked_at = NULL, locked_by = NULL
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
`, judgeJobStatusJudged, now.Add(time.Duration(job.Attempts)*judgeRetryBackoff), errMessage, now, job.ID); err != nil {
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

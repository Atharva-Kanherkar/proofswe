package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
	"github.com/Atharva-Kanherkar/proofswe/internal/judge"
)

const (
	submissionStatusQueued  = "queued"
	submissionStatusJudging = "judging"
	submissionStatusJudged  = "judged"
	submissionStatusFailed  = "failed"

	judgeJobStatusQueued  = "queued"
	judgeJobStatusJudging = "judging"
	judgeJobStatusJudged  = "judged"
	judgeJobStatusFailed  = "failed"

	maxJudgeAttempts       = 3
	judgeRetryBackoff      = 30 * time.Second
	judgeVisibilityTimeout = 5 * time.Minute
)

type submissionStore interface {
	CreateSubmission(context.Context, submitRequest) (submissionRecord, error)
	GetSubmission(context.Context, string) (submissionRecord, bool, error)
	ClaimJudgeJob(context.Context, string, time.Time) (judgeJobRecord, bool, error)
	CompleteJudgeJob(context.Context, judgeJobRecord, judgeRunRecord) error
	FailJudgeJob(context.Context, judgeJobRecord, string, time.Time, bool) error
	Close() error
}

var errPermanentJudgeFailure = errors.New("permanent judge failure")

type submissionRecord struct {
	SubmissionID  string
	TaskID        string
	Status        string
	ClientVersion string
	Contributor   string
	PayloadSHA256 string
	Scorecard     *submitScorecard
	ErrorCode     string
	ErrorMessage  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type judgeJobRecord struct {
	ID           int64
	SubmissionID string
	Attempts     int
	Task         corpus.Task
}

type judgeRunRecord struct {
	SubmissionID  string
	JudgeModel    string
	JudgeVersion  string
	PromptVersion string
	Verdict       judge.Verdict
	Scorecard     *submitScorecard
	Status        string
	StartedAt     time.Time
	CompletedAt   time.Time
}

func validateSubmittedTask(task corpus.Task) error {
	if task.CorpusSchemaVersion != corpus.SchemaVersion {
		return fmt.Errorf("unsupported corpus schema version %d", task.CorpusSchemaVersion)
	}
	if task.TaskID == "" {
		return fmt.Errorf("missing task_id")
	}
	if problems := corpus.ReproducibilityProblems(task); len(problems) > 0 {
		return fmt.Errorf("not a reproducible corpus task: %v", problems)
	}
	return nil
}

func payloadSHA256(req submitRequest) (string, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func newSubmissionID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "sub_" + hex.EncodeToString(b[:]), nil
}

type memorySubmissionStore struct {
	mu          sync.Mutex
	nextJobID   int64
	tasks       map[string]corpus.Task
	submissions map[string]submissionRecord
	byPayload   map[string]string
	jobs        map[int64]memoryJudgeJob
	runs        []judgeRunRecord
}

type memoryJudgeJob struct {
	record     judgeJobRecord
	status     string
	runAfter   time.Time
	lockedAt   time.Time
	lockedBy   string
	lastError  string
	createdAt  time.Time
	updatedAt  time.Time
	failedDone bool
}

func newMemorySubmissionStore() *memorySubmissionStore {
	return &memorySubmissionStore{
		tasks:       make(map[string]corpus.Task),
		submissions: make(map[string]submissionRecord),
		byPayload:   make(map[string]string),
		jobs:        make(map[int64]memoryJudgeJob),
	}
}

func (s *memorySubmissionStore) CreateSubmission(_ context.Context, req submitRequest) (submissionRecord, error) {
	if err := validateSubmittedTask(req.Task); err != nil {
		return submissionRecord{}, err
	}
	payloadHash, err := payloadSHA256(req)
	if err != nil {
		return submissionRecord{}, err
	}
	submissionID, err := newSubmissionID()
	if err != nil {
		return submissionRecord{}, err
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	if existingID := s.byPayload[payloadHash]; existingID != "" {
		rec := s.submissions[existingID]
		return rec, nil
	}
	if _, ok := s.tasks[req.Task.TaskID]; !ok {
		s.tasks[req.Task.TaskID] = req.Task
	}
	rec := submissionRecord{
		SubmissionID:  submissionID,
		TaskID:        req.Task.TaskID,
		Status:        submissionStatusQueued,
		ClientVersion: req.ClientVersion,
		Contributor:   req.Task.Contributor,
		PayloadSHA256: payloadHash,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	s.submissions[submissionID] = rec
	s.byPayload[payloadHash] = submissionID
	s.nextJobID++
	s.jobs[s.nextJobID] = memoryJudgeJob{
		record:    judgeJobRecord{ID: s.nextJobID, SubmissionID: submissionID, Task: req.Task},
		status:    judgeJobStatusQueued,
		runAfter:  now,
		createdAt: now,
		updatedAt: now,
	}
	return rec, nil
}

func (s *memorySubmissionStore) GetSubmission(_ context.Context, submissionID string) (submissionRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.submissions[submissionID]
	return rec, ok, nil
}

func (s *memorySubmissionStore) ClaimJudgeJob(_ context.Context, workerID string, now time.Time) (judgeJobRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, job := range s.jobs {
		if job.status == judgeJobStatusJudging && !job.lockedAt.IsZero() && now.Sub(job.lockedAt) >= judgeVisibilityTimeout {
			job.status = judgeJobStatusQueued
			job.lockedAt = time.Time{}
			job.lockedBy = ""
			job.runAfter = now
			job.updatedAt = now
		}
		if job.status != judgeJobStatusQueued || job.runAfter.After(now) {
			s.jobs[id] = job
			continue
		}
		job.status = judgeJobStatusJudging
		job.record.Attempts++
		job.lockedAt = now
		job.lockedBy = workerID
		job.updatedAt = now
		s.jobs[id] = job
		rec := s.submissions[job.record.SubmissionID]
		rec.Status = submissionStatusJudging
		rec.UpdatedAt = now
		s.submissions[rec.SubmissionID] = rec
		return job.record, true, nil
	}
	return judgeJobRecord{}, false, nil
}

func (s *memorySubmissionStore) CompleteJudgeJob(_ context.Context, job judgeJobRecord, run judgeRunRecord) error {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.jobs[job.ID]
	stored.status = judgeJobStatusJudged
	stored.updatedAt = now
	s.jobs[job.ID] = stored
	rec := s.submissions[job.SubmissionID]
	rec.Status = submissionStatusJudged
	rec.Scorecard = run.Scorecard
	rec.UpdatedAt = now
	s.submissions[job.SubmissionID] = rec
	s.runs = append(s.runs, run)
	return nil
}

func (s *memorySubmissionStore) FailJudgeJob(_ context.Context, job judgeJobRecord, errMessage string, now time.Time, permanent bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.jobs[job.ID]
	stored.lastError = errMessage
	stored.updatedAt = now
	rec := s.submissions[job.SubmissionID]
	if permanent || job.Attempts >= maxJudgeAttempts {
		stored.status = judgeJobStatusFailed
		stored.failedDone = true
		rec.Status = submissionStatusFailed
		rec.ErrorCode = "judge_failed"
		rec.ErrorMessage = errMessage
	} else {
		stored.status = judgeJobStatusQueued
		stored.lockedAt = time.Time{}
		stored.lockedBy = ""
		stored.runAfter = now.Add(time.Duration(job.Attempts) * judgeRetryBackoff)
		rec.Status = submissionStatusQueued
		rec.ErrorMessage = errMessage
	}
	rec.UpdatedAt = now
	s.jobs[job.ID] = stored
	s.submissions[job.SubmissionID] = rec
	return nil
}

func (s *memorySubmissionStore) Close() error { return nil }

func (s *memorySubmissionStore) taskCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tasks)
}

func (s *memorySubmissionStore) judgeRunCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.runs)
}

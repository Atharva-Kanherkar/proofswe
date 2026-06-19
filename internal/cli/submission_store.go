package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
	"github.com/Atharva-Kanherkar/proofswe/internal/judge"
)

const (
	submissionStatusQueued  = "queued"
	submissionStatusJudging = "judging"
	submissionStatusJudged  = "judged"
	submissionStatusPublish = "publishing"
	submissionStatusPubDone = "published"
	submissionStatusFailed  = "failed"

	judgeJobStatusQueued  = "queued"
	judgeJobStatusJudging = "judging"
	judgeJobStatusJudged  = "judged"
	judgeJobStatusPubDone = "published"
	judgeJobStatusFailed  = "failed"

	maxJudgeAttempts       = 3
	judgeRetryBackoff      = 30 * time.Second
	judgeVisibilityTimeout = 5 * time.Minute
)

type submissionStore interface {
	CreateSubmission(context.Context, submitRequest) (submissionRecord, error)
	GetSubmission(context.Context, string) (submissionRecord, bool, error)
	ListPublishedCorpus(context.Context, publishedCorpusQuery) ([]publishedCorpusRecord, error)
	ListPublishedModelStats(context.Context, publishedCorpusQuery) ([]publishedModelRecord, error)
	ClaimJudgeJob(context.Context, string, time.Time) (judgeJobRecord, bool, error)
	ClaimPublishJob(context.Context, string, time.Time) (judgeJobRecord, bool, error)
	CompleteJudgeJob(context.Context, judgeJobRecord, judgeRunRecord) error
	GetTaskMapping(context.Context, corpus.Task) (corpusMapping, bool, error)
	CompletePublishJob(context.Context, judgeJobRecord, corpusMapping) error
	FailPublishJob(context.Context, judgeJobRecord, string, time.Time, bool) error
	FailJudgeJob(context.Context, judgeJobRecord, string, time.Time, bool) error
	Close() error
}

var (
	errPermanentJudgeFailure = errors.New("permanent judge failure")
	errTaskIDConflict        = errors.New("task_id conflicts with stored corpus task")
)

type submissionRecord struct {
	SubmissionID  string
	TaskID        string
	Status        string
	ClientVersion string
	Contributor   string
	PayloadSHA256 string
	Scorecard     *submitScorecard
	GitHubPath    string
	GitHubPRURL   string
	GitHubCommit  string
	ErrorCode     string
	ErrorMessage  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// seq is a monotonic creation counter used by the in-memory store to order
	// submissions deterministically when UpdatedAt timestamps collide (coarse
	// OS clocks, e.g. Windows). It is unexported and never serialized.
	seq int64
}

type judgeJobRecord struct {
	ID           int64
	SubmissionID string
	Attempts     int
	Task         corpus.Task
	Scorecard    *submitScorecard
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

type publishedCorpusQuery struct {
	Limit   int
	Harness string
	Model   string
}

type publishedCorpusRecord struct {
	Submission submissionRecord
	Harness    string
	Model      string
	RepoURL    string
}

type publishedModelRecord struct {
	Harness           string
	Model             string
	SubmissionCount   int
	AverageScore      float64
	BestScore         float64
	LatestScore       float64
	LatestPublishedAt time.Time
}

func validateSubmittedTask(task corpus.Task) error {
	if task.CorpusSchemaVersion != corpus.SchemaVersion {
		return fmt.Errorf("unsupported corpus schema version %d", task.CorpusSchemaVersion)
	}
	if task.TaskID == "" {
		return fmt.Errorf("missing task_id")
	}
	if want := corpusTaskID(task); task.TaskID != want {
		return fmt.Errorf("task_id mismatch: got %s, want %s", task.TaskID, want)
	}
	if problems := corpus.ReproducibilityProblems(task); len(problems) > 0 {
		return fmt.Errorf("not a reproducible corpus task: %v", problems)
	}
	if corpus.RequiresCodePublicationAgreement(task) && !corpus.HasCodePublicationAgreement(task) {
		return fmt.Errorf("code publication agreement required for raw code from license %q", task.Repo.LicenseSPDX)
	}
	return nil
}

func corpusTaskID(task corpus.Task) string {
	starting := ""
	if len(task.Prompts) > 0 {
		starting = task.Prompts[0].Text
	}
	sum := sha256.Sum256([]byte(task.Repo.RemoteURL + "\x00" + task.Repo.BaseCommit + "\x00" + starting))
	return "sha256:" + hex.EncodeToString(sum[:])
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
	nextSeq     int64
	tasks       map[string]corpus.Task
	mappings    map[string]corpusMapping
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
		mappings:    make(map[string]corpusMapping),
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
	s.nextSeq++
	rec := submissionRecord{
		SubmissionID:  submissionID,
		TaskID:        req.Task.TaskID,
		Status:        submissionStatusQueued,
		ClientVersion: req.ClientVersion,
		Contributor:   req.Task.Contributor,
		PayloadSHA256: payloadHash,
		CreatedAt:     now,
		UpdatedAt:     now,
		seq:           s.nextSeq,
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

func (s *memorySubmissionStore) ListPublishedCorpus(_ context.Context, q publishedCorpusQuery) ([]publishedCorpusRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	latest := s.latestPublishedSubmissions()
	var out []publishedCorpusRecord
	for _, rec := range latest {
		task, ok := s.tasks[rec.TaskID]
		if !ok {
			continue
		}
		if q.Harness != "" && task.Harness != q.Harness {
			continue
		}
		if q.Model != "" && task.Model != q.Model {
			continue
		}
		out = append(out, publishedCorpusRecord{
			Submission: rec,
			Harness:    task.Harness,
			Model:      task.Model,
			RepoURL:    task.Repo.RemoteURL,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i].Submission, out[j].Submission
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		return left.seq > right.seq
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (s *memorySubmissionStore) ListPublishedModelStats(_ context.Context, q publishedCorpusQuery) ([]publishedModelRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	latest := s.latestPublishedSubmissions()
	type aggregate struct {
		row publishedModelRecord
		sum float64
	}
	byModel := map[string]*aggregate{}
	for _, rec := range latest {
		task, ok := s.tasks[rec.TaskID]
		if !ok || (q.Harness != "" && task.Harness != q.Harness) || (q.Model != "" && task.Model != q.Model) {
			continue
		}
		key := task.Harness + "\x00" + task.Model
		item := byModel[key]
		if item == nil {
			item = &aggregate{row: publishedModelRecord{Harness: task.Harness, Model: task.Model}}
			byModel[key] = item
		}
		item.row.SubmissionCount++
		item.sum += rec.Scorecard.Composite
		if rec.Scorecard.Composite > item.row.BestScore {
			item.row.BestScore = rec.Scorecard.Composite
		}
		if rec.UpdatedAt.After(item.row.LatestPublishedAt) {
			item.row.LatestScore = rec.Scorecard.Composite
			item.row.LatestPublishedAt = rec.UpdatedAt
		}
	}
	out := make([]publishedModelRecord, 0, len(byModel))
	for _, item := range byModel {
		item.row.AverageScore = item.sum / float64(item.row.SubmissionCount)
		out = append(out, item.row)
	}
	return out, nil
}

func (s *memorySubmissionStore) latestPublishedSubmissions() map[string]submissionRecord {
	latest := make(map[string]submissionRecord)
	for _, rec := range s.submissions {
		if rec.Status != submissionStatusPubDone || rec.Scorecard == nil || rec.GitHubPath == "" {
			continue
		}
		current, ok := latest[rec.TaskID]
		if !ok || rec.UpdatedAt.After(current.UpdatedAt) || (rec.UpdatedAt.Equal(current.UpdatedAt) && rec.seq > current.seq) {
			latest[rec.TaskID] = rec
		}
	}
	return latest
}

func (s *memorySubmissionStore) GetTaskMapping(_ context.Context, task corpus.Task) (corpusMapping, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.tasks[task.TaskID]
	if ok && !sameCorpusTask(stored, task) {
		return corpusMapping{}, false, errTaskIDConflict
	}
	mapping, ok := s.mappings[task.TaskID]
	return mapping, ok, nil
}

func (s *memorySubmissionStore) ClaimJudgeJob(_ context.Context, workerID string, now time.Time) (judgeJobRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, job := range s.jobs {
		rec := s.submissions[job.record.SubmissionID]
		if job.status == judgeJobStatusJudging && rec.Scorecard == nil && !job.lockedAt.IsZero() && now.Sub(job.lockedAt) >= judgeVisibilityTimeout {
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
		job.record.Scorecard = rec.Scorecard
		job.lockedAt = now
		job.lockedBy = workerID
		job.updatedAt = now
		s.jobs[id] = job
		if rec.Scorecard != nil {
			rec.Status = submissionStatusPublish
		} else {
			rec.Status = submissionStatusJudging
		}
		rec.UpdatedAt = now
		s.submissions[rec.SubmissionID] = rec
		return job.record, true, nil
	}
	return judgeJobRecord{}, false, nil
}

func (s *memorySubmissionStore) ClaimPublishJob(_ context.Context, workerID string, now time.Time) (judgeJobRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, job := range s.jobs {
		rec := s.submissions[job.record.SubmissionID]
		if job.status == judgeJobStatusJudging && rec.Scorecard != nil && !job.lockedAt.IsZero() && now.Sub(job.lockedAt) >= judgeVisibilityTimeout {
			job.status = judgeJobStatusJudged
			job.lockedAt = time.Time{}
			job.lockedBy = ""
			job.runAfter = now
			job.updatedAt = now
		}
		if job.status != judgeJobStatusJudged || job.runAfter.After(now) {
			s.jobs[id] = job
			continue
		}
		if rec.Scorecard == nil {
			s.jobs[id] = job
			continue
		}
		job.status = judgeJobStatusJudging
		job.record.Attempts++
		job.record.Scorecard = rec.Scorecard
		job.lockedAt = now
		job.lockedBy = workerID
		job.updatedAt = now
		s.jobs[id] = job
		rec.Status = submissionStatusPublish
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
	stored.record.Attempts = 0
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

func (s *memorySubmissionStore) CompletePublishJob(_ context.Context, job judgeJobRecord, mapping corpusMapping) error {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mappings[job.Task.TaskID] = mapping
	stored := s.jobs[job.ID]
	stored.status = judgeJobStatusPubDone
	stored.updatedAt = now
	s.jobs[job.ID] = stored
	rec := s.submissions[job.SubmissionID]
	rec.Status = submissionStatusPubDone
	rec.GitHubPath = mapping.Path
	rec.GitHubPRURL = mapping.PRURL
	rec.GitHubCommit = mapping.CommitSHA
	rec.UpdatedAt = now
	s.submissions[job.SubmissionID] = rec
	return nil
}

func (s *memorySubmissionStore) FailPublishJob(_ context.Context, job judgeJobRecord, errMessage string, now time.Time, permanent bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.jobs[job.ID]
	stored.lastError = errMessage
	stored.updatedAt = now
	rec := s.submissions[job.SubmissionID]
	rec.ErrorMessage = errMessage
	if permanent || job.Attempts >= maxJudgeAttempts {
		stored.status = judgeJobStatusFailed
		rec.Status = submissionStatusFailed
		rec.ErrorCode = "publish_failed"
	} else {
		stored.status = judgeJobStatusJudged
		stored.lockedAt = time.Time{}
		stored.lockedBy = ""
		stored.runAfter = now.Add(time.Duration(job.Attempts) * judgeRetryBackoff)
		rec.Status = submissionStatusJudged
	}
	rec.UpdatedAt = now
	s.jobs[job.ID] = stored
	s.submissions[job.SubmissionID] = rec
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

func sameCorpusTask(a, b corpus.Task) bool {
	return reflect.DeepEqual(a, b)
}

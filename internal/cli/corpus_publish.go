package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
)

const (
	defaultCorpusRepo = "Atharva-Kanherkar/proofswe-corpus"
	githubAPIBaseURL  = "https://api.github.com"
)

type corpusMapping struct {
	Path      string
	PRURL     string
	CommitSHA string
}

type corpusPublisher interface {
	Publish(context.Context, corpus.Task, *submitScorecard) (corpusMapping, error)
}

type githubCorpusPublisher struct {
	client  submitDoer
	token   string
	repo    string
	baseURL string
}

func newConfiguredCorpusPublisher(cfg Config) corpusPublisher {
	if strings.EqualFold(strings.TrimSpace(getenvOrEmpty(cfg, "PROOFSWE_DISABLE_GITHUB_PUBLISH")), "true") {
		return nil
	}
	token := strings.TrimSpace(firstNonEmpty(getenvOrEmpty(cfg, "PROOFSWE_GITHUB_TOKEN"), getenvOrEmpty(cfg, "GITHUB_TOKEN")))
	if token == "" {
		return nil
	}
	repo := firstNonEmpty(strings.TrimSpace(getenvOrEmpty(cfg, "PROOFSWE_CORPUS_REPO")), defaultCorpusRepo)
	return githubCorpusPublisher{
		client:  &http.Client{Timeout: 30 * time.Second},
		token:   token,
		repo:    repo,
		baseURL: githubAPIBaseURL,
	}
}

func (p githubCorpusPublisher) Publish(ctx context.Context, task corpus.Task, card *submitScorecard) (corpusMapping, error) {
	path, err := corpusTaskPath(task.TaskID)
	if err != nil {
		return corpusMapping{}, err
	}
	shortID := shortTaskID(task.TaskID)
	branch := "proofswe/task/" + shortID
	title := "Add proofswe task " + shortID

	baseSHA, err := p.defaultBranchSHA(ctx)
	if err != nil {
		return corpusMapping{}, err
	}
	if err := p.ensureBranch(ctx, branch, baseSHA); err != nil {
		return corpusMapping{}, err
	}
	commitSHA, err := p.putTask(ctx, branch, path, taskWithOfficialScorecard(task, card))
	if err != nil {
		return corpusMapping{}, err
	}
	prURL, err := p.ensurePullRequest(ctx, branch, title)
	if err != nil {
		return corpusMapping{}, err
	}
	return corpusMapping{Path: path, PRURL: prURL, CommitSHA: commitSHA}, nil
}

func corpusTaskPath(taskID string) (string, error) {
	full, ok := strings.CutPrefix(taskID, "sha256:")
	if !ok || len(full) < 2 {
		return "", fmt.Errorf("invalid sha256 task id %q", taskID)
	}
	return "tasks/sha256/" + full[:2] + "/" + full + ".json", nil
}

func taskWithOfficialScorecard(task corpus.Task, card *submitScorecard) corpus.Task {
	if card == nil {
		return task
	}
	task.Scorecard = &corpus.Scorecard{Composite: card.Composite, Note: card.Note}
	for _, axis := range card.Axes {
		task.Scorecard.Axes = append(task.Scorecard.Axes, corpus.Axis{Name: axis.Name, Score: axis.Score})
	}
	return task
}

func (p githubCorpusPublisher) defaultBranchSHA(ctx context.Context) (string, error) {
	var out struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := p.githubJSON(ctx, http.MethodGet, "/repos/"+p.repo+"/git/ref/heads/main", nil, &out); err != nil {
		return "", err
	}
	if out.Object.SHA == "" {
		return "", fmt.Errorf("github: empty main ref sha")
	}
	return out.Object.SHA, nil
}

func (p githubCorpusPublisher) ensureBranch(ctx context.Context, branch, sha string) error {
	body := map[string]string{"ref": "refs/heads/" + branch, "sha": sha}
	err := p.githubJSON(ctx, http.MethodPost, "/repos/"+p.repo+"/git/refs", body, nil)
	if isGitHubConflict(err) {
		return nil
	}
	return err
}

func (p githubCorpusPublisher) putTask(ctx context.Context, branch, path string, task corpus.Task) (string, error) {
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	existingSHA, err := p.contentSHA(ctx, branch, path)
	if err != nil && !isGitHubNotFound(err) {
		return "", err
	}
	body := map[string]any{
		"message": "Add proofswe task " + shortTaskID(task.TaskID),
		"content": base64.StdEncoding.EncodeToString(data),
		"branch":  branch,
	}
	if existingSHA != "" {
		body["sha"] = existingSHA
	}
	var out struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := p.githubJSON(ctx, http.MethodPut, "/repos/"+p.repo+"/contents/"+path, body, &out); err != nil {
		return "", err
	}
	return out.Commit.SHA, nil
}

func (p githubCorpusPublisher) contentSHA(ctx context.Context, branch, path string) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	err := p.githubJSON(ctx, http.MethodGet, "/repos/"+p.repo+"/contents/"+path+"?ref="+url.QueryEscape(branch), nil, &out)
	return out.SHA, err
}

func (p githubCorpusPublisher) ensurePullRequest(ctx context.Context, branch, title string) (string, error) {
	body := map[string]string{"title": title, "head": branch, "base": "main"}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	err := p.githubJSON(ctx, http.MethodPost, "/repos/"+p.repo+"/pulls", body, &out)
	if err == nil {
		return out.HTMLURL, nil
	}
	if !isGitHubConflict(err) {
		return "", err
	}
	var prs []struct {
		HTMLURL string `json:"html_url"`
	}
	owner, _, _ := strings.Cut(p.repo, "/")
	q := "?state=open&head=" + url.QueryEscape(owner+":"+branch)
	if err := p.githubJSON(ctx, http.MethodGet, "/repos/"+p.repo+"/pulls"+q, nil, &prs); err != nil {
		return "", err
	}
	if len(prs) == 0 || prs[0].HTMLURL == "" {
		return "", fmt.Errorf("github: pull request already exists but could not be found")
	}
	return prs[0].HTMLURL, nil
}

type githubAPIError struct {
	StatusCode int
	Body       string
}

func (e githubAPIError) Error() string {
	return fmt.Sprintf("github: status %d: %s", e.StatusCode, strings.TrimSpace(e.Body))
}

func isGitHubConflict(err error) bool {
	var apiErr githubAPIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnprocessableEntity
}

func isGitHubNotFound(err error) bool {
	var apiErr githubAPIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func (p githubCorpusPublisher) githubJSON(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.baseURL, "/")+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("x-github-api-version", "2022-11-28")
	req.Header.Set("authorization", "Bearer "+p.token)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	client := p.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return githubAPIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("github: decode response: %w", err)
	}
	return nil
}

package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/adapter/claudecode"
	"github.com/Atharva-Kanherkar/proofswe/internal/adapter/codex"
	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
)

const (
	submitSchemaVersion   = 1
	defaultSubmitEndpoint = "https://proofswe.com/v1/submissions"
)

type submitDoer interface {
	Do(*http.Request) (*http.Response, error)
}

var submitHTTPClient submitDoer = &http.Client{Timeout: 60 * time.Second}

type submitRequest struct {
	SchemaVersion int         `json:"schema_version"`
	ClientVersion string      `json:"client_version,omitempty"`
	Task          corpus.Task `json:"task"`
}

type submitResponse struct {
	SubmissionID    string           `json:"submission_id,omitempty"`
	TaskID          string           `json:"task_id,omitempty"`
	Status          string           `json:"status,omitempty"`
	URL             string           `json:"url,omitempty"`
	GitHubPath      string           `json:"github_path,omitempty"`
	GitHubPRURL     string           `json:"github_pr_url,omitempty"`
	GitHubCommitSHA string           `json:"github_commit_sha,omitempty"`
	Judge           submitJudge      `json:"judge,omitempty"`
	Scorecard       *submitScorecard `json:"scorecard,omitempty"`
}

type submitJudge struct {
	Status  string `json:"status,omitempty"`
	Model   string `json:"model,omitempty"`
	Version string `json:"version,omitempty"`
}

type submitScorecard struct {
	Composite float64      `json:"composite"`
	Utility   any          `json:"utility,omitempty"`
	Axes      []submitAxis `json:"axes,omitempty"`
	Note      string       `json:"note,omitempty"`
}

type submitAxis struct {
	Name    string  `json:"name"`
	Present bool    `json:"present,omitempty"`
	Score   float64 `json:"score"`
	Detail  string  `json:"detail,omitempty"`
}

func runSubmitCommand(ctx context.Context, cfg Config, args []string) error {
	flags := flag.NewFlagSet("submit", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var harness, handle, endpoint, token string
	var asJSON, force bool
	var wait bool
	var waitTimeout, pollInterval time.Duration
	flags.StringVar(&harness, "harness", "", "claudecode|codex (auto-detected if empty)")
	flags.StringVar(&handle, "as", "", "optional attribution, e.g. @you (omit to stay anonymous)")
	flags.StringVar(&endpoint, "endpoint", "", "submission endpoint (default: PROOFSWE_API_URL or hosted proofswe API)")
	flags.StringVar(&token, "token", "", "optional proofswe API token (default: PROOFSWE_API_TOKEN)")
	flags.BoolVar(&asJSON, "json", false, "emit the server response as JSON")
	flags.BoolVar(&force, "force", false, "submit even if the task is not fully reproducible")
	flags.BoolVar(&wait, "wait", true, "poll until the server scorecard is ready")
	flags.BoolFunc("no-wait", "submit and return immediately without polling", func(string) error {
		wait = false
		return nil
	})
	flags.DurationVar(&waitTimeout, "wait-timeout", 2*time.Minute, "maximum time to wait for a server scorecard")
	flags.DurationVar(&pollInterval, "poll-interval", 2*time.Second, "delay between submission status polls")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("%w: submit accepts at most one transcript path", ErrUsage)
	}
	var path string
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	} else {
		var err error
		path, harness, err = latestSubmitTranscript(ctx, cfg, harness)
		if err != nil {
			return err
		}
	}

	if harness == "" {
		harness = detectHarness(path)
	}
	if harness != "claudecode" && harness != "codex" {
		return fmt.Errorf("%w: unknown harness %q", ErrUsage, harness)
	}

	task, err := buildSubmitTask(ctx, cfg, harness, path, handle)
	if err != nil {
		return err
	}
	if problems := corpus.ReproducibilityProblems(task); len(problems) > 0 {
		printContributionProblems(cfg.Stderr, problems)
		if !force {
			return fmt.Errorf("not a reproducible corpus task (use --force to submit anyway, but it cannot be re-run)")
		}
	}

	endpoint = submitEndpoint(cfg, endpoint)
	token = firstNonEmpty(token, getenvOrEmpty(cfg, "PROOFSWE_API_TOKEN"))
	resp, err := submitTask(ctx, endpoint, token, submitRequest{
		SchemaVersion: submitSchemaVersion,
		ClientVersion: cfg.Version,
		Task:          task,
	})
	if err != nil {
		return err
	}
	if wait && resp.SubmissionID != "" && isPendingSubmissionStatus(resp.Status) && (resp.Scorecard == nil || isPendingPublishStatus(resp.Status)) {
		if polled, err := pollSubmission(ctx, endpoint, token, resp, waitTimeout, pollInterval); err == nil {
			resp = polled
		} else if !asJSON {
			_, _ = fmt.Fprintf(cfg.Stderr, "proofswe submit: score polling stopped: %v\n", err)
		}
	}
	if asJSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	printSubmitText(cfg.Stdout, resp)
	return nil
}

func buildSubmitTask(ctx context.Context, cfg Config, harness, path, handle string) (corpus.Task, error) {
	captured, err := buildContributionTask(ctx, cfg, harness, path)
	if err != nil {
		return corpus.Task{}, err
	}
	result, _, _, err := scoreTranscript(cfg, harness, path, false, judgeOptions{})
	if err != nil {
		return corpus.Task{}, err
	}
	extracted := extractTranscriptSignals(harness, path)
	_, landed, _ := successFactsFromExtracted(extracted)
	taskID := contributionTaskID(captured)
	return corpus.FromCapture(captured, extracted, landed, &result, taskID, strings.TrimSpace(handle), time.Now()), nil
}

type submitTranscriptCandidate struct {
	path    string
	harness string
	modTime time.Time
}

func latestSubmitTranscript(ctx context.Context, cfg Config, harness string) (string, string, error) {
	cfg = cfg.withDefaults()
	repoRoot, ok := gitRepoRootContext(ctx, cfg.WorkDir)
	if !ok {
		return "", "", fmt.Errorf("cannot auto-detect a transcript outside a git repository; pass a transcript path explicitly")
	}
	repoRoot = canonicalSubmitPath(repoRoot)
	var candidates []submitTranscriptCandidate
	if harness == "" || harness == "claudecode" {
		root := filepath.Join(cfg.HomeDir, ".claude")
		transcripts, err := claudecode.Discover(root)
		if err == nil {
			for _, transcript := range transcripts {
				if !isClaudeMainTranscript(root, transcript) || !transcriptBelongsToRepo(ctx, "claudecode", transcript.Path, transcript.RepoPath, repoRoot) {
					continue
				}
				candidates = append(candidates, submitCandidate("claudecode", transcript.Path))
			}
		}
	}
	if harness == "" || harness == "codex" {
		root := firstNonEmpty(getenvOrEmpty(cfg, "CODEX_HOME"), filepath.Join(cfg.HomeDir, ".codex"))
		transcripts, err := codex.Discover(root)
		if err == nil {
			for _, transcript := range transcripts {
				if !transcriptBelongsToRepo(ctx, "codex", transcript.Path, "", repoRoot) {
					continue
				}
				candidates = append(candidates, submitCandidate("codex", transcript.Path))
			}
		}
	}
	var latest submitTranscriptCandidate
	for _, candidate := range candidates {
		if candidate.path == "" {
			continue
		}
		if latest.path == "" || candidate.modTime.After(latest.modTime) || candidate.modTime.Equal(latest.modTime) && candidate.path > latest.path {
			latest = candidate
		}
	}
	if latest.path == "" {
		if harness != "" {
			return "", "", fmt.Errorf("no %s transcript found for current repo %s; pass a transcript path explicitly", harness, repoRoot)
		}
		return "", "", fmt.Errorf("no supported Claude Code or Codex transcript found for current repo %s; pass a transcript path explicitly", repoRoot)
	}
	return latest.path, latest.harness, nil
}

func submitCandidate(harness, path string) submitTranscriptCandidate {
	info, err := os.Stat(path)
	if err != nil {
		return submitTranscriptCandidate{}
	}
	return submitTranscriptCandidate{path: path, harness: harness, modTime: info.ModTime()}
}

func isClaudeMainTranscript(root string, transcript claudecode.Transcript) bool {
	if transcript.Path == "" || transcript.ProjectSlug == "" {
		return false
	}
	return filepath.Clean(filepath.Dir(transcript.Path)) == filepath.Clean(filepath.Join(root, "projects", transcript.ProjectSlug))
}

func transcriptBelongsToRepo(ctx context.Context, harness, transcriptPath, fallbackRepoPath, repoRoot string) bool {
	if cwd := transcriptCWD(harness, transcriptPath); cwd != "" {
		root, ok := gitRepoRootContext(ctx, cwd)
		return ok && canonicalSubmitPath(root) == repoRoot
	}
	return fallbackRepoPath != "" && canonicalSubmitPath(fallbackRepoPath) == repoRoot
}

func transcriptCWD(harness, transcriptPath string) string {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	for i := 0; i < 128; i++ {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if cwd := recordCWD(harness, line); cwd != "" {
				return cwd
			}
		}
		if err != nil {
			return ""
		}
	}
	return ""
}

func recordCWD(harness string, line []byte) string {
	switch harness {
	case "claudecode":
		var raw struct {
			CWD string `json:"cwd"`
		}
		if err := json.Unmarshal(line, &raw); err == nil {
			return raw.CWD
		}
	case "codex":
		var raw struct {
			CWD     string `json:"cwd"`
			Payload struct {
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &raw); err == nil {
			if raw.CWD != "" {
				return raw.CWD
			}
			return raw.Payload.CWD
		}
	}
	return ""
}

func canonicalSubmitPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(abs)
}

func submitEndpoint(cfg Config, flagValue string) string {
	return firstNonEmpty(flagValue, getenvOrEmpty(cfg, "PROOFSWE_API_URL"), defaultSubmitEndpoint)
}

func getenvOrEmpty(cfg Config, key string) string {
	if cfg.Getenv != nil {
		return cfg.Getenv(key)
	}
	return os.Getenv(key)
}

func submitTask(ctx context.Context, endpoint, token string, reqBody submitRequest) (submitResponse, error) {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return submitResponse{}, fmt.Errorf("encode submission: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return submitResponse{}, fmt.Errorf("create submission request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	if token != "" {
		req.Header.Set("authorization", "Bearer "+token)
	}

	resp, err := submitHTTPClient.Do(req)
	if err != nil {
		return submitResponse{}, fmt.Errorf("submit task: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return submitResponse{}, fmt.Errorf("submit task: server status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out submitResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return submitResponse{}, fmt.Errorf("decode submission response: %w", err)
	}
	return out, nil
}

func pollSubmission(ctx context.Context, endpoint, token string, initial submitResponse, timeout, interval time.Duration) (submitResponse, error) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	pollURL := initial.URL
	if pollURL == "" || strings.HasPrefix(pollURL, "/") {
		pollURL = trimTrailingSlash(endpoint) + "/" + initial.SubmissionID
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	current := initial
	for {
		if isTerminalSubmissionStatus(current.Status) {
			return current, nil
		}
		if current.Scorecard != nil && !isPendingPublishStatus(current.Status) {
			return current, nil
		}
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		case <-ticker.C:
		}
		next, err := getSubmission(ctx, pollURL, token)
		if err != nil {
			return current, err
		}
		current = next
	}
}

func getSubmission(ctx context.Context, url, token string) (submitResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return submitResponse{}, fmt.Errorf("create poll request: %w", err)
	}
	req.Header.Set("accept", "application/json")
	if token != "" {
		req.Header.Set("authorization", "Bearer "+token)
	}
	resp, err := submitHTTPClient.Do(req)
	if err != nil {
		return submitResponse{}, fmt.Errorf("poll submission: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return submitResponse{}, fmt.Errorf("poll submission: server status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out submitResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return submitResponse{}, fmt.Errorf("decode poll response: %w", err)
	}
	return out, nil
}

func isPendingSubmissionStatus(status string) bool {
	switch status {
	case "", submissionStatusQueued, submissionStatusJudging, submissionStatusPublish:
		return true
	default:
		return false
	}
}

func isTerminalSubmissionStatus(status string) bool {
	switch status {
	case submissionStatusPubDone, submissionStatusFailed:
		return true
	default:
		return false
	}
}

func isPendingPublishStatus(status string) bool {
	return status == submissionStatusPublish
}

func printSubmitText(w io.Writer, r submitResponse) {
	status := firstNonEmpty(r.Status, "submitted")
	_, _ = fmt.Fprintf(w, "\nproofswe submit · %s\n\n", status)
	if r.SubmissionID != "" {
		_, _ = fmt.Fprintf(w, "  submission  %s\n", r.SubmissionID)
	}
	if r.TaskID != "" {
		_, _ = fmt.Fprintf(w, "  task        %s\n", r.TaskID)
	}
	if r.Judge.Status != "" || r.Judge.Model != "" {
		_, _ = fmt.Fprintf(w, "  judge       %s", firstNonEmpty(r.Judge.Status, "server"))
		if r.Judge.Model != "" {
			_, _ = fmt.Fprintf(w, " · %s", r.Judge.Model)
		}
		if r.Judge.Version != "" {
			_, _ = fmt.Fprintf(w, " · %s", r.Judge.Version)
		}
		_, _ = fmt.Fprintln(w)
	}
	if r.Scorecard != nil {
		_, _ = fmt.Fprintf(w, "\n  official score: %.0f / 100\n", r.Scorecard.Composite)
		for _, axis := range r.Scorecard.Axes {
			_, _ = fmt.Fprintf(w, "  %-11s %3.0f", axis.Name, axis.Score)
			if axis.Detail != "" {
				_, _ = fmt.Fprintf(w, "   %s", axis.Detail)
			}
			_, _ = fmt.Fprintln(w)
		}
		if r.Scorecard.Note != "" {
			_, _ = fmt.Fprintf(w, "  note        %s\n", r.Scorecard.Note)
		}
	} else {
		_, _ = fmt.Fprintln(w, "\n  official score pending server judge")
	}
	if r.GitHubPath != "" {
		_, _ = fmt.Fprintf(w, "  corpus      %s\n", r.GitHubPath)
	}
	if r.GitHubPRURL != "" {
		_, _ = fmt.Fprintf(w, "  corpus PR   %s\n", r.GitHubPRURL)
	}
	if r.URL != "" {
		_, _ = fmt.Fprintf(w, "\n  %s\n", r.URL)
	}
	_, _ = fmt.Fprintln(w)
}

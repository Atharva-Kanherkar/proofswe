package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/adapter/claudecode"
	"github.com/Atharva-Kanherkar/proofswe/internal/adapter/codex"
	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
)

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

type uploadTranscript struct {
	Harness   string
	Path      string
	RepoRoot  string
	SessionID string
	ModTime   time.Time
}

type uploadRepoGroup struct {
	RepoRoot    string
	Transcripts []uploadTranscript
}

type uploadOptions struct {
	Harness                        string
	Handle                         string
	Endpoint                       string
	Token                          string
	Repos                          []string
	All                            bool
	DryRun                         bool
	Force                          bool
	AcceptCodePublicationAgreement bool
	Wait                           bool
	WaitTimeout                    time.Duration
	PollInterval                   time.Duration
	BatchSize                      int
}

type uploadSummary struct {
	Discovered int `json:"discovered"`
	Selected   int `json:"selected"`
	Submitted  int `json:"submitted"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}

func runUploadCommand(ctx context.Context, cfg Config, args []string) error {
	flags := flag.NewFlagSet("upload", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts uploadOptions
	var repos stringListFlag
	flags.StringVar(&opts.Harness, "harness", "", "claudecode|codex (default: both)")
	flags.StringVar(&opts.Handle, "as", "", "optional attribution, e.g. @you (omit to stay anonymous)")
	flags.StringVar(&opts.Endpoint, "endpoint", "", "submission endpoint (default: PROOFSWE_API_URL or hosted proofswe API)")
	flags.StringVar(&opts.Token, "token", "", "optional proofswe API token (default: PROOFSWE_API_TOKEN)")
	flags.Var(&repos, "repo", "select a repository path; repeat for multiple repos")
	flags.BoolVar(&opts.All, "all", false, "select every discovered repository without prompting")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "show selected uploads without posting")
	flags.BoolVar(&opts.Force, "force", false, "submit even if a task is not fully reproducible")
	flags.BoolVar(&opts.AcceptCodePublicationAgreement, "accept-code-publication-agreement", false, "confirm you have the right to publish captured raw code to the public corpus")
	flags.BoolVar(&opts.Wait, "wait", true, "poll each submission until the server scorecard is ready")
	flags.BoolFunc("no-wait", "submit and return immediately without polling", func(string) error {
		opts.Wait = false
		return nil
	})
	flags.DurationVar(&opts.WaitTimeout, "wait-timeout", 2*time.Minute, "maximum time to wait for each server scorecard")
	flags.DurationVar(&opts.PollInterval, "poll-interval", 2*time.Second, "delay between submission status polls")
	flags.IntVar(&opts.BatchSize, "batch-size", 10, "number of selected transcripts per upload batch")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%w: upload does not accept positional arguments", ErrUsage)
	}
	opts.Repos = repos
	return runUpload(ctx, cfg, opts)
}

func runUpload(ctx context.Context, cfg Config, opts uploadOptions) error {
	cfg = cfg.withDefaults()
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}
	if opts.BatchSize <= 0 {
		return fmt.Errorf("%w: --batch-size must be positive", ErrUsage)
	}
	groups, err := discoverUploadRepoGroups(ctx, cfg, opts.Harness)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return fmt.Errorf("no supported transcripts with repository provenance found")
	}

	selected, err := selectUploadTranscripts(cfg, groups, opts)
	if err != nil {
		return err
	}
	summary := uploadSummary{Discovered: countUploadTranscripts(groups), Selected: len(selected)}
	if len(selected) == 0 {
		printUploadSummary(cfg.Stdout, summary)
		return nil
	}
	if opts.DryRun {
		for _, item := range selected {
			_, _ = fmt.Fprintf(cfg.Stdout, "would upload %s %s (%s)\n", item.Harness, item.Path, item.RepoRoot)
		}
		printUploadSummary(cfg.Stdout, summary)
		return nil
	}

	opts.Endpoint = submitEndpoint(cfg, opts.Endpoint)
	opts.Token = firstNonEmpty(opts.Token, getenvOrEmpty(cfg, "PROOFSWE_API_TOKEN"))
	for start := 0; start < len(selected); start += opts.BatchSize {
		end := start + opts.BatchSize
		if end > len(selected) {
			end = len(selected)
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "\nBatch %d-%d of %d\n", start+1, end, len(selected))
		for _, item := range selected[start:end] {
			if err := uploadOneTranscript(ctx, cfg, opts, item); err != nil {
				summary.Failed++
				_, _ = fmt.Fprintf(cfg.Stderr, "failed %s: %v\n", item.Path, err)
				continue
			}
			summary.Submitted++
		}
	}
	printUploadSummary(cfg.Stdout, summary)
	if summary.Failed > 0 {
		return fmt.Errorf("bulk upload completed with %d failure(s)", summary.Failed)
	}
	return nil
}

func discoverUploadRepoGroups(ctx context.Context, cfg Config, harness string) ([]uploadRepoGroup, error) {
	if harness != "" && harness != "claudecode" && harness != "codex" {
		return nil, fmt.Errorf("%w: unknown harness %q", ErrUsage, harness)
	}
	byRepo := map[string][]uploadTranscript{}
	if harness == "" || harness == "claudecode" {
		root := filepath.Join(cfg.HomeDir, ".claude")
		transcripts, err := claudecode.Discover(root)
		if err == nil {
			for _, transcript := range transcripts {
				item := uploadTranscriptFromPath(ctx, "claudecode", transcript.Path, transcript.RepoPath)
				if item.RepoRoot == "" {
					continue
				}
				item.SessionID = string(transcript.SessionID)
				byRepo[item.RepoRoot] = append(byRepo[item.RepoRoot], item)
			}
		}
	}
	if harness == "" || harness == "codex" {
		root := firstNonEmpty(getenvOrEmpty(cfg, "CODEX_HOME"), filepath.Join(cfg.HomeDir, ".codex"))
		transcripts, err := codex.Discover(root)
		if err == nil {
			for _, transcript := range transcripts {
				item := uploadTranscriptFromPath(ctx, "codex", transcript.Path, "")
				if item.RepoRoot == "" {
					continue
				}
				item.SessionID = string(transcript.SessionID)
				byRepo[item.RepoRoot] = append(byRepo[item.RepoRoot], item)
			}
		}
	}
	groups := make([]uploadRepoGroup, 0, len(byRepo))
	for repo, transcripts := range byRepo {
		sortUploadTranscripts(transcripts)
		groups = append(groups, uploadRepoGroup{RepoRoot: repo, Transcripts: transcripts})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].RepoRoot < groups[j].RepoRoot
	})
	return groups, nil
}

func uploadTranscriptFromPath(ctx context.Context, harness, path, fallbackRepoPath string) uploadTranscript {
	item := submitCandidate(harness, path)
	out := uploadTranscript{Harness: harness, Path: item.path, ModTime: item.modTime}
	if cwd := transcriptCWD(harness, path); cwd != "" {
		if root, ok := gitRepoRootContext(ctx, cwd); ok {
			out.RepoRoot = canonicalSubmitPath(root)
			return out
		}
	}
	if fallbackRepoPath != "" {
		if root, ok := gitRepoRootContext(ctx, fallbackRepoPath); ok {
			out.RepoRoot = canonicalSubmitPath(root)
		}
	}
	return out
}

func selectUploadTranscripts(cfg Config, groups []uploadRepoGroup, opts uploadOptions) ([]uploadTranscript, error) {
	if len(opts.Repos) > 0 || opts.All {
		return selectUploadTranscriptsByFlags(groups, opts.Repos, opts.All), nil
	}
	if !isTTY(cfg.Stdin) {
		return nil, fmt.Errorf("%w: upload requires --repo or --all when stdin is not interactive", ErrUsage)
	}
	repoIndexes, err := promptUploadRepoSelection(cfg, groups)
	if err != nil {
		return nil, err
	}
	var selected []uploadTranscript
	for _, index := range repoIndexes {
		transcripts, err := promptUploadTranscriptDeselection(cfg, groups[index])
		if err != nil {
			return nil, err
		}
		selected = append(selected, transcripts...)
	}
	sortUploadTranscripts(selected)
	return selected, nil
}

func selectUploadTranscriptsByFlags(groups []uploadRepoGroup, repos []string, all bool) []uploadTranscript {
	repoSet := map[string]bool{}
	for _, repo := range repos {
		repoSet[canonicalSubmitPath(repo)] = true
	}
	var selected []uploadTranscript
	for _, group := range groups {
		if all || repoSet[group.RepoRoot] {
			selected = append(selected, group.Transcripts...)
		}
	}
	sortUploadTranscripts(selected)
	return selected
}

func promptUploadRepoSelection(cfg Config, groups []uploadRepoGroup) ([]int, error) {
	_, _ = fmt.Fprintln(cfg.Stdout, "Select repositories to upload:")
	for i, group := range groups {
		_, _ = fmt.Fprintf(cfg.Stdout, "  %d. %s (%d transcript(s))\n", i+1, group.RepoRoot, len(group.Transcripts))
	}
	_, _ = fmt.Fprint(cfg.Stdout, "Repositories [all, comma list, ranges, q]: ")
	line, err := readPromptLine(cfg.Stdin)
	if err != nil {
		return nil, err
	}
	return parseUploadSelection(line, len(groups), true)
}

func promptUploadTranscriptDeselection(cfg Config, group uploadRepoGroup) ([]uploadTranscript, error) {
	_, _ = fmt.Fprintf(cfg.Stdout, "\n%s\n", group.RepoRoot)
	for i, item := range group.Transcripts {
		label := item.SessionID
		if label == "" {
			label = filepath.Base(item.Path)
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "  %d. [%s] %s %s\n", i+1, item.Harness, label, item.ModTime.Format(time.RFC3339))
	}
	_, _ = fmt.Fprint(cfg.Stdout, "Deselect transcripts [enter keeps all, comma list, ranges, q skips repo]: ")
	line, err := readPromptLine(cfg.Stdin)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(strings.ToLower(line))
	if trimmed == "" {
		return append([]uploadTranscript(nil), group.Transcripts...), nil
	}
	if trimmed == "q" || trimmed == "quit" || trimmed == "none" {
		return nil, nil
	}
	deselected, err := parseUploadSelection(line, len(group.Transcripts), false)
	if err != nil {
		return nil, err
	}
	drop := map[int]bool{}
	for _, index := range deselected {
		drop[index] = true
	}
	selected := make([]uploadTranscript, 0, len(group.Transcripts)-len(drop))
	for i, item := range group.Transcripts {
		if !drop[i] {
			selected = append(selected, item)
		}
	}
	return selected, nil
}

func parseUploadSelection(input string, max int, allowAll bool) ([]int, error) {
	trimmed := strings.TrimSpace(strings.ToLower(input))
	if trimmed == "" {
		return nil, nil
	}
	if trimmed == "q" || trimmed == "quit" {
		return nil, nil
	}
	if allowAll && (trimmed == "a" || trimmed == "all" || trimmed == "*") {
		out := make([]int, max)
		for i := range out {
			out[i] = i
		}
		return out, nil
	}
	set := map[int]bool{}
	for _, part := range strings.Split(trimmed, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			start, err := parseUploadSelectionNumber(lo, max)
			if err != nil {
				return nil, err
			}
			end, err := parseUploadSelectionNumber(hi, max)
			if err != nil {
				return nil, err
			}
			if end < start {
				return nil, fmt.Errorf("%w: invalid range %q", ErrUsage, part)
			}
			for i := start; i <= end; i++ {
				set[i] = true
			}
			continue
		}
		index, err := parseUploadSelectionNumber(part, max)
		if err != nil {
			return nil, err
		}
		set[index] = true
	}
	out := make([]int, 0, len(set))
	for index := range set {
		out = append(out, index)
	}
	sort.Ints(out)
	return out, nil
}

func parseUploadSelectionNumber(value string, max int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 || n > max {
		return 0, fmt.Errorf("%w: selection %q is out of range", ErrUsage, value)
	}
	return n - 1, nil
}

func uploadOneTranscript(ctx context.Context, cfg Config, opts uploadOptions, item uploadTranscript) error {
	submitCfg := cfg
	submitCfg.WorkDir = item.RepoRoot
	task, err := buildSubmitTask(ctx, submitCfg, item.Harness, item.Path, opts.Handle)
	if err != nil {
		return err
	}
	if err := applyCodePublicationAgreement(submitCfg, &task, opts.AcceptCodePublicationAgreement); err != nil {
		return err
	}
	if problems := corpus.ReproducibilityProblems(task); len(problems) > 0 {
		if !opts.Force {
			return fmt.Errorf("not a reproducible corpus task: %v", problems)
		}
	}
	resp, err := submitTask(ctx, opts.Endpoint, opts.Token, submitRequest{
		SchemaVersion: submitSchemaVersion,
		ClientVersion: cfg.Version,
		Task:          task,
	})
	if err != nil {
		return err
	}
	if opts.Wait && resp.SubmissionID != "" && isPendingSubmissionStatus(resp.Status) && (resp.Scorecard == nil || isPendingPublishStatus(resp.Status)) {
		if polled, err := pollSubmission(ctx, opts.Endpoint, opts.Token, resp, opts.WaitTimeout, opts.PollInterval); err == nil {
			resp = polled
		} else {
			_, _ = fmt.Fprintf(cfg.Stderr, "proofswe upload: score polling stopped for %s: %v\n", item.Path, err)
		}
	}
	status := firstNonEmpty(resp.Status, "submitted")
	_, _ = fmt.Fprintf(cfg.Stdout, "uploaded %s · %s\n", filepath.Base(item.Path), status)
	return nil
}

func readPromptLine(r io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return b.String(), nil
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			if err == io.EOF {
				return b.String(), nil
			}
			return "", err
		}
	}
}

func sortUploadTranscripts(transcripts []uploadTranscript) {
	sort.Slice(transcripts, func(i, j int) bool {
		if transcripts[i].RepoRoot != transcripts[j].RepoRoot {
			return transcripts[i].RepoRoot < transcripts[j].RepoRoot
		}
		if !transcripts[i].ModTime.Equal(transcripts[j].ModTime) {
			return transcripts[i].ModTime.After(transcripts[j].ModTime)
		}
		return transcripts[i].Path < transcripts[j].Path
	})
}

func countUploadTranscripts(groups []uploadRepoGroup) int {
	var total int
	for _, group := range groups {
		total += len(group.Transcripts)
	}
	return total
}

func printUploadSummary(w io.Writer, summary uploadSummary) {
	_, _ = fmt.Fprintf(w, "\nproofswe upload · selected %d / discovered %d · submitted %d · skipped %d · failed %d\n",
		summary.Selected, summary.Discovered, summary.Submitted, summary.Skipped, summary.Failed)
}

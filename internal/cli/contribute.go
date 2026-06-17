package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/corpus"
	"github.com/Atharva-Kanherkar/proofswe/internal/hashing"
	"github.com/Atharva-Kanherkar/proofswe/internal/redact"
)

// runContributeCommand turns a captured session into a reproducible corpus task
// and writes a publishable task.json.
//
//	proofswe contribute <transcript.jsonl> [--harness=…]
//	    [--as=@handle] [--out=task.json] [--print] [--force]
//
// Reproducibility requires a public remote, a base commit, and a permissive
// license: only then can a third party clone the starting state and re-run the
// task against another model. Sessions that don't qualify are refused (the
// corpus is the reproducible subset; private-repo work contributes aggregate
// stats elsewhere). Secrets are scrubbed on the way out — the one filter the
// corpus keeps, to protect the dataset's survival, not anyone's privacy.
func runContributeCommand(cfg Config, args []string) error {
	flags := flag.NewFlagSet("contribute", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var harness, outPath, handle string
	var printJSON, force bool
	flags.StringVar(&harness, "harness", "", "claudecode|codex (auto-detected if empty)")
	flags.StringVar(&handle, "as", "", "optional attribution, e.g. @you (omit to stay anonymous)")
	flags.StringVar(&outPath, "out", "", "write the task.json here (default: ./<task-id>.json)")
	flags.BoolVar(&printJSON, "print", false, "print the task.json to stdout instead of writing a file")
	flags.BoolVar(&force, "force", false, "emit even if the task is not fully reproducible (NOT corpus-eligible)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("%w: contribute requires exactly one transcript path", ErrUsage)
	}
	path := flags.Arg(0)

	if harness == "" {
		harness = detectHarness(path)
	}
	if harness != "claudecode" && harness != "codex" {
		return fmt.Errorf("%w: unknown harness %q", ErrUsage, harness)
	}

	ctx := context.Background()
	captured, err := buildContributionTask(ctx, cfg, harness, path)
	if err != nil {
		return err
	}

	result, _, _, err := scoreTranscript(cfg, harness, path, false, judgeOptions{})
	if err != nil {
		return err
	}
	extracted := extractTranscriptSignals(harness, path)
	_, landed, _ := successFactsFromExtracted(extracted)

	taskID := contributionTaskID(captured)
	task := corpus.FromCapture(captured, extracted, landed, &result, taskID, strings.TrimSpace(handle), time.Now())

	if problems := corpus.ReproducibilityProblems(task); len(problems) > 0 {
		printContributionProblems(cfg.Stderr, problems)
		if !force {
			return fmt.Errorf("not a reproducible corpus task (use --force to emit anyway, but it cannot be re-run)")
		}
	}

	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("encode task: %w", err)
	}
	data = append(data, '\n')

	if printJSON {
		_, err = cfg.Stdout.Write(data)
		return err
	}

	if outPath == "" {
		outPath = shortTaskID(taskID) + ".json"
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	printContributionSummary(cfg.Stdout, task, outPath)
	return nil
}

// buildContributionTask reconstructs a full-content, scrubbed core.Task from a
// transcript plus the current repo. Unlike the capture hook it intentionally
// uses the full consent tier and does NOT apply per-repo opt-out downgrades:
// running `contribute` is itself the explicit, deliberate consent to publish.
func buildContributionTask(ctx context.Context, cfg Config, harness, path string) (core.Task, error) {
	// Hashes are irrelevant to the published corpus, so an ephemeral salt keeps
	// this self-contained without touching the proofswe state dir.
	salt := []byte("proofswe-contribute")
	h := hashing.New(salt)
	cwd := cfg.WorkDir

	transcript := extractTaskTranscript(harness, path, salt)
	full := core.CategoriesForTier(core.ConsentTierFull)

	info := collectRepoInfo(ctx, cwd, h)
	repo := info.repo
	var added []lineRef
	if info.root != "" {
		added, _ = gitAddedLinesContext(ctx, info.root)
	}

	task := core.Task{
		TaskSchemaVersion: core.TaskSchemaVersion,
		Harness:           core.HarnessName(harness),
		AdapterVersion:    harness + "/1",
		CapturedAt:        time.Now().UTC(),
		ConsentTier:       core.ConsentTierFull,
		Model:             core.TaskModel{ID: core.ModelId(transcript.model)},
		Repo:              repo,
	}

	report := redact.Report{ScrubberVersion: redact.ScrubberVersion, BestEffortNotice: redact.BestEffortNotice}
	task.Prompts, report = buildPromptRecords(transcript.prompts, h, full, report)
	task.Trajectory, report = buildTrajectoryRecords(transcript, h, full, report)
	task.Code, report = buildCodeRecord(added, h, full, repoAllowsRawCode(repo), report)
	task.RedactionReport = core.RedactionReport{
		ScrubberVersion:  redact.ScrubberVersion,
		SpansRedacted:    report.SpansRedacted,
		ByCategory:       report.ByCategory,
		BestEffortNotice: redact.BestEffortNotice,
	}
	return core.ProjectWithCategories(task, core.ConsentTierFull, full), nil
}

// contributionTaskID derives a stable, content-addressed id from the starting
// state and the task statement, so the same session dedupes to one corpus entry.
func contributionTaskID(task core.Task) string {
	starting := ""
	if len(task.Prompts) > 0 {
		starting = task.Prompts[0].Text
	}
	sum := sha256.Sum256([]byte(task.Repo.RemoteURL + "\x00" + task.Repo.BaseCommit + "\x00" + starting))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func shortTaskID(taskID string) string {
	id := strings.TrimPrefix(taskID, "sha256:")
	if len(id) > 16 {
		id = id[:16]
	}
	return "task-" + id
}

func printContributionProblems(w io.Writer, problems []string) {
	_, _ = fmt.Fprintln(w, "\nthis session is not a reproducible corpus task:")
	for _, p := range problems {
		_, _ = fmt.Fprintf(w, "  ✗ %s\n", p)
	}
	_, _ = fmt.Fprintln(w, "  → the corpus is the reproducible OSS subset; private-repo work isn't re-runnable by others.")
}

func printContributionSummary(w io.Writer, task corpus.Task, outPath string) {
	_, _ = fmt.Fprintf(w, "\nwrote %s\n\n", outPath)
	_, _ = fmt.Fprintf(w, "  repo      %s @ %s\n", task.Repo.RemoteURL, shortCommit(task.Repo.BaseCommit))
	_, _ = fmt.Fprintf(w, "  license   %s\n", task.Repo.LicenseSPDX)
	_, _ = fmt.Fprintf(w, "  model     %s\n", task.Model)
	_, _ = fmt.Fprintf(w, "  prompts   %d   transcript %d msgs / %d tool calls\n",
		len(task.Prompts), len(task.Transcript.AssistantMessages), len(task.Transcript.ToolCalls))
	if task.Scorecard != nil {
		_, _ = fmt.Fprintf(w, "  score     %.0f / 100\n", task.Scorecard.Composite)
	}
	_, _ = fmt.Fprintf(w, "  scrubbed  %d secret span(s) redacted\n", task.Scrub.SpansRedacted)
	_, _ = fmt.Fprintln(w, "\n  submit it to the corpus:")
	_, _ = fmt.Fprintf(w, "    gh repo fork %s --clone --remote && \\\n", corpusRepoSlug)
	_, _ = fmt.Fprintf(w, "    cp %s <fork>/tasks/ && cd <fork> && \\\n", outPath)
	_, _ = fmt.Fprintf(w, "    git checkout -b task-%s && git add tasks && \\\n", shortCommit(task.Repo.BaseCommit))
	_, _ = fmt.Fprintln(w, "    git commit -m 'add task' && gh pr create --fill")
	_, _ = fmt.Fprintln(w)
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

// corpusRepoSlug is the public corpus repository contributions target.
const corpusRepoSlug = "Atharva-Kanherkar/proofswe-corpus"

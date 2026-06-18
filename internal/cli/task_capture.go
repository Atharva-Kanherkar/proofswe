package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/hashing"
	"github.com/Atharva-Kanherkar/proofswe/internal/reader"
	"github.com/Atharva-Kanherkar/proofswe/internal/redact"
)

var scrubText = redact.Scrub

type taskTranscript struct {
	prompts           []string
	assistantMessages []string
	toolCalls         []namedText
	toolOutputs       []namedText
	tools             []string
	model             string
}

type namedText struct {
	name string
	text string
}

func captureTaskRecord(ctx context.Context, cfg Config, harness string, in hookInput, now time.Time, salt []byte, resolved consentResolution) (core.Task, consentResolution, error) {
	h := hashing.New(salt)
	cwd := in.CWD
	if cwd == "" {
		cwd = cfg.WorkDir
	}
	transcript := extractTaskTranscript(harness, in.TranscriptPath, salt)
	meta := sessionMetadata(harness, in, salt)
	if transcript.model != "" {
		meta.model = transcript.model
	}

	repo := core.TaskRepo{LockfileHashes: lockfileHashes(cwd, h)}
	var repoRoot string
	var added []lineRef
	if categoryAllowed(resolved.Categories, core.CategoryRepoLinkage) || categoryAllowed(resolved.Categories, core.CategoryCodeDiffs) {
		info := collectRepoInfo(ctx, cwd, h)
		repo = info.repo
		repo.LockfileHashes = mergeStringMaps(repo.LockfileHashes, lockfileHashes(info.root, h))
		repoRoot = info.root
		if repo.RemoteHash != "" {
			var err error
			resolved, err = effectiveConsent(cfg, repo.RemoteHash)
			if err != nil {
				return core.Task{}, resolved, err
			}
		}
		if categoryAllowed(resolved.Categories, core.CategoryCodeDiffs) && repoRoot != "" {
			// Best-effort: nil on error leaves the code record empty.
			added, _ = gitAddedLinesContext(ctx, repoRoot)
		}
	}

	taskID := core.DeterministicTaskID(salt, repo.RemoteURL, core.SessionId(in.SessionID))
	task := core.Task{
		TaskSchemaVersion: core.TaskSchemaVersion,
		TaskID:            taskID,
		Harness:           core.HarnessName(harness),
		AdapterVersion:    harness + "/1",
		CapturedAt:        now.UTC(),
		ConsentTier:       resolved.Tier,
		Session: core.TaskSession{
			IDHash: h.StringHash(in.SessionID),
			ID:     core.SessionId(in.SessionID),
		},
		Model: core.TaskModel{ID: core.ModelId(meta.model)},
		Repo:  repo,
		Environment: core.TaskEnvironment{
			OS:        runtime.GOOS,
			Toolchain: map[string]string{"go": runtime.Version()},
			Tools:     sortedDistinct(transcript.tools),
		},
		SpecSignals: core.TaskSpecSignals{
			StartingPromptWords:       wordCount(firstString(transcript.prompts)),
			PromptCount:               len(transcript.prompts),
			ReferencesExternalContext: referencesExternalContext(transcript.prompts),
		},
		RedactionReport: core.RedactionReport{
			ScrubberVersion:  redact.ScrubberVersion,
			ByCategory:       map[string]int{},
			BestEffortNotice: redact.BestEffortNotice,
		},
	}

	report := redact.Report{ScrubberVersion: redact.ScrubberVersion, BestEffortNotice: redact.BestEffortNotice}
	task.Prompts, report = buildPromptRecords(transcript.prompts, h, resolved.Categories, report)
	task.Trajectory, report = buildTrajectoryRecords(transcript, h, resolved.Categories, report)
	task.Code, report = buildCodeRecord(added, h, resolved.Categories, repoAllowsRawCode(repo), report)
	task.RedactionReport = core.RedactionReport{
		ScrubberVersion:  redact.ScrubberVersion,
		SpansRedacted:    report.SpansRedacted,
		ByCategory:       report.ByCategory,
		BestEffortNotice: redact.BestEffortNotice,
	}
	if task.RedactionReport.ByCategory == nil {
		task.RedactionReport.ByCategory = map[string]int{}
	}
	task = core.ProjectWithCategories(task, resolved.Tier, resolved.Categories)
	return task, resolved, nil
}

func writeCapturedTask(cfg Config, task core.Task) error {
	data, err := marshalTaskJSON(task)
	if err != nil {
		return err
	}
	path, err := taskRecordPath(cfg, task.TaskID)
	if err != nil {
		return err
	}
	return writeTaskFileAtomic(path, data)
}

func buildPromptRecords(prompts []string, h hashing.Hasher, categories []core.ConsentCategory, report redact.Report) ([]core.TaskPrompt, redact.Report) {
	out := make([]core.TaskPrompt, 0, len(prompts))
	allowAll := categoryAllowed(categories, core.CategoryAllPrompts) || categoryAllowed(categories, core.CategoryFullTranscript)
	allowStart := categoryAllowed(categories, core.CategoryStartingPrompt) || allowAll
	for i, prompt := range prompts {
		text := ""
		if allowAll || (allowStart && i == 0) {
			var scrubReport redact.Report
			text, scrubReport = scrubText(prompt)
			report = redact.MergeReports(report, scrubReport)
		}
		out = append(out, core.TaskPrompt{
			TurnIndex: i,
			Role:      "user",
			TextHash:  h.StringHash(prompt),
			Text:      text,
		})
	}
	return out, report
}

func buildTrajectoryRecords(transcript taskTranscript, h hashing.Hasher, categories []core.ConsentCategory, report redact.Report) (core.TaskTrajectory, redact.Report) {
	var trajectory core.TaskTrajectory
	for i, text := range transcript.assistantMessages {
		clear := ""
		if categoryAllowed(categories, core.CategoryAssistantMsgs) || categoryAllowed(categories, core.CategoryFullTranscript) {
			var scrubReport redact.Report
			clear, scrubReport = scrubText(text)
			report = redact.MergeReports(report, scrubReport)
		}
		trajectory.AssistantMessages = append(trajectory.AssistantMessages, core.TaskText{TurnIndex: i, TextHash: h.StringHash(text), Text: clear})
	}
	for i, call := range transcript.toolCalls {
		clear := ""
		if categoryAllowed(categories, core.CategoryToolCalls) || categoryAllowed(categories, core.CategoryFullTranscript) {
			var scrubReport redact.Report
			clear, scrubReport = scrubText(call.text)
			report = redact.MergeReports(report, scrubReport)
		}
		trajectory.ToolCalls = append(trajectory.ToolCalls, core.TaskText{TurnIndex: i, Name: call.name, TextHash: h.StringHash(call.text), Text: clear})
	}
	for i, output := range transcript.toolOutputs {
		clear := ""
		if categoryAllowed(categories, core.CategoryToolOutputs) || categoryAllowed(categories, core.CategoryFullTranscript) {
			var scrubReport redact.Report
			clear, scrubReport = scrubText(output.text)
			report = redact.MergeReports(report, scrubReport)
		}
		trajectory.ToolOutputs = append(trajectory.ToolOutputs, core.TaskText{TurnIndex: i, Name: output.name, TextHash: h.StringHash(output.text), Text: clear})
	}
	return trajectory, report
}

func buildCodeRecord(added []lineRef, h hashing.Hasher, categories []core.ConsentCategory, codeAllowed bool, report redact.Report) (core.TaskCode, redact.Report) {
	// Group added lines by file in first-seen order. `added` is the SAME set
	// keeprate uses (working-tree diff vs HEAD *plus* untracked new files), so the
	// reconstructed content captures new-file bodies that `git diff HEAD` omits.
	order := make([]string, 0)
	byPath := map[string][]string{}
	files := make([]core.TaskFile, 0)
	for _, ref := range added {
		if ref.path == "" {
			continue
		}
		if _, ok := byPath[ref.path]; !ok {
			order = append(order, ref.path)
			files = append(files, core.TaskFile{PathHash: h.StringHash(ref.path), Path: ref.path, Role: core.ClassifyFileRole(ref.path)})
		}
		byPath[ref.path] = append(byPath[ref.path], ref.text)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].PathHash < files[j].PathHash })

	code := core.TaskCode{Files: files}
	if len(order) == 0 || !codeAllowed || (!categoryAllowed(categories, core.CategoryCodeDiffs) && !categoryAllowed(categories, core.CategoryFullTranscript)) {
		return code, report
	}

	var solution, test strings.Builder
	for _, p := range order {
		b := &solution
		if core.ClassifyFileRole(p) == core.FileRoleTest {
			b = &test
		}
		fmt.Fprintf(b, "+++ b/%s\n", p)
		for _, line := range byPath[p] {
			b.WriteString("+")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	var scrubReport redact.Report
	code.Patch, scrubReport = scrubText(solution.String())
	report = redact.MergeReports(report, scrubReport)
	code.TestPatch, scrubReport = scrubText(test.String())
	report = redact.MergeReports(report, scrubReport)
	return code, report
}

func extractTaskTranscript(harness, path string, salt []byte) taskTranscript {
	var out taskTranscript
	if path == "" {
		return out
	}
	if events, err := parseTranscript(harness, salt, path); err == nil {
		for _, event := range events {
			env := eventEnvelope(event)
			if env.Model.ID != "" {
				out.model = string(env.Model.ID)
			}
			if call, ok := event.(*core.ToolCall); ok && call.Name != "" {
				out.tools = append(out.tools, call.Name)
			}
		}
	}

	file, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = file.Close() }()
	_, _ = reader.ReadNewLines(file, 0, reader.Options{}, func(line []byte, _ int64) error {
		extractTaskLine(harness, line, &out)
		return nil
	})
	return out
}

func extractTaskLine(harness string, line []byte, out *taskTranscript) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return
	}
	switch harness {
	case "claudecode":
		extractClaudeTaskLine(raw, out)
	case "codex":
		extractCodexTaskLine(raw, out)
	}
}

func extractClaudeTaskLine(raw map[string]any, out *taskTranscript) {
	typ, _ := raw["type"].(string)
	msg, _ := raw["message"].(map[string]any)
	switch typ {
	case "user":
		// Claude tool results are user records whose content is a tool_result
		// block array — capture those as tool outputs, not prompts.
		if outputs := toolResults(msg["content"]); len(outputs) > 0 {
			out.toolOutputs = append(out.toolOutputs, outputs...)
			return
		}
		if text := contentText(msg["content"]); text != "" {
			out.prompts = append(out.prompts, text)
		}
	case "assistant":
		if model, _ := msg["model"].(string); model != "" {
			out.model = model
		}
		if text := contentText(msg["content"]); text != "" {
			out.assistantMessages = append(out.assistantMessages, text)
		}
		for _, call := range toolUses(msg["content"]) {
			out.toolCalls = append(out.toolCalls, call)
			out.tools = append(out.tools, call.name)
		}
	}
}

func extractCodexTaskLine(raw map[string]any, out *taskTranscript) {
	typ, _ := raw["type"].(string)
	payload, _ := raw["payload"].(map[string]any)
	if typ == "turn_context" {
		if model, _ := payload["model"].(string); model != "" {
			out.model = model
		}
	}
	if typ != "response_item" {
		return
	}
	itemType, _ := payload["type"].(string)
	switch itemType {
	case "message":
		role, _ := payload["role"].(string)
		text := contentText(payload["content"])
		if role == "user" && text != "" {
			out.prompts = append(out.prompts, text)
		}
		if role == "assistant" && text != "" {
			out.assistantMessages = append(out.assistantMessages, text)
		}
	case "function_call":
		name, _ := payload["name"].(string)
		text := stringifyJSON(payload["arguments"])
		out.toolCalls = append(out.toolCalls, namedText{name: name, text: text})
		out.tools = append(out.tools, name)
	case "function_call_output":
		out.toolOutputs = append(out.toolOutputs, namedText{text: stringifyJSON(payload["output"])})
	}
}

func contentText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			m, _ := item.(map[string]any)
			for _, key := range []string{"text", "input_text", "output_text"} {
				if text, _ := m[key].(string); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func toolUses(value any) []namedText {
	items, _ := value.([]any)
	var out []namedText
	for _, item := range items {
		m, _ := item.(map[string]any)
		if typ, _ := m["type"].(string); typ != "tool_use" {
			continue
		}
		name, _ := m["name"].(string)
		out = append(out, namedText{name: name, text: stringifyJSON(m["input"])})
	}
	return out
}

func toolResults(value any) []namedText {
	items, _ := value.([]any)
	var out []namedText
	for _, item := range items {
		m, _ := item.(map[string]any)
		if typ, _ := m["type"].(string); typ != "tool_result" {
			continue
		}
		out = append(out, namedText{text: contentText(m["content"])})
	}
	return out
}

func stringifyJSON(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

type repoInfo struct {
	root string
	repo core.TaskRepo
}

func collectRepoInfo(ctx context.Context, cwd string, h hashing.Hasher) repoInfo {
	root, ok := gitRepoRootContext(ctx, cwd)
	if !ok {
		return repoInfo{repo: core.TaskRepo{LockfileHashes: lockfileHashes(cwd, h)}}
	}
	repo := core.TaskRepo{LockfileHashes: lockfileHashes(root, h)}
	if out, err := runGitContext(ctx, root, "remote", "get-url", "origin"); err == nil {
		repo.RemoteURL = strings.TrimSpace(string(out))
		repo.RemoteHash = h.StringHash(repo.RemoteURL)
		repo.IsPublic = isPublicRemote(repo.RemoteURL)
	}
	if out, err := runGitContext(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		repo.BaseCommit = strings.TrimSpace(string(out))
		if ts, tsErr := runGitContext(ctx, root, "show", "-s", "--format=%cI", "HEAD"); tsErr == nil {
			repo.BaseCommitCommittedAt = strings.TrimSpace(string(ts))
		}
	}
	if out, err := runGitContext(ctx, root, "branch", "--show-current"); err == nil {
		repo.Branch = strings.TrimSpace(string(out))
	}
	if out, err := runGitContext(ctx, root, "status", "--porcelain"); err == nil {
		repo.Dirty = strings.TrimSpace(string(out)) != ""
	}
	repo.LicenseSPDX = detectLicenseSPDX(root)
	return repoInfo{root: root, repo: repo}
}

func lockfileHashes(root string, h hashing.Hasher) map[string]string {
	if root == "" {
		return map[string]string{}
	}
	names := []string{"go.sum", "go.mod", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "Cargo.lock", "poetry.lock", "requirements.txt"}
	out := map[string]string{}
	for _, name := range names {
		data, err := readRepoFile(root, name)
		if err != nil || len(data) == 0 {
			continue
		}
		out[name] = h.StringHash(string(data))
	}
	return out
}

func detectLicenseSPDX(root string) string {
	for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"} {
		data, err := readRepoFile(root, name)
		if err != nil {
			continue
		}
		text := strings.ToLower(string(data))
		switch {
		case strings.Contains(text, "apache license") && strings.Contains(text, "version 2.0"):
			return "Apache-2.0"
		case strings.Contains(text, "mit license"):
			return "MIT"
		case strings.Contains(text, "bsd 2-clause"):
			return "BSD-2-Clause"
		case strings.Contains(text, "bsd 3-clause"):
			return "BSD-3-Clause"
		case strings.Contains(text, "isc license"):
			return "ISC"
		case strings.Contains(text, "the unlicense"):
			return "Unlicense"
		case strings.Contains(text, "0bsd"):
			return "0BSD"
		case strings.Contains(text, "gnu general public license") && strings.Contains(text, "affero"):
			return "AGPL-3.0"
		case strings.Contains(text, "gnu general public license"):
			return "GPL-3.0"
		case strings.Contains(text, "mozilla public license"):
			return "MPL-2.0"
		}
	}
	return ""
}

// repoAllowsRawCode reports whether raw added-line code may be published for
// this repo. Any public repo with a remote and a base commit qualifies; the
// license is recorded for provenance but no longer gates inclusion.
func repoAllowsRawCode(repo core.TaskRepo) bool {
	return repo.RemoteURL != "" && repo.BaseCommit != "" && repo.IsPublic
}

func isPublicRemote(remote string) bool {
	lower := strings.ToLower(strings.TrimSpace(remote))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "private") || strings.Contains(lower, "enterprise") || strings.Contains(lower, "corp") {
		return false
	}
	switch remoteHost(lower) {
	case "github.com", "gitlab.com", "codeberg.org":
		return true
	default:
		return false
	}
}

func remoteHost(remote string) string {
	if i := strings.Index(remote, "://"); i >= 0 {
		rest := remote[i+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		if slash := strings.Index(rest, "/"); slash >= 0 {
			rest = rest[:slash]
		}
		if colon := strings.Index(rest, ":"); colon >= 0 {
			rest = rest[:colon]
		}
		return rest
	}
	if at := strings.LastIndex(remote, "@"); at >= 0 {
		rest := remote[at+1:]
		if sep := strings.IndexAny(rest, ":/"); sep >= 0 {
			return rest[:sep]
		}
		return rest
	}
	if sep := strings.IndexAny(remote, ":/"); sep >= 0 {
		return remote[:sep]
	}
	return remote
}

func categoryAllowed(categories []core.ConsentCategory, category core.ConsentCategory) bool {
	for _, c := range categories {
		if c == category || c == core.CategoryFullTranscript {
			return true
		}
	}
	return false
}

func sortedDistinct(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mergeStringMaps(a, b map[string]string) map[string]string {
	if a == nil {
		a = map[string]string{}
	}
	for k, v := range b {
		a[k] = v
	}
	return a
}

func wordCount(text string) int {
	return len(strings.Fields(text))
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func referencesExternalContext(prompts []string) bool {
	for _, prompt := range prompts {
		lower := strings.ToLower(prompt)
		if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "see attached") || strings.Contains(lower, "screenshot") {
			return true
		}
	}
	return false
}

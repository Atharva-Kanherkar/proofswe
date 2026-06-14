package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const TaskSchemaVersion = 1

type ConsentTier string

const (
	ConsentTierHashesOnly ConsentTier = "hashes-only"
	ConsentTierPrompts    ConsentTier = "prompts"
	ConsentTierActions    ConsentTier = "actions"
	ConsentTierCode       ConsentTier = "code"
	ConsentTierFull       ConsentTier = "full"
)

type ConsentCategory string

const (
	CategoryStartingPrompt ConsentCategory = "starting-prompt"
	CategoryAllPrompts     ConsentCategory = "all-prompts"
	CategoryAssistantMsgs  ConsentCategory = "assistant-msgs"
	CategoryToolCalls      ConsentCategory = "tool-calls"
	CategoryToolOutputs    ConsentCategory = "tool-outputs"
	CategoryCodeDiffs      ConsentCategory = "code+diffs"
	CategoryRepoLinkage    ConsentCategory = "repo-linkage"
	CategoryFullTranscript ConsentCategory = "full-transcript"
)

var tierOrder = map[ConsentTier]int{
	ConsentTierHashesOnly: 0,
	ConsentTierPrompts:    1,
	ConsentTierActions:    2,
	ConsentTierCode:       3,
	ConsentTierFull:       4,
}

var orderedCategories = []ConsentCategory{
	CategoryStartingPrompt,
	CategoryAllPrompts,
	CategoryAssistantMsgs,
	CategoryToolCalls,
	CategoryToolOutputs,
	CategoryCodeDiffs,
	CategoryRepoLinkage,
	CategoryFullTranscript,
}

type Task struct {
	TaskSchemaVersion int             `json:"task_schema_version"`
	TaskID            string          `json:"task_id"`
	Harness           HarnessName     `json:"harness"`
	HarnessCLIVersion string          `json:"harness_cli_version,omitempty"`
	AdapterVersion    string          `json:"adapter_version"`
	CapturedAt        time.Time       `json:"captured_at"`
	ConsentTier       ConsentTier     `json:"consent_tier"`
	Session           TaskSession     `json:"session"`
	Model             TaskModel       `json:"model"`
	Repo              TaskRepo        `json:"repo"`
	Environment       TaskEnvironment `json:"environment"`
	SpecSignals       TaskSpecSignals `json:"spec_signals"`
	Prompts           []TaskPrompt    `json:"prompts,omitempty"`
	Trajectory        TaskTrajectory  `json:"trajectory,omitzero"`
	Code              TaskCode        `json:"code,omitzero"`
	RedactionReport   RedactionReport `json:"redaction_report"`
	Unknown           json.RawMessage `json:"-"`
}

type TaskSession struct {
	IDHash string    `json:"id_hash,omitempty"`
	ID     SessionId `json:"id,omitempty"`
}

type TaskModel struct {
	ID ModelId `json:"id,omitempty"`
}

type TaskRepo struct {
	RemoteHash            string            `json:"remote_hash,omitempty"`
	RemoteURL             string            `json:"remote_url,omitempty"`
	BaseCommit            string            `json:"base_commit,omitempty"`
	BaseCommitCommittedAt string            `json:"base_commit_committed_at,omitempty"`
	Branch                string            `json:"branch,omitempty"`
	Dirty                 bool              `json:"dirty,omitempty"`
	IsPublic              bool              `json:"is_public,omitempty"`
	LicenseSPDX           string            `json:"license_spdx,omitempty"`
	LockfileHashes        map[string]string `json:"lockfile_hashes,omitempty"`
}

type TaskEnvironment struct {
	OS             string            `json:"os,omitempty"`
	Toolchain      map[string]string `json:"toolchain,omitempty"`
	Tools          []string          `json:"tools,omitempty"`
	ApprovalPolicy string            `json:"approval_policy,omitempty"`
	SandboxPolicy  string            `json:"sandbox_policy,omitempty"`
}

type TaskSpecSignals struct {
	StartingPromptWords       int  `json:"starting_prompt_words,omitempty"`
	PromptCount               int  `json:"prompt_count,omitempty"`
	HadFailingTestAtBase      bool `json:"had_failing_test_at_base,omitempty"`
	ReferencesExternalContext bool `json:"references_external_context,omitempty"`
}

type TaskPrompt struct {
	TurnIndex int    `json:"turn_index"`
	Role      string `json:"role,omitempty"`
	TextHash  string `json:"text_hash,omitempty"`
	Text      string `json:"text,omitempty"`
}

type TaskTrajectory struct {
	AssistantMessages []TaskText `json:"assistant_messages,omitempty"`
	ToolCalls         []TaskText `json:"tool_calls,omitempty"`
	ToolOutputs       []TaskText `json:"tool_outputs,omitempty"`
}

type TaskText struct {
	TurnIndex int    `json:"turn_index,omitempty"`
	Name      string `json:"name,omitempty"`
	TextHash  string `json:"text_hash,omitempty"`
	Text      string `json:"text,omitempty"`
}

type TaskCode struct {
	Patch     string     `json:"patch,omitempty"`
	TestPatch string     `json:"test_patch,omitempty"`
	Files     []TaskFile `json:"files,omitempty"`
}

type TaskFile struct {
	PathHash string   `json:"path_hash,omitempty"`
	Path     string   `json:"path,omitempty"`
	Role     FileRole `json:"role,omitempty"`
}

type RedactionReport struct {
	ScrubberVersion  string         `json:"scrubber_version"`
	SpansRedacted    int            `json:"spans_redacted"`
	ByCategory       map[string]int `json:"by_category,omitempty"`
	BestEffortNotice string         `json:"best_effort_notice"`
}

type FileRole string

const (
	FileRoleUnknown  FileRole = "unknown"
	FileRoleSolution FileRole = "solution"
	FileRoleTest     FileRole = "test"
	FileRoleConfig   FileRole = "config"
	FileRoleLockfile FileRole = "lockfile"
)

func NormalizeConsentTier(tier ConsentTier) ConsentTier {
	if _, ok := tierOrder[tier]; ok {
		return tier
	}
	return ConsentTierHashesOnly
}

func ConsentTierRank(tier ConsentTier) int {
	return tierOrder[NormalizeConsentTier(tier)]
}

func MinConsentTier(a, b ConsentTier) ConsentTier {
	if ConsentTierRank(a) <= ConsentTierRank(b) {
		return NormalizeConsentTier(a)
	}
	return NormalizeConsentTier(b)
}

func MaxConsentTier(a, b ConsentTier) ConsentTier {
	if ConsentTierRank(a) >= ConsentTierRank(b) {
		return NormalizeConsentTier(a)
	}
	return NormalizeConsentTier(b)
}

func CategoriesForTier(tier ConsentTier) []ConsentCategory {
	switch NormalizeConsentTier(tier) {
	case ConsentTierPrompts:
		return append([]ConsentCategory(nil), orderedCategories[:2]...)
	case ConsentTierActions:
		return append([]ConsentCategory(nil), orderedCategories[:5]...)
	case ConsentTierCode:
		return append([]ConsentCategory(nil), orderedCategories[:7]...)
	case ConsentTierFull:
		return append([]ConsentCategory(nil), orderedCategories...)
	default:
		return nil
	}
}

func CategorySet(categories []ConsentCategory) map[ConsentCategory]bool {
	out := make(map[ConsentCategory]bool, len(categories))
	for _, category := range categories {
		for _, known := range orderedCategories {
			if category == known {
				out[category] = true
				break
			}
		}
	}
	return out
}

func Project(task Task, tier ConsentTier) Task {
	return ProjectWithCategories(task, tier, CategoriesForTier(tier))
}

func ProjectWithCategories(task Task, tier ConsentTier, categories []ConsentCategory) Task {
	task = cloneTask(task)
	tier = NormalizeConsentTier(tier)
	allowed := CategorySet(categories)
	task.TaskSchemaVersion = TaskSchemaVersion
	task.ConsentTier = tier

	allowAllPrompts := allowed[CategoryAllPrompts] || allowed[CategoryFullTranscript]
	allowStartingPrompt := allowed[CategoryStartingPrompt] || allowAllPrompts
	for i := range task.Prompts {
		allowText := allowAllPrompts || (allowStartingPrompt && i == firstPromptIndex(task.Prompts))
		if !allowText {
			task.Prompts[i].Text = ""
		}
	}

	if !(allowed[CategoryAssistantMsgs] || allowed[CategoryFullTranscript]) {
		blankTaskTexts(task.Trajectory.AssistantMessages)
	}
	if !(allowed[CategoryToolCalls] || allowed[CategoryFullTranscript]) {
		blankTaskTexts(task.Trajectory.ToolCalls)
	}
	if !(allowed[CategoryToolOutputs] || allowed[CategoryFullTranscript]) {
		blankTaskTexts(task.Trajectory.ToolOutputs)
	}

	if !(allowed[CategoryCodeDiffs] || allowed[CategoryFullTranscript]) {
		task.Code.Patch = ""
		task.Code.TestPatch = ""
		for i := range task.Code.Files {
			task.Code.Files[i].Path = ""
		}
	}
	if !(allowed[CategoryRepoLinkage] || allowed[CategoryFullTranscript]) {
		task.Repo.RemoteURL = ""
		task.Repo.BaseCommit = ""
		task.Repo.BaseCommitCommittedAt = ""
		task.Repo.Branch = ""
		task.Repo.Dirty = false
		task.Repo.IsPublic = false
		task.Repo.LicenseSPDX = ""
	}
	if !allowed[CategoryFullTranscript] {
		task.Session.ID = ""
	}

	if task.Repo.LockfileHashes == nil {
		task.Repo.LockfileHashes = map[string]string{}
	}
	if task.Environment.Toolchain == nil {
		task.Environment.Toolchain = map[string]string{}
	}
	if task.RedactionReport.ScrubberVersion == "" {
		task.RedactionReport.ScrubberVersion = "redact/1"
	}
	if task.RedactionReport.BestEffortNotice == "" {
		task.RedactionReport.BestEffortNotice = "automated redaction is best-effort and can miss secrets"
	}
	sort.Strings(task.Environment.Tools)
	return task
}

func cloneTask(task Task) Task {
	if task.Repo.LockfileHashes != nil {
		task.Repo.LockfileHashes = cloneStringMap(task.Repo.LockfileHashes)
	}
	if task.Environment.Toolchain != nil {
		task.Environment.Toolchain = cloneStringMap(task.Environment.Toolchain)
	}
	if task.Environment.Tools != nil {
		task.Environment.Tools = append([]string(nil), task.Environment.Tools...)
	}
	if task.Prompts != nil {
		task.Prompts = append([]TaskPrompt(nil), task.Prompts...)
	}
	if task.Trajectory.AssistantMessages != nil {
		task.Trajectory.AssistantMessages = append([]TaskText(nil), task.Trajectory.AssistantMessages...)
	}
	if task.Trajectory.ToolCalls != nil {
		task.Trajectory.ToolCalls = append([]TaskText(nil), task.Trajectory.ToolCalls...)
	}
	if task.Trajectory.ToolOutputs != nil {
		task.Trajectory.ToolOutputs = append([]TaskText(nil), task.Trajectory.ToolOutputs...)
	}
	if task.Code.Files != nil {
		task.Code.Files = append([]TaskFile(nil), task.Code.Files...)
	}
	if task.RedactionReport.ByCategory != nil {
		task.RedactionReport.ByCategory = cloneIntMap(task.RedactionReport.ByCategory)
	}
	if task.Unknown != nil {
		task.Unknown = append(json.RawMessage(nil), task.Unknown...)
	}
	return task
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstPromptIndex(prompts []TaskPrompt) int {
	for i := range prompts {
		return i
	}
	return -1
}

func blankTaskTexts(texts []TaskText) {
	for i := range texts {
		texts[i].Text = ""
	}
}

func DeterministicTaskID(salt []byte, remote string, sessionID SessionId) string {
	sum := sha256.New()
	_, _ = sum.Write(salt)
	_, _ = sum.Write([]byte(remote))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(sessionID))
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

func ClassifyFileRole(path string) FileRole {
	if unsafeRelativePath(path) {
		return FileRoleUnknown
	}
	clean := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(path, "\\", "/")))
	base := strings.ToLower(filepath.Base(clean))
	lower := strings.ToLower(clean)

	if isLockfile(base) {
		return FileRoleLockfile
	}
	if isConfigFile(base) || strings.HasPrefix(lower, ".github/") || strings.HasPrefix(lower, ".circleci/") {
		return FileRoleConfig
	}
	if strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/") ||
		strings.Contains(lower, "/spec/") ||
		strings.HasPrefix(lower, "test/") ||
		strings.HasPrefix(lower, "tests/") ||
		strings.HasPrefix(lower, "spec/") ||
		strings.Contains(base, "_test.") ||
		strings.Contains(base, ".test.") ||
		strings.Contains(base, "_spec.") ||
		strings.Contains(base, ".spec.") ||
		strings.HasPrefix(base, "test_") {
		return FileRoleTest
	}
	return FileRoleSolution
}

func unsafeRelativePath(path string) bool {
	if path == "" || strings.ContainsRune(path, 0) || filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return true
	}
	clean := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(path, "\\", "/")))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return true
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func isLockfile(base string) bool {
	switch base {
	case "go.sum", "cargo.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "poetry.lock", "pipfile.lock", "composer.lock", "gemfile.lock":
		return true
	default:
		return false
	}
}

func isConfigFile(base string) bool {
	switch base {
	case "go.mod", "package.json", "tsconfig.json", "vite.config.ts", "webpack.config.js", "makefile", "dockerfile", "docker-compose.yml", "docker-compose.yaml", ".golangci.yml", ".golangci.yaml", ".gitignore":
		return true
	default:
		return strings.HasSuffix(base, ".toml") || strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml")
	}
}

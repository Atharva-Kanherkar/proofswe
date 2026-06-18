package core

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"pgregory.net/rapid"
)

func TestProjectDropsHigherTierFields(t *testing.T) {
	base := sampleTask()
	tests := []struct {
		tier                ConsentTier
		wantPromptText      bool
		wantAssistantText   bool
		wantToolCallText    bool
		wantToolOutputText  bool
		wantCodeText        bool
		wantRepoLinkageText bool
		wantSessionID       bool
	}{
		{tier: ConsentTierHashesOnly},
		{tier: ConsentTierPrompts, wantPromptText: true},
		{tier: ConsentTierActions, wantPromptText: true, wantAssistantText: true, wantToolCallText: true, wantToolOutputText: true},
		{tier: ConsentTierCode, wantPromptText: true, wantAssistantText: true, wantToolCallText: true, wantToolOutputText: true, wantCodeText: true, wantRepoLinkageText: true},
		{tier: ConsentTierFull, wantPromptText: true, wantAssistantText: true, wantToolCallText: true, wantToolOutputText: true, wantCodeText: true, wantRepoLinkageText: true, wantSessionID: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.tier), func(t *testing.T) {
			got := Project(base, tt.tier)
			if (got.Prompts[0].Text != "") != tt.wantPromptText {
				t.Fatalf("prompt text presence = %t, want %t", got.Prompts[0].Text != "", tt.wantPromptText)
			}
			if (got.Trajectory.AssistantMessages[0].Text != "") != tt.wantAssistantText {
				t.Fatalf("assistant text presence = %t, want %t", got.Trajectory.AssistantMessages[0].Text != "", tt.wantAssistantText)
			}
			if (got.Trajectory.ToolCalls[0].Text != "") != tt.wantToolCallText {
				t.Fatalf("tool call text presence = %t, want %t", got.Trajectory.ToolCalls[0].Text != "", tt.wantToolCallText)
			}
			if (got.Trajectory.ToolOutputs[0].Text != "") != tt.wantToolOutputText {
				t.Fatalf("tool output text presence = %t, want %t", got.Trajectory.ToolOutputs[0].Text != "", tt.wantToolOutputText)
			}
			if (got.Code.Patch != "" || got.Code.TestPatch != "" || got.Code.Files[0].Path != "") != tt.wantCodeText {
				t.Fatalf("code cleartext presence mismatch for %s: %+v", tt.tier, got.Code)
			}
			if (got.Repo.RemoteURL != "" || got.Repo.BaseCommit != "" || got.Repo.Branch != "" || got.Repo.LicenseSPDX != "") != tt.wantRepoLinkageText {
				t.Fatalf("repo linkage presence mismatch for %s: %+v", tt.tier, got.Repo)
			}
			if (got.Session.ID != "") != tt.wantSessionID {
				t.Fatalf("session id presence = %t, want %t", got.Session.ID != "", tt.wantSessionID)
			}
		})
	}
}

func TestDefaultTierStoresNoCleartext(t *testing.T) {
	got := Project(sampleTask(), ConsentTierHashesOnly)
	if got.Prompts[0].Text != "" ||
		got.Code.Patch != "" ||
		got.Code.TestPatch != "" ||
		got.Code.Files[0].Path != "" ||
		got.Repo.RemoteURL != "" ||
		got.Repo.BaseCommit != "" ||
		got.Session.ID != "" {
		t.Fatalf("default projection leaked cleartext: %+v", got)
	}
	if got.Prompts[0].TextHash == "" || got.Code.Files[0].PathHash == "" || got.Session.IDHash == "" {
		t.Fatalf("default projection dropped hashes: %+v", got)
	}
}

func TestProjectionRequiresExplicitStringCoverage(t *testing.T) {
	explicit := map[string]string{
		"Task.TaskID":                                  "metadata",
		"Task.Harness":                                 "metadata",
		"Task.HarnessCLIVersion":                       "metadata",
		"Task.AdapterVersion":                          "metadata",
		"Task.ConsentTier":                             "metadata",
		"Task.Session.IDHash":                          "hash",
		"Task.Session.ID":                              "full-transcript",
		"Task.Model.ID":                                "metadata",
		"Task.Repo.RemoteHash":                         "hash",
		"Task.Repo.RemoteURL":                          "repo-linkage",
		"Task.Repo.BaseCommit":                         "repo-linkage",
		"Task.Repo.BaseCommitCommittedAt":              "repo-linkage",
		"Task.Repo.BaseCommitSource":                   "repo-linkage",
		"Task.Repo.Branch":                             "repo-linkage",
		"Task.Repo.LicenseSPDX":                        "repo-linkage",
		"Task.Repo.LockfileHashes{key}":                "metadata",
		"Task.Repo.LockfileHashes{value}":              "hash",
		"Task.Environment.OS":                          "metadata",
		"Task.Environment.Toolchain{key}":              "metadata",
		"Task.Environment.Toolchain{value}":            "metadata",
		"Task.Environment.Tools[]":                     "metadata",
		"Task.Environment.ApprovalPolicy":              "metadata",
		"Task.Environment.SandboxPolicy":               "metadata",
		"Task.Prompts[].Role":                          "metadata",
		"Task.Prompts[].TextHash":                      "hash",
		"Task.Prompts[].Text":                          "prompts",
		"Task.Trajectory.AssistantMessages[].Name":     "metadata",
		"Task.Trajectory.AssistantMessages[].TextHash": "hash",
		"Task.Trajectory.AssistantMessages[].Text":     "assistant-msgs",
		"Task.Trajectory.ToolCalls[].Name":             "metadata",
		"Task.Trajectory.ToolCalls[].TextHash":         "hash",
		"Task.Trajectory.ToolCalls[].Text":             "tool-calls",
		"Task.Trajectory.ToolOutputs[].Name":           "metadata",
		"Task.Trajectory.ToolOutputs[].TextHash":       "hash",
		"Task.Trajectory.ToolOutputs[].Text":           "tool-outputs",
		"Task.Code.Patch":                              "code+diffs",
		"Task.Code.TestPatch":                          "code+diffs",
		"Task.Code.Files[].PathHash":                   "hash",
		"Task.Code.Files[].Path":                       "code+diffs",
		"Task.Code.Files[].Role":                       "metadata",
		"Task.RedactionReport.ScrubberVersion":         "metadata",
		"Task.RedactionReport.ByCategory{key}":         "metadata",
		"Task.RedactionReport.BestEffortNotice":        "metadata",
	}
	got := stringFieldPaths(reflect.TypeOf(Task{}), "Task")
	want := make([]string, 0, len(explicit))
	for path := range explicit {
		want = append(want, path)
	}
	sort.Strings(want)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("string field projection coverage mismatch (-want +got):\n%s", diff)
	}
	for path, classification := range explicit {
		if classification == "" {
			t.Fatalf("field %s has empty projection classification", path)
		}
	}
}

func TestFileRoleClassification(t *testing.T) {
	tests := []struct {
		path string
		want FileRole
	}{
		{"foo_test.go", FileRoleTest},
		{"test_bar.py", FileRoleTest},
		{"spec/baz_spec.rb", FileRoleTest},
		{"src/main.rs", FileRoleSolution},
		{"go.sum", FileRoleLockfile},
		{"package-lock.json", FileRoleLockfile},
		{".github/workflows/ci.yml", FileRoleConfig},
		{"go.mod", FileRoleConfig},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ClassifyFileRole(tt.path); got != tt.want {
				t.Fatalf("ClassifyFileRole() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileRoleClassificationPathTraversalAndOddNames(t *testing.T) {
	tests := []struct {
		path string
		want FileRole
	}{
		{"../../etc/passwd", FileRoleUnknown},
		{"a/b/../c_test.go", FileRoleTest},
		{"dir with spaces/naive_test.go", FileRoleTest},
		{"unicode/naive_test.go", FileRoleTest},
		{"nested/go.sum", FileRoleLockfile},
		{"test/readme.md", FileRoleTest},
		{"src/specimen.go", FileRoleSolution},
		{"/absolute/file_test.go", FileRoleUnknown},
		{"..\\windows\\escape_test.go", FileRoleUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ClassifyFileRole(tt.path); got != tt.want {
				t.Fatalf("ClassifyFileRole() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTaskIDIsDeterministicAndAddressable(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	a := DeterministicTaskID(salt, "https://github.com/Atharva-Kanherkar/proofswe", "sess")
	if a == "" || !strings.HasPrefix(a, "sha256:") {
		t.Fatalf("task id = %q, want sha256 prefix", a)
	}
	if got := DeterministicTaskID(salt, "https://github.com/Atharva-Kanherkar/proofswe", "sess"); got != a {
		t.Fatalf("stable task id mismatch: %q vs %q", got, a)
	}
	if got := DeterministicTaskID(salt, "https://github.com/other/repo", "sess"); got == a {
		t.Fatalf("task id did not change with remote")
	}
	if got := DeterministicTaskID(salt, "https://github.com/Atharva-Kanherkar/proofswe", "other"); got == a {
		t.Fatalf("task id did not change with session")
	}
	if got := DeterministicTaskID([]byte("different"), "https://github.com/Atharva-Kanherkar/proofswe", "sess"); got == a {
		t.Fatalf("task id did not change with salt")
	}
}

func TestTaskRoundTrip(t *testing.T) {
	task := sampleTask()
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if diff := cmp.Diff(task, got); diff != "" {
		t.Fatalf("round trip mismatch (-want +got):\n%s", diff)
	}
}

func TestTolerantParseUnknownFutureTaskFieldsRoundTrip(t *testing.T) {
	raw := []byte(`{"task_schema_version":999,"task_id":"sha256:abc","future":{"nested":true},"session":{"id_hash":"sha256:s","future":1},"redaction_report":{"scrubber_version":"redact/9","best_effort_notice":"notice","future":true}}`)
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		t.Fatalf("Unmarshal future task: %v", err)
	}
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal future task: %v", err)
	}
	var got Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal remarshal: %v", err)
	}
	if got.TaskSchemaVersion != 999 || got.TaskID != "sha256:abc" || got.Session.IDHash != "sha256:s" {
		t.Fatalf("modeled fields not preserved after tolerant parse: %+v", got)
	}
}

func TestTierNestingSuperset(t *testing.T) {
	tiers := []ConsentTier{ConsentTierHashesOnly, ConsentTierPrompts, ConsentTierActions, ConsentTierCode, ConsentTierFull}
	for i := 0; i < len(tiers)-1; i++ {
		a := CategorySet(CategoriesForTier(tiers[i]))
		b := CategorySet(CategoriesForTier(tiers[i+1]))
		if len(b) <= len(a) {
			t.Fatalf("%s categories not stricter subset of %s", tiers[i], tiers[i+1])
		}
		for category := range a {
			if !b[category] {
				t.Fatalf("%s missing category %s from %s", tiers[i+1], category, tiers[i])
			}
		}
	}
}

func TestProjectMonotonicity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		task := sampleTask()
		lo := rapid.SampledFrom([]ConsentTier{ConsentTierHashesOnly, ConsentTierPrompts, ConsentTierActions, ConsentTierCode, ConsentTierFull}).Draw(t, "lo")
		hi := rapid.SampledFrom([]ConsentTier{ConsentTierHashesOnly, ConsentTierPrompts, ConsentTierActions, ConsentTierCode, ConsentTierFull}).Draw(t, "hi")
		if ConsentTierRank(lo) > ConsentTierRank(hi) {
			lo, hi = hi, lo
		}
		got := Project(Project(task, hi), lo)
		want := Project(task, lo)
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("projection not monotonic (-want +got):\n%s", diff)
		}
	})
}

func FuzzProjectNeverWidensTier(f *testing.F) {
	f.Add("hashes-only", "prompt", "patch", "remote")
	f.Add("prompts", "hello", "", "")
	f.Fuzz(func(t *testing.T, tierName, promptText, patchText, remote string) {
		task := sampleTask()
		task.Prompts[0].Text = promptText
		task.Code.Patch = patchText
		task.Repo.RemoteURL = remote
		got := Project(task, ConsentTier(tierName))
		switch NormalizeConsentTier(ConsentTier(tierName)) {
		case ConsentTierHashesOnly:
			if got.Prompts[0].Text != "" || got.Code.Patch != "" || got.Repo.RemoteURL != "" {
				t.Fatalf("hashes-only widened: %+v", got)
			}
		case ConsentTierPrompts:
			if got.Code.Patch != "" || got.Repo.RemoteURL != "" {
				t.Fatalf("prompts widened: %+v", got)
			}
		case ConsentTierActions:
			if got.Code.Patch != "" || got.Repo.RemoteURL != "" {
				t.Fatalf("actions widened: %+v", got)
			}
		case ConsentTierCode, ConsentTierFull:
			// These tiers may keep code/repo fields; the lower-tier cases above
			// pin the no-widening property.
		}
	})
}

func FuzzParseTaskTolerant(f *testing.F) {
	f.Add([]byte(`{"task_schema_version":999,"task_id":"sha256:x","unknown":true}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var task Task
		_ = json.Unmarshal(data, &task)
	})
}

func stringFieldPaths(t reflect.Type, path string) []string {
	var out []string
	collectStringFieldPaths(t, path, &out)
	sort.Strings(out)
	return out
}

func collectStringFieldPaths(t reflect.Type, path string, out *[]string) {
	if t == reflect.TypeOf(time.Time{}) || t == reflect.TypeOf(json.RawMessage{}) {
		return
	}
	switch t.Kind() { //nolint:exhaustive // Non-string/container kinds are intentionally ignored.
	case reflect.String:
		*out = append(*out, path)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" || field.Tag.Get("json") == "-" {
				continue
			}
			collectStringFieldPaths(field.Type, path+"."+field.Name, out)
		}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return
		}
		collectStringFieldPaths(t.Elem(), path+"[]", out)
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			*out = append(*out, path+"{key}")
		}
		if t.Elem().Kind() == reflect.String {
			*out = append(*out, path+"{value}")
		} else {
			collectStringFieldPaths(t.Elem(), path+"{value}", out)
		}
	case reflect.Pointer:
		collectStringFieldPaths(t.Elem(), path, out)
	default:
		return
	}
}

func sampleTask() Task {
	return Task{
		TaskSchemaVersion: TaskSchemaVersion,
		TaskID:            "sha256:task",
		Harness:           "codex",
		AdapterVersion:    "codex/1",
		CapturedAt:        time.Unix(1_700_000_000, 0).UTC(),
		ConsentTier:       ConsentTierFull,
		Session:           TaskSession{IDHash: "sha256:session", ID: "sess"},
		Model:             TaskModel{ID: "gpt-5"},
		Repo: TaskRepo{
			RemoteHash:            "sha256:remote",
			RemoteURL:             "https://github.com/Atharva-Kanherkar/proofswe",
			BaseCommit:            "abc123",
			BaseCommitCommittedAt: "2026-06-14T00:00:00Z",
			BaseCommitSource:      BaseCommitSourceHead,
			Branch:                "main",
			Dirty:                 true,
			IsPublic:              true,
			LicenseSPDX:           "MIT",
			LockfileHashes:        map[string]string{"go.sum": "sha256:lock"},
		},
		Environment: TaskEnvironment{
			OS:             "darwin",
			Toolchain:      map[string]string{"go": "go1.25.5"},
			Tools:          []string{"Bash", "Read"},
			ApprovalPolicy: "never",
			SandboxPolicy:  "danger-full-access",
		},
		SpecSignals: TaskSpecSignals{StartingPromptWords: 2, PromptCount: 1},
		Prompts: []TaskPrompt{{
			TurnIndex: 0,
			Role:      "user",
			TextHash:  "sha256:prompt",
			Text:      "hello world",
		}},
		Trajectory: TaskTrajectory{
			AssistantMessages: []TaskText{{TurnIndex: 1, TextHash: "sha256:assistant", Text: "done"}},
			ToolCalls:         []TaskText{{TurnIndex: 1, Name: "Bash", TextHash: "sha256:call", Text: `{"command":"go test ./..."}`}},
			ToolOutputs:       []TaskText{{TurnIndex: 1, Name: "Bash", TextHash: "sha256:output", Text: "ok"}},
		},
		Code: TaskCode{
			Patch:     "diff --git a/a b/a",
			TestPatch: "diff --git a/a_test.go b/a_test.go",
			Files:     []TaskFile{{PathHash: "sha256:path", Path: "a.go", Role: FileRoleSolution}},
		},
		RedactionReport: RedactionReport{
			ScrubberVersion:  "redact/1",
			SpansRedacted:    1,
			ByCategory:       map[string]int{"secret.openai": 1},
			BestEffortNotice: "automated redaction is best-effort and can miss secrets",
		},
	}
}

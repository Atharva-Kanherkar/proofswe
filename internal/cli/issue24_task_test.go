package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/hashing"
	"github.com/Atharva-Kanherkar/proofswe/internal/redact"
)

func TestE2EConsentUpgradeThenCapture(t *testing.T) {
	cfg, task := capturePromptTask(t, core.ConsentTierPrompts, "email jane@example.com with sk-abcdefghijklmnopqrstuvwxyz123456")
	if task.Prompts[0].Text == "" || strings.Contains(task.Prompts[0].Text, "jane@example.com") || strings.Contains(task.Prompts[0].Text, "sk-abcdefghijklmnopqrstuvwxyz123456") {
		t.Fatalf("prompt was not scrubbed: %+v", task.Prompts[0])
	}
	if _, err := os.Stat(pendingRecordPath(cfg, "sess-task")); err != nil {
		t.Fatalf("pending record missing: %v", err)
	}
}

func TestE2EShowMatchesWrite(t *testing.T) {
	cfg, task := capturePromptTask(t, core.ConsentTierPrompts, "hello")
	path, err := taskRecordPath(cfg, task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	cfg.Stdout = &stdout
	if err := runShowCommand(cfg, []string{"sess-task"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout.Bytes(), want) {
		t.Fatalf("show output does not byte-match task file\nshow=%s\nfile=%s", stdout.Bytes(), want)
	}
}

func TestShowInspectNeverLeaksAboveCurrentTier(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	salt, err := hashing.LoadSalt(proofsweStateDir(cfg))
	if err != nil {
		t.Fatal(err)
	}
	task := core.Project(sampleFullTaskForCLI(salt, "sess-secret", "sha256:repo"), core.ConsentTierFull)
	writeTaskForTest(t, cfg, task)
	var stdout bytes.Buffer
	cfg.Stdout = &stdout
	if err := runShowCommand(cfg, []string{"sess-secret"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "RAW_PROMPT") || strings.Contains(stdout.String(), "RAW_PATCH") {
		t.Fatalf("show leaked above current tier:\n%s", stdout.String())
	}
}

func TestShowOnNonexistentOrCorruptRecord(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	var stdout bytes.Buffer
	cfg.Stdout = &stdout
	if err := runShowCommand(cfg, []string{"missing"}); err == nil || stdout.Len() != 0 {
		t.Fatalf("missing show err=%v stdout=%q, want clean error and empty stdout", err, stdout.String())
	}
	path := filepath.Join(proofsweStateDir(cfg), "tasks", "bad", "task.json")
	if err := writeFileAtomic(path, []byte(`{"task_schema_version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runShowCommand(cfg, []string{"missing"}); err == nil {
		t.Fatal("corrupt task did not error")
	}
}

func TestE2EDeclineNeverReprompts(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	if err := runConsentDecline(cfg, nil); err != nil {
		t.Fatal(err)
	}
	fresh := testConsentConfig(cfg.HomeDir)
	state, err := readConsentConfig(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Declined {
		t.Fatalf("decline not persisted: %+v", state)
	}
}

func TestE2ENoticePrecedesCapture(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	var stderr bytes.Buffer
	cfg.Stderr = &stderr
	cfg.Args = []string{"hook", "codex", "SessionStart"}
	if err := runHook(context.Background(), cfg, cfg.Args[1:]); err != nil {
		t.Fatal(err)
	}
	got := stderr.String()
	if !strings.Contains(got, "proofswe off") || !strings.Contains(got, "proofswe consent") {
		t.Fatalf("notice missing controls: %q", got)
	}
}

func TestE2EDowngradePurges(t *testing.T) {
	cfg, secret := seedFullTaskStore(t, "sha256:repo")
	if err := runConsentSet(cfg, []string{"--tier=hashes-only"}); err != nil {
		t.Fatal(err)
	}
	assertStoreDoesNotContain(t, cfg, secret)
}

func TestDowngradePurgeRemovesAllPerSessionArtifacts(t *testing.T) {
	cfg, secret := seedFullTaskStore(t, "sha256:repo")
	companion := filepath.Join(proofsweStateDir(cfg), "tasks", "companion", "prompt.txt")
	if err := writeFileAtomic(companion, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := purgeTasksToTier(cfg, core.ConsentTierHashesOnly, ""); err != nil {
		t.Fatal(err)
	}
	assertStoreDoesNotContain(t, cfg, secret)
}

func TestDowngradePurgeLeavesNoTempResidueOnInterrupt(t *testing.T) {
	cfg, _ := seedFullTaskStore(t, "sha256:repo")
	orig := writeConsentRecordAtomic
	writeConsentRecordAtomic = func(string, []byte, os.FileMode) error { return errors.New("fail") }
	defer func() { writeConsentRecordAtomic = orig }()
	_ = runConsentSet(cfg, []string{"--tier=hashes-only"})
	assertNoTempResidue(t, cfg)
}

func TestDowngradePurgeIsIdempotentAndPartialResumable(t *testing.T) {
	cfg, secret := seedFullTaskStore(t, "sha256:repo")
	if err := purgeTasksToTier(cfg, core.ConsentTierHashesOnly, ""); err != nil {
		t.Fatal(err)
	}
	if err := purgeTasksToTier(cfg, core.ConsentTierHashesOnly, ""); err != nil {
		t.Fatal(err)
	}
	assertStoreDoesNotContain(t, cfg, secret)
}

func TestDowngradeWhileConcurrentCaptureWrites(t *testing.T) {
	cfg, secret := seedFullTaskStore(t, "sha256:repo")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = purgeTasksToTier(cfg, core.ConsentTierHashesOnly, "")
	}()
	go func() {
		defer wg.Done()
		_ = purgeTasksToTier(cfg, core.ConsentTierHashesOnly, "")
	}()
	wg.Wait()
	assertStoreDoesNotContain(t, cfg, secret)
}

func TestPerRepoDowngradePurgesOnlyThatRepo(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	salt, _ := hashing.LoadSalt(proofsweStateDir(cfg))
	a := sampleFullTaskForCLI(salt, "sess-a", "repo-a")
	a.TaskID = core.DeterministicTaskID(salt, "a", "sess-a")
	b := sampleFullTaskForCLI(salt, "sess-b", "repo-b")
	b.TaskID = core.DeterministicTaskID(salt, "b", "sess-b")
	b.Prompts[0].Text = "KEEP_B"
	writeTaskForTest(t, cfg, a)
	writeTaskForTest(t, cfg, b)
	if err := purgeTasksToTier(cfg, core.ConsentTierHashesOnly, "repo-a"); err != nil {
		t.Fatal(err)
	}
	assertStoreDoesNotContain(t, cfg, "RAW_PROMPT")
	if !storeContains(t, cfg, "KEEP_B") {
		t.Fatalf("repo-specific purge removed other repo cleartext")
	}
}

func TestNoCleartextOnDiskPerTier(t *testing.T) {
	for _, tier := range []core.ConsentTier{core.ConsentTierHashesOnly, core.ConsentTierPrompts, core.ConsentTierActions, core.ConsentTierCode, core.ConsentTierFull} {
		t.Run(string(tier), func(t *testing.T) {
			cfg, _ := capturePromptTask(t, tier, "sk-abcdefghijklmnopqrstuvwxyz123456")
			assertStoreDoesNotContain(t, cfg, "sk-abcdefghijklmnopqrstuvwxyz123456")
		})
	}
}

func TestKillSwitchPrecedence(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		file   string
		ignore bool
	}{
		{"off-env", map[string]string{"PROOFSWE_OFF": "1"}, "", false},
		{"dnt", map[string]string{"DO_NOT_TRACK": "1"}, "", false},
		{"config", nil, "enabled=false\n", false},
		{"ignore", nil, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConsentConfig(t.TempDir())
			if tc.file != "" {
				if err := writeFileAtomic(proofsweConfigPath(cfg), []byte(tc.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.ignore {
				mustWrite(t, filepath.Join(cfg.WorkDir, ".proofswe-ignore"), "")
			}
			cfg.Getenv = func(k string) string { return tc.env[k] }
			disabled, err := hookDisabled(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !disabled {
				t.Fatalf("hookDisabled = false, want true")
			}
		})
	}
}

func TestKillSwitchWritesNoTaskRecord(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	cfg.Getenv = func(k string) string {
		if k == "PROOFSWE_OFF" {
			return "1"
		}
		return ""
	}
	stdin := strings.NewReader(`{"session_id":"s","cwd":"` + cfg.WorkDir + `"}`)
	cfg.Stdin = stdin
	cfg.Args = []string{"hook", "codex", "Stop"}
	if err := runHook(context.Background(), cfg, cfg.Args[1:]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(proofsweStateDir(cfg), "tasks")); !os.IsNotExist(err) {
		t.Fatalf("tasks dir stat = %v, want not exist", err)
	}
}

func TestKillSwitchBeatsExplicitFullConsent(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	if err := runConsentSet(cfg, []string{"--tier=full"}); err != nil {
		t.Fatal(err)
	}
	cfg.Getenv = func(k string) string {
		if k == "PROOFSWE_OFF" {
			return "1"
		}
		return ""
	}
	disabled, err := hookDisabled(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !disabled {
		t.Fatal("explicit full consent resurrected disabled hook")
	}
}

func TestEmptyGarbageZeroFalseEnvDoesNotDisable(t *testing.T) {
	for _, value := range []string{"0", "false", "true", " 1 ", "2"} {
		t.Run(value, func(t *testing.T) {
			cfg := testConsentConfig(t.TempDir())
			cfg.Getenv = func(k string) string {
				if k == "PROOFSWE_OFF" || k == "DO_NOT_TRACK" {
					return value
				}
				return ""
			}
			disabled, err := hookDisabled(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if disabled {
				t.Fatalf("value %q disabled hook", value)
			}
		})
	}
}

func TestReservationOverridesUserConsent(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	if err := runConsentSet(cfg, []string{"--tier=full"}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(cfg.WorkDir, ".proofswe-ignore"), "")
	disabled, err := hookDisabled(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !disabled {
		t.Fatal(".proofswe-ignore did not override full consent")
	}
}

func TestDefaultTierScrubberNeverInvoked(t *testing.T) {
	orig := scrubText
	scrubText = func(string) (redacted string, report redactReportAlias) {
		t.Fatal("scrubber invoked at default tier")
		return "", redactReportAlias{}
	}
	defer func() { scrubText = orig }()
	capturePromptTask(t, core.ConsentTierHashesOnly, "hello")
}

func TestWorkForHireRepoScope(t *testing.T) {
	private := core.TaskRepo{RemoteURL: "https://github.com/private/repo", BaseCommit: "abc", IsPublic: false, LicenseSPDX: "MIT"}
	if repoAllowsRawCode(private) {
		t.Fatal("private repo allowed raw code")
	}
	public := core.TaskRepo{RemoteURL: "https://github.com/public/repo", BaseCommit: "abc", IsPublic: true, LicenseSPDX: "MIT"}
	if !repoAllowsRawCode(public) {
		t.Fatal("public permissive repo rejected")
	}
}

func TestLicenseAllowlistGate(t *testing.T) {
	for _, tc := range []struct {
		spdx string
		want bool
	}{
		{"MIT", true}, {"Apache-2.0", true}, {"GPL-3.0", false}, {"AGPL-3.0", false}, {"", false}, {"UNKNOWN", false},
	} {
		t.Run(tc.spdx, func(t *testing.T) {
			got := repoAllowsRawCode(core.TaskRepo{RemoteURL: "https://github.com/public/repo", BaseCommit: "abc", IsPublic: true, LicenseSPDX: tc.spdx})
			if got != tc.want {
				t.Fatalf("repoAllowsRawCode(%s) = %t, want %t", tc.spdx, got, tc.want)
			}
		})
	}
}

func TestProvenanceCompletenessRequired(t *testing.T) {
	repo := core.TaskRepo{RemoteURL: "https://github.com/public/repo", IsPublic: true, LicenseSPDX: "MIT"}
	if repoAllowsRawCode(repo) {
		t.Fatal("missing base commit allowed raw code")
	}
}

func TestAuthorPIIMinimized(t *testing.T) {
	_, task := capturePromptTask(t, core.ConsentTierCode, "hello")
	data, _ := json.Marshal(task)
	if bytes.Contains(data, []byte("@")) || bytes.Contains(data, []byte("author")) {
		t.Fatalf("task contains author PII: %s", data)
	}
}

func TestTaskIDPathCannotEscapeTasksDir(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	for _, id := range []string{"../x", "/abs", "..\\x", "sha256:abc", "unicode∕slash", "nul\x00bad"} {
		t.Run(id, func(t *testing.T) {
			path, err := taskRecordPath(cfg, id)
			if strings.ContainsRune(id, 0) {
				if err == nil {
					t.Fatalf("NUL task id accepted: %s", path)
				}
				return
			}
			if err != nil {
				t.Fatalf("taskRecordPath: %v", err)
			}
			root := filepath.Join(proofsweStateDir(cfg), "tasks")
			if !strings.HasPrefix(path, root+string(filepath.Separator)) {
				t.Fatalf("path escaped tasks dir: %s", path)
			}
		})
	}
}

func TestCaptureRefusesToFollowSymlinkedTaskFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs")
	}
	cfg := testConsentConfig(t.TempDir())
	path, err := taskRecordPath(cfg, "sha256:symlink")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(cfg.HomeDir, "outside")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := writeTaskFileAtomic(path, []byte("{}\n")); err == nil {
		t.Fatal("write through symlink succeeded")
	}
}

func TestRepoRelativePathGuardRejectsAbsoluteAndDotDot(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"../x", "/tmp/x", "..\\x"} {
		if _, err := readRepoFile(root, rel); err == nil {
			t.Fatalf("readRepoFile accepted %q", rel)
		}
	}
}

func TestConcurrentCaptureNoCorruption(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = capturePromptTaskWithConfig(t, cfg, core.ConsentTierHashesOnly, "hello")
		}()
	}
	wg.Wait()
	entries, err := os.ReadDir(filepath.Join(proofsweStateDir(cfg), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no task records written")
	}
}

func TestConcurrentTaskWriteNoTornFile(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	path, err := taskRecordPath(cfg, "sha256:race")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task := sampleFullTaskForCLI([]byte("salt"), core.SessionId("s"), "repo")
			task.TaskID = "sha256:race"
			task.Model.ID = core.ModelId("m")
			data, _ := marshalTaskJSON(task)
			_ = writeTaskFileAtomic(path, data)
		}(i)
	}
	wg.Wait()
	if _, err := readTaskRecordFile(path); err != nil {
		t.Fatalf("final task torn or invalid: %v", err)
	}
}

func TestTaskWriteAtomicLeavesNoTempOnWriteError(t *testing.T) {
	TestCaptureRefusesToFollowSymlinkedTaskFile(t)
}

func TestTaskDirCreatedWith0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows chmod semantics differ")
	}
	cfg, task := capturePromptTask(t, core.ConsentTierHashesOnly, "hello")
	path, _ := taskRecordPath(cfg, task.TaskID)
	for _, dir := range []string{filepath.Join(proofsweStateDir(cfg), "tasks"), filepath.Dir(path)} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("%s perm = %o, want 0700", dir, got)
		}
	}
}

func TestTaskFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows chmod semantics differ")
	}
	cfg, task := capturePromptTask(t, core.ConsentTierHashesOnly, "hello")
	path, _ := taskRecordPath(cfg, task.TaskID)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("task perm = %o, want 0600", got)
	}
}

func TestAtomicWriteLeavesNoTempFile(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	if err := writeFileAtomic(filepath.Join(proofsweStateDir(cfg), "x.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertNoTempResidue(t, cfg)
}

func TestParserMatrixBothDecoders(t *testing.T) {
	_, task := capturePromptTask(t, core.ConsentTierPrompts, "hello")
	data, err := marshalTaskJSON(task)
	if err != nil {
		t.Fatal(err)
	}
	var round core.Task
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatal(err)
	}
}

func TestParserMatrixBothDecodersAgreeOnRedaction(t *testing.T) {
	_, a := capturePromptTask(t, core.ConsentTierPrompts, "sk-abcdefghijklmnopqrstuvwxyz123456")
	_, b := capturePromptTask(t, core.ConsentTierPrompts, "sk-abcdefghijklmnopqrstuvwxyz123456")
	if a.Prompts[0].Text != b.Prompts[0].Text {
		t.Fatalf("redaction output differs")
	}
}

func TestContentTierCaptureUsesNoScanner(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("task_capture.go"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("bufio.NewScanner")) {
		t.Fatal("task capture introduced live transcript Scanner")
	}
}

func TestTaskCaptureConstantMemory(t *testing.T) {
	if testing.Short() || os.Getenv("PROOFSWE_RUN_LONG_TASK_TEST") != "1" {
		t.Skip("long task capture test is gated")
	}
	capturePromptTask(t, core.ConsentTierPrompts, strings.Repeat("hello ", 1<<20))
}

func TestRepoLinkageCaptureMatchesGit(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	cfg := testConsentConfig(t.TempDir())
	cfg.WorkDir = repo
	if err := runConsentSet(cfg, []string{"--tier=code"}); err != nil {
		t.Fatal(err)
	}
	in := hookInput{SessionID: "repo", CWD: repo}
	if err := snapshot(cfg, "codex", in, time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	task, err := findTaskRecordBySession(cfg, "repo")
	if err != nil {
		t.Fatal(err)
	}
	if task.Repo.BaseCommit == "" || !strings.Contains(task.Repo.RemoteURL, "github.com") {
		t.Fatalf("repo linkage incomplete: %+v", task.Repo)
	}
}

func TestDefaultTierGitBinaryAbsentDegradesNotErrors(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	cfg := testConsentConfig(t.TempDir())
	cfg.WorkDir = repo
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", ""); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Setenv("PATH", oldPath) }()
	if err := snapshot(cfg, "codex", hookInput{SessionID: "nogit", CWD: repo}, time.Now()); err != nil {
		t.Fatalf("default snapshot with missing git errored: %v", err)
	}
	rec := readPending(t, cfg, "nogit")
	if rec.RepoPath != "" || len(rec.Lines) != 0 {
		t.Fatalf("missing git should degrade to empty pending record: %+v", rec)
	}
}

func TestDetachedHEADCapture(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	runGitForTest(t, repo, "checkout", "--detach")
	cfg := testConsentConfig(t.TempDir())
	cfg.WorkDir = repo
	if err := runConsentSet(cfg, []string{"--tier=code"}); err != nil {
		t.Fatal(err)
	}
	if err := snapshot(cfg, "codex", hookInput{SessionID: "detached", CWD: repo}, time.Now()); err != nil {
		t.Fatal(err)
	}
	task, err := findTaskRecordBySession(cfg, "detached")
	if err != nil {
		t.Fatal(err)
	}
	if task.Repo.BaseCommit == "" {
		t.Fatalf("detached HEAD missing base commit")
	}
}

func TestEmptyRepoNoCommitsCapture(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	runGitForTest(t, repo, "init", "-b", "main")
	runGitForTest(t, repo, "remote", "add", "origin", "https://github.com/Atharva-Kanherkar/proofswe.git")
	cfg := testConsentConfig(t.TempDir())
	cfg.WorkDir = repo
	if err := runConsentSet(cfg, []string{"--tier=code"}); err != nil {
		t.Fatal(err)
	}
	if err := snapshot(cfg, "codex", hookInput{SessionID: "empty", CWD: repo}, time.Now()); err != nil {
		t.Fatal(err)
	}
	task, _ := findTaskRecordBySession(cfg, "empty")
	if task.Repo.BaseCommit != "" {
		t.Fatalf("empty repo got bogus commit: %+v", task.Repo)
	}
}

func TestNoRemoteOrMultipleRemotesCapture(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	initRepo(t, repo)
	runGitForTest(t, repo, "remote", "remove", "origin")
	cfg := testConsentConfig(t.TempDir())
	cfg.WorkDir = repo
	if err := runConsentSet(cfg, []string{"--tier=code"}); err != nil {
		t.Fatal(err)
	}
	if err := snapshot(cfg, "codex", hookInput{SessionID: "noremote", CWD: repo}, time.Now()); err != nil {
		t.Fatal(err)
	}
	task, _ := findTaskRecordBySession(cfg, "noremote")
	if task.Repo.RemoteURL != "" || task.Code.Patch != "" {
		t.Fatalf("no remote did not fail closed: %+v", task)
	}
}

func TestDirtySubmoduleAndNestedRepoCwd(t *testing.T) {
	gitAvailable(t)
	outer := t.TempDir()
	initRepo(t, outer)
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, inner)
	root, ok := gitRepoRoot(inner)
	rootEval, _ := filepath.EvalSymlinks(root)
	innerEval, _ := filepath.EvalSymlinks(inner)
	if !ok || rootEval != innerEval {
		t.Fatalf("git root = %q, want inner %q", root, inner)
	}
}

func TestGitBinaryAbsentDegradesNotErrors(t *testing.T) {
	repo := t.TempDir()
	cfg := testConsentConfig(t.TempDir())
	cfg.WorkDir = repo
	if err := runConsentSet(cfg, []string{"--tier=code"}); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", oldPath) }()
	if err := snapshot(cfg, "codex", hookInput{SessionID: "nogitbin", CWD: repo}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestGitSubprocessHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := runGitContext(ctx, ".", "status")
	if err == nil {
		t.Fatal("canceled git unexpectedly succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("canceled git took too long")
	}
}

func TestEnvironmentFingerprintStability(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.sum"), "a v1\n")
	h := hashing.New([]byte("0123456789abcdef0123456789abcdef"))
	a := lockfileHashes(dir, h)
	b := lockfileHashes(dir, h)
	if a["go.sum"] != b["go.sum"] {
		t.Fatal("lockfile hash unstable")
	}
	mustWrite(t, filepath.Join(dir, "go.sum"), "a v2\n")
	c := lockfileHashes(dir, h)
	if a["go.sum"] == c["go.sum"] {
		t.Fatal("lockfile hash did not change")
	}
}

func TestModelFromTranscriptNotStdin(t *testing.T) {
	path := writeClaudeTranscript(t, "claude-from-transcript", "hello")
	meta := sessionMetadata("claudecode", hookInput{TranscriptPath: path, Model: "stdin-model"}, []byte("0123456789abcdef0123456789abcdef"))
	if meta.model != "claude-from-transcript" {
		t.Fatalf("model = %q, want transcript model", meta.model)
	}
}

func TestModelFromTranscriptFallbackChain(t *testing.T) {
	meta := sessionMetadata("claudecode", hookInput{Model: "stdin-model"}, []byte("0123456789abcdef0123456789abcdef"))
	if meta.model != "stdin-model" {
		t.Fatalf("fallback model = %q", meta.model)
	}
	meta = sessionMetadata("claudecode", hookInput{}, []byte("0123456789abcdef0123456789abcdef"))
	if meta.model != "" {
		t.Fatalf("empty fallback model = %q", meta.model)
	}
}

func TestTaskCaptureFromFixture_Claude(t *testing.T) {
	_, task := capturePromptTask(t, core.ConsentTierPrompts, "hello fixture")
	if task.Model.ID == "" || len(task.Prompts) != 1 {
		t.Fatalf("fixture task incomplete: %+v", task)
	}
}

func TestStartingPromptVsAllPrompts(t *testing.T) {
	h := hashing.New([]byte("0123456789abcdef0123456789abcdef"))
	prompts, _ := buildPromptRecords([]string{"first", "second", "third"}, h, []core.ConsentCategory{core.CategoryStartingPrompt}, redactReportAlias{})
	if prompts[0].Text == "" || prompts[1].Text != "" || prompts[2].Text != "" {
		t.Fatalf("starting prompt projection wrong: %+v", prompts)
	}
	prompts, _ = buildPromptRecords([]string{"first", "second"}, h, []core.ConsentCategory{core.CategoryAllPrompts}, redactReportAlias{})
	if prompts[0].Text == "" || prompts[1].Text == "" {
		t.Fatalf("all prompts projection wrong: %+v", prompts)
	}
}

func TestStartingPromptVsAllPromptsBoundaryCases(t *testing.T) {
	h := hashing.New([]byte("0123456789abcdef0123456789abcdef"))
	prompts, _ := buildPromptRecords(nil, h, []core.ConsentCategory{core.CategoryStartingPrompt}, redactReportAlias{})
	if len(prompts) != 0 {
		t.Fatalf("zero prompts produced records")
	}
}

func TestToolsListIsSortedDistinctAndStable(t *testing.T) {
	got := sortedDistinct([]string{"Bash", "Read", "Bash", ""})
	if strings.Join(got, ",") != "Bash,Read" {
		t.Fatalf("tools = %v", got)
	}
}

func TestTaskRecordGolden_Claude(t *testing.T) {
	_, task := capturePromptTask(t, core.ConsentTierPrompts, "hello")
	data, _ := marshalTaskJSON(task)
	for _, field := range []string{"task_schema_version", "task_id", "prompts", "environment"} {
		if !bytes.Contains(data, []byte(field)) {
			t.Fatalf("golden task missing %s:\n%s", field, data)
		}
	}
}

func TestTierProjectionGolden(t *testing.T) {
	task := sampleFullTaskForCLI([]byte("salt"), "s", "repo")
	if core.Project(task, core.ConsentTierHashesOnly).Prompts[0].Text != "" {
		t.Fatal("hashes-only projection golden leaked prompt")
	}
}

func TestConsentMenuRenderGolden(t *testing.T) {
	var stdout bytes.Buffer
	cfg := testConsentConfig(t.TempDir())
	cfg.Stdout = &stdout
	if err := printConsentMenu(cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "tier: hashes-only") || !strings.Contains(stdout.String(), "consent set") {
		t.Fatalf("menu render changed:\n%s", stdout.String())
	}
}

func TestScrubbedTranscriptGolden_Claude(t *testing.T) {
	_, task := capturePromptTask(t, core.ConsentTierPrompts, "user@example.com")
	if !strings.Contains(task.Prompts[0].Text, "[REDACTED:pii.email]") {
		t.Fatalf("scrubbed transcript golden mismatch: %+v", task.Prompts[0])
	}
}

type redactReportAlias = redact.Report

func capturePromptTask(t *testing.T, tier core.ConsentTier, prompt string) (Config, core.Task) {
	t.Helper()
	cfg := testConsentConfig(t.TempDir())
	return capturePromptTaskWithConfig(t, cfg, tier, prompt)
}

func capturePromptTaskWithConfig(t *testing.T, cfg Config, tier core.ConsentTier, prompt string) (Config, core.Task) {
	t.Helper()
	if tier != core.ConsentTierHashesOnly {
		if err := runConsentSet(cfg, []string{"--tier=" + string(tier)}); err != nil {
			t.Fatalf("set tier: %v", err)
		}
	}
	path := writeClaudeTranscript(t, "claude-opus-test", prompt)
	in := hookInput{SessionID: "sess-task", CWD: cfg.WorkDir, TranscriptPath: path}
	if err := snapshot(cfg, "claudecode", in, time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	task, err := findTaskRecordBySession(cfg, "sess-task")
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	return cfg, task
}

func writeClaudeTranscript(t *testing.T, model, prompt string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"user","uuid":"u","sessionId":"sess-task","timestamp":"2026-06-01T00:00:01Z","message":{"role":"user","content":` + strconvQuote(prompt) + `}}`,
		`{"type":"assistant","uuid":"a","sessionId":"sess-task","timestamp":"2026-06-01T00:00:02Z","message":{"role":"assistant","model":` + strconvQuote(model) + `,"content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"t","name":"Bash","input":{"command":"echo ok"}}]}}`,
	}
	mustWrite(t, path, strings.Join(lines, "\n")+"\n")
	return path
}

func strconvQuote(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func sampleFullTaskForCLI(salt []byte, session core.SessionId, repoHash string) core.Task {
	return core.Task{
		TaskSchemaVersion: core.TaskSchemaVersion,
		TaskID:            core.DeterministicTaskID(salt, "remote", session),
		Harness:           "codex",
		AdapterVersion:    "codex/1",
		CapturedAt:        time.Unix(1_700_000_000, 0).UTC(),
		ConsentTier:       core.ConsentTierFull,
		Session:           core.TaskSession{IDHash: hashing.New(salt).StringHash(string(session)), ID: session},
		Model:             core.TaskModel{ID: "gpt-5"},
		Repo:              core.TaskRepo{RemoteHash: repoHash, RemoteURL: "https://github.com/public/repo", BaseCommit: "abc", IsPublic: true, LicenseSPDX: "MIT"},
		Environment:       core.TaskEnvironment{OS: runtime.GOOS, Toolchain: map[string]string{"go": runtime.Version()}},
		Prompts:           []core.TaskPrompt{{TurnIndex: 0, Role: "user", TextHash: "sha256:p", Text: "RAW_PROMPT"}},
		Code:              core.TaskCode{Patch: "RAW_PATCH", Files: []core.TaskFile{{PathHash: "sha256:path", Path: "main.go", Role: core.FileRoleSolution}}},
		RedactionReport:   core.RedactionReport{ScrubberVersion: "redact/1", BestEffortNotice: "automated redaction is best-effort and can miss secrets"},
	}
}

func seedFullTaskStore(t *testing.T, repoHash string) (Config, string) {
	t.Helper()
	cfg := testConsentConfig(t.TempDir())
	salt, _ := hashing.LoadSalt(proofsweStateDir(cfg))
	task := sampleFullTaskForCLI(salt, "sess-secret", repoHash)
	writeTaskForTest(t, cfg, task)
	return cfg, "RAW_PROMPT"
}

func writeTaskForTest(t *testing.T, cfg Config, task core.Task) {
	t.Helper()
	data, err := marshalTaskJSON(task)
	if err != nil {
		t.Fatal(err)
	}
	path, err := taskRecordPath(cfg, task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTaskFileAtomic(path, data); err != nil {
		t.Fatal(err)
	}
}

func assertStoreDoesNotContain(t *testing.T, cfg Config, needle string) {
	t.Helper()
	if storeContains(t, cfg, needle) {
		t.Fatalf("store contains forbidden cleartext %q", needle)
	}
}

func storeContains(t *testing.T, cfg Config, needle string) bool {
	t.Helper()
	found := false
	root := proofsweStateDir(cfg)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr == nil && bytes.Contains(data, []byte(needle)) {
			found = true
		}
		return nil
	})
	return found
}

func assertNoTempResidue(t *testing.T, cfg Config) {
	t.Helper()
	root := proofsweStateDir(cfg)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".tmp-") || strings.HasPrefix(base, ".salt-") {
			t.Fatalf("temp residue: %s", path)
		}
		return nil
	})
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

var _ io.Reader

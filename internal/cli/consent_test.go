package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"pgregory.net/rapid"
)

func TestTierPresetExpandsToCategories(t *testing.T) {
	tests := []struct {
		tier core.ConsentTier
		want []core.ConsentCategory
	}{
		{core.ConsentTierHashesOnly, nil},
		{core.ConsentTierPrompts, []core.ConsentCategory{core.CategoryStartingPrompt, core.CategoryAllPrompts}},
		{core.ConsentTierActions, []core.ConsentCategory{core.CategoryStartingPrompt, core.CategoryAllPrompts, core.CategoryAssistantMsgs, core.CategoryToolCalls, core.CategoryToolOutputs}},
		{core.ConsentTierCode, []core.ConsentCategory{core.CategoryStartingPrompt, core.CategoryAllPrompts, core.CategoryAssistantMsgs, core.CategoryToolCalls, core.CategoryToolOutputs, core.CategoryCodeDiffs, core.CategoryRepoLinkage}},
		{core.ConsentTierFull, core.ConsentCategories()},
	}
	for _, tt := range tests {
		t.Run(string(tt.tier), func(t *testing.T) {
			got := core.CategoriesForTier(tt.tier)
			if strings.Join(categoryStrings(got), ",") != strings.Join(categoryStrings(tt.want), ",") {
				t.Fatalf("categories = %v, want %v", got, tt.want)
			}
		})
	}

	starting := core.CategorySet([]core.ConsentCategory{core.CategoryStartingPrompt})
	all := core.CategorySet(core.CategoriesForTier(core.ConsentTierPrompts))
	if !all[core.CategoryStartingPrompt] || !all[core.CategoryAllPrompts] || starting[core.CategoryAllPrompts] {
		t.Fatalf("starting-prompt/all-prompts relationship broken")
	}
}

func TestConsentStateTransitions(t *testing.T) {
	home := t.TempDir()
	cfg := testConsentConfig(home)
	if err := runConsentSet(cfg, []string{"--tier=full"}); err != nil {
		t.Fatalf("set full: %v", err)
	}
	state, err := readConsentConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if state.Tier != core.ConsentTierFull {
		t.Fatalf("tier = %s, want full", state.Tier)
	}
	if err := runConsentSet(cfg, []string{"--tier=hashes-only"}); err != nil {
		t.Fatalf("downgrade: %v", err)
	}
	state, _ = readConsentConfig(cfg)
	if state.Tier != core.ConsentTierHashesOnly {
		t.Fatalf("tier = %s, want hashes-only", state.Tier)
	}
	if err := runConsentDecline(cfg, nil); err != nil {
		t.Fatalf("decline: %v", err)
	}
	state, _ = readConsentConfig(cfg)
	if !state.Declined || state.Tier != core.ConsentTierHashesOnly {
		t.Fatalf("decline state = %+v", state)
	}
	if err := runConsentSet(cfg, []string{"--tier=banana"}); !errors.Is(err, ErrUsage) {
		t.Fatalf("invalid transition err = %v, want ErrUsage", err)
	}
}

func TestConsentMalformedConfigFailsClosedToHashesOnly(t *testing.T) {
	cases := map[string]string{
		"unknown":          "tier=banana\n",
		"empty":            "tier=\n",
		"duplicate":        "tier=prompts\ntier=code\n",
		"repo-conflict":    "repo.sha256_x.tier=prompts\nrepo.sha256_x.tier=code\n",
		"category-unknown": "category.future=true\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConsentConfig(t.TempDir())
			if err := writeFileAtomic(proofsweConfigPath(cfg), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			state, err := readConsentConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !state.Malformed || state.Tier != core.ConsentTierHashesOnly || len(state.RepoTiers) != 0 || len(state.Categories) != 0 {
				t.Fatalf("state = %+v, want fail-closed malformed hashes-only", state)
			}
		})
	}
}

func TestConsentPrecedenceFlagsOverEnvOverRepoOverGlobal(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	if err := appendConsentGrant(cfg, core.ConsentTierFull, "", false); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, proofsweConfigPath(cfg), "tier=full\nrepo.repo_hash.tier=hashes-only\n")
	cfg.Getenv = func(k string) string {
		if k == "PROOFSWE_TIER" {
			return "prompts"
		}
		return ""
	}
	flagTier := core.ConsentTierCode
	got, err := effectiveConsentWithFlag(cfg, "repo_hash", &flagTier)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != core.ConsentTierCode {
		t.Fatalf("flag tier = %s, want code", got.Tier)
	}
	got, err = effectiveConsent(cfg, "repo_hash")
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != core.ConsentTierPrompts {
		t.Fatalf("env tier = %s, want prompts", got.Tier)
	}
	cfg.Getenv = func(string) string { return "" }
	got, err = effectiveConsent(cfg, "repo_hash")
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != core.ConsentTierHashesOnly {
		t.Fatalf("repo tier = %s, want hashes-only", got.Tier)
	}
}

func TestConsentEnvTierCannotExceedPersistedConsentRecord(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	cfg.Getenv = func(k string) string {
		if k == "PROOFSWE_TIER" {
			return "full"
		}
		return ""
	}
	got, err := effectiveConsent(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != core.ConsentTierHashesOnly {
		t.Fatalf("tier without grant = %s, want hashes-only", got.Tier)
	}
	if err := appendConsentGrant(cfg, core.ConsentTierPrompts, "", false); err != nil {
		t.Fatal(err)
	}
	got, err = effectiveConsent(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != core.ConsentTierPrompts {
		t.Fatalf("tier clamped to grant = %s, want prompts", got.Tier)
	}
}

func TestConsentRecordWriteFailFailsClosed(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	orig := writeConsentRecordAtomic
	writeConsentRecordAtomic = func(string, []byte, os.FileMode) error {
		return errors.New("boom")
	}
	defer func() { writeConsentRecordAtomic = orig }()

	err := runConsentSet(cfg, []string{"--tier=full"})
	if err == nil {
		t.Fatal("runConsentSet succeeded, want error")
	}
	state, readErr := readConsentConfig(cfg)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if state.Tier != core.ConsentTierHashesOnly {
		t.Fatalf("tier after failed record write = %s, want hashes-only", state.Tier)
	}
}

func TestConsentRecordRoundTripsAndIsDemonstrable(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	if err := runConsentSet(cfg, []string{"--tier=prompts"}); err != nil {
		t.Fatal(err)
	}
	first, err := readConsentRecord(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first.PolicyVersion == "" || first.InstallID == "" || first.UpdatedAt.IsZero() || len(first.Grants) != 1 {
		t.Fatalf("incomplete consent record: %+v", first)
	}
	if err := runConsentSet(cfg, []string{"--tier=code"}); err != nil {
		t.Fatal(err)
	}
	second, err := readConsentRecord(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if second.InstallID != first.InstallID || len(second.Grants) != 2 {
		t.Fatalf("grant history not preserved: first=%+v second=%+v", first, second)
	}
}

func TestConsentNoAutoRepromptPersistsAcrossRestart(t *testing.T) {
	home := t.TempDir()
	cfg := testConsentConfig(home)
	if err := runConsentDecline(cfg, nil); err != nil {
		t.Fatal(err)
	}
	fresh := testConsentConfig(home)
	state, err := readConsentConfig(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Declined {
		t.Fatalf("decline did not persist: %+v", state)
	}
}

func TestConsentTTYGating(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg := Config{
		Args:    []string{"consent"},
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  &stderr,
		HomeDir: t.TempDir(),
		Getenv:  func(string) string { return "" },
	}
	err := Run(t.Context(), cfg)
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("Run(consent) err = %v, want ErrUsage", err)
	}
	if !strings.Contains(stdout.String(), "tier: hashes-only") || !strings.Contains(stderr.String(), "requires flags") {
		t.Fatalf("unexpected output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestConsentFilePermissionsCrossPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows chmod semantics differ")
	}
	cfg := testConsentConfig(t.TempDir())
	if err := runConsentSet(cfg, []string{"--tier=prompts"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{proofsweConfigPath(cfg), consentRecordPath(cfg)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s perm = %o, want 0600", path, got)
		}
	}
}

func TestConsentMachineInvariant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "proofswe-consent-*")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.RemoveAll(dir) }()
		cfg := testConsentConfig(dir)
		actions := rapid.SliceOfN(rapid.SampledFrom([]core.ConsentTier{
			core.ConsentTierHashesOnly,
			core.ConsentTierPrompts,
			core.ConsentTierActions,
		}), 1, 5).Draw(t, "actions")
		for _, tier := range actions {
			if err := runConsentSet(cfg, []string{"--tier=" + string(tier)}); err != nil {
				t.Fatalf("set %s: %v", tier, err)
			}
			state, err := readConsentConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if state.Tier != tier {
				t.Fatalf("config tier = %s, want %s", state.Tier, tier)
			}
		}
	})
}

func categoryStrings(categories []core.ConsentCategory) []string {
	out := make([]string, 0, len(categories))
	for _, category := range categories {
		out = append(out, string(category))
	}
	return out
}

func testConsentConfig(home string) Config {
	return Config{
		HomeDir: home,
		WorkDir: home,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Getenv:  func(string) string { return "" },
	}
}

func TestConsentPerRepoWritePreservesGlobal(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	if err := runConsentSet(cfg, []string{"--tier=code"}); err != nil {
		t.Fatal(err)
	}
	if err := runConsentSet(cfg, []string{"--tier=hashes-only", "--repo=repo_hash"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(proofsweConfigPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "tier=code") || !strings.Contains(text, "repo.repo_hash.tier=hashes-only") {
		t.Fatalf("repo write did not preserve global config:\n%s", text)
	}
}

func TestConsentRecordPathUnderStateDir(t *testing.T) {
	cfg := testConsentConfig(t.TempDir())
	if got := consentRecordPath(cfg); filepath.Dir(got) != proofsweStateDir(cfg) {
		t.Fatalf("consent record path = %s, want under state dir", got)
	}
}

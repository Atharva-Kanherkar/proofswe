package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/hashing"
)

const (
	consentSchemaVersion = 1
	consentPolicyVersion = "phase-a/1"
)

var writeConsentRecordAtomic = writeFileAtomic

type consentConfig struct {
	Tier       core.ConsentTier
	Categories map[core.ConsentCategory]bool
	RepoTiers  map[string]core.ConsentTier
	Declined   bool
	Malformed  bool
}

type consentResolution struct {
	Tier       core.ConsentTier
	Categories []core.ConsentCategory
	MaxGrant   core.ConsentTier
	Config     consentConfig
}

type consentRecord struct {
	SchemaVersion int            `json:"schema_version"`
	PolicyVersion string         `json:"policy_version"`
	InstallID     string         `json:"install_id"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Grants        []consentGrant `json:"grants,omitempty"`
	Declines      []consentGrant `json:"declines,omitempty"`
}

type consentGrant struct {
	Timestamp time.Time        `json:"ts"`
	Tier      core.ConsentTier `json:"tier"`
	RepoHash  string           `json:"repo_hash,omitempty"`
}

func runConsentCommand(cfg Config, args []string) error {
	cfg = cfg.withDefaults()
	if len(args) == 0 {
		if isTTY(cfg.Stdin) {
			return printConsentMenu(cfg)
		}
		if err := printConsentState(cfg); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cfg.Stderr, "proofswe consent requires flags when stdin is not a TTY; try `proofswe consent set --tier=prompts` or `proofswe consent show`")
		return fmt.Errorf("%w: consent requires flags in non-interactive mode", ErrUsage)
	}

	switch args[0] {
	case "show":
		return printConsentState(cfg)
	case "set":
		return runConsentSet(cfg, args[1:])
	case "enable":
		return runConsentEnable(cfg, args[1:])
	case "decline":
		return runConsentDecline(cfg, args[1:])
	default:
		return fmt.Errorf("%w: unknown consent command %q", ErrUsage, args[0])
	}
}

func runConsentSet(cfg Config, args []string) error {
	args = normalizeRepoFlag(args)
	flags := flag.NewFlagSet("consent set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var tierName, repoHash string
	flags.StringVar(&tierName, "tier", "", "consent tier")
	flags.StringVar(&repoHash, "repo", "", "remote hash or current")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 0 || tierName == "" {
		return fmt.Errorf("%w: consent set requires --tier", ErrUsage)
	}
	tier := core.NormalizeConsentTier(core.ConsentTier(tierName))
	if tierName != string(tier) {
		return fmt.Errorf("%w: unknown consent tier %q", ErrUsage, tierName)
	}
	if repoHash == "current" {
		var err error
		repoHash, err = currentRepoRemoteHash(cfg)
		if err != nil {
			return fmt.Errorf("resolve current repo hash: %w", err)
		}
	}

	prev, _ := effectiveConsent(cfg, "")
	if err := appendConsentGrant(cfg, tier, repoHash, false); err != nil {
		_ = writeTierConfig(cfg, core.ConsentTierHashesOnly, repoHash, nil, false)
		return fmt.Errorf("write consent record: %w", err)
	}
	if err := writeTierConfig(cfg, tier, repoHash, nil, false); err != nil {
		return fmt.Errorf("write proofswe config: %w", err)
	}
	if repoHash != "" || tier == core.ConsentTierHashesOnly || core.ConsentTierRank(tier) < core.ConsentTierRank(prev.Tier) {
		if err := purgeTasksToTier(cfg, tier, repoHash); err != nil {
			return fmt.Errorf("purge task records: %w", err)
		}
	}
	_, err := fmt.Fprintf(cfg.Stdout, "consent tier: %s\n", tier)
	return err
}

func runConsentEnable(cfg Config, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: consent enable requires one category", ErrUsage)
	}
	category, ok := parseConsentCategory(args[0])
	if !ok {
		return fmt.Errorf("%w: unknown consent category %q", ErrUsage, args[0])
	}
	requiredTier := tierForCategory(category)
	if err := appendConsentGrant(cfg, requiredTier, "", false); err != nil {
		_ = writeTierConfig(cfg, core.ConsentTierHashesOnly, "", nil, false)
		return fmt.Errorf("write consent record: %w", err)
	}
	state, err := readConsentConfig(cfg)
	if err != nil {
		return err
	}
	state.Categories[category] = true
	if err := writeTierConfig(cfg, state.Tier, "", state.Categories, state.Declined); err != nil {
		return fmt.Errorf("write proofswe config: %w", err)
	}
	_, err = fmt.Fprintf(cfg.Stdout, "consent category enabled: %s\n", category)
	return err
}

func runConsentDecline(cfg Config, args []string) error {
	flags := flag.NewFlagSet("consent decline", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var tierName string
	flags.StringVar(&tierName, "tier", string(core.ConsentTierFull), "declined tier")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	tier := core.NormalizeConsentTier(core.ConsentTier(tierName))
	if err := appendConsentGrant(cfg, tier, "", true); err != nil {
		return fmt.Errorf("write consent decline: %w", err)
	}
	if err := writeTierConfig(cfg, core.ConsentTierHashesOnly, "", nil, true); err != nil {
		return fmt.Errorf("write proofswe config: %w", err)
	}
	if err := purgeTasksToTier(cfg, core.ConsentTierHashesOnly, ""); err != nil {
		return fmt.Errorf("purge task records: %w", err)
	}
	_, err := fmt.Fprintln(cfg.Stdout, "consent declined; proofswe will not prompt again")
	return err
}

func printConsentMenu(cfg Config) error {
	if err := printConsentState(cfg); err != nil {
		return err
	}
	_, err := fmt.Fprint(cfg.Stdout, "\nUse `proofswe consent set --tier=<tier>` or `proofswe consent enable <category>` to change consent.\n")
	return err
}

func printConsentState(cfg Config) error {
	resolved, err := effectiveConsent(cfg, "")
	if err != nil {
		return err
	}
	categories := make([]string, 0, len(resolved.Categories))
	for _, category := range resolved.Categories {
		categories = append(categories, string(category))
	}
	sort.Strings(categories)
	_, err = fmt.Fprintf(cfg.Stdout, "tier: %s\nmax_grant: %s\ncategories: %s\ndeclined: %t\n", resolved.Tier, resolved.MaxGrant, strings.Join(categories, ","), resolved.Config.Declined)
	return err
}

func readConsentConfig(cfg Config) (consentConfig, error) {
	state := consentConfig{
		Tier:       core.ConsentTierHashesOnly,
		Categories: map[core.ConsentCategory]bool{},
		RepoTiers:  map[string]core.ConsentTier{},
	}
	data, err := os.ReadFile(proofsweConfigPath(cfg))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}

	seenTier := false
	seenRepos := map[string]core.ConsentTier{}
	seenCategories := map[core.ConsentCategory]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch {
		case key == "tier":
			tier := core.NormalizeConsentTier(core.ConsentTier(value))
			if value != string(tier) || seenTier {
				state.Malformed = true
				return failClosedConsentConfig(state), nil
			}
			seenTier = true
			state.Tier = tier
		case strings.HasPrefix(key, "repo.") && strings.HasSuffix(key, ".tier"):
			repoHash := strings.TrimSuffix(strings.TrimPrefix(key, "repo."), ".tier")
			tier := core.NormalizeConsentTier(core.ConsentTier(value))
			if repoHash == "" || value != string(tier) {
				state.Malformed = true
				return failClosedConsentConfig(state), nil
			}
			if prior, ok := seenRepos[repoHash]; ok && prior != tier {
				state.Malformed = true
				return failClosedConsentConfig(state), nil
			}
			seenRepos[repoHash] = tier
			state.RepoTiers[repoHash] = tier
		case strings.HasPrefix(key, "category."):
			category, ok := parseConsentCategory(strings.TrimPrefix(key, "category."))
			enabled, boolOK := parseConfigBool(value)
			if !ok || !boolOK {
				state.Malformed = true
				return failClosedConsentConfig(state), nil
			}
			if prior, ok := seenCategories[category]; ok && prior != enabled {
				state.Malformed = true
				return failClosedConsentConfig(state), nil
			}
			seenCategories[category] = enabled
			state.Categories[category] = enabled
		case key == "declined":
			declined, ok := parseConfigBool(value)
			if !ok {
				state.Malformed = true
				return failClosedConsentConfig(state), nil
			}
			state.Declined = declined
		}
	}
	return state, nil
}

func failClosedConsentConfig(state consentConfig) consentConfig {
	return consentConfig{
		Tier:       core.ConsentTierHashesOnly,
		Categories: map[core.ConsentCategory]bool{},
		RepoTiers:  map[string]core.ConsentTier{},
		Declined:   state.Declined,
		Malformed:  true,
	}
}

func effectiveConsent(cfg Config, repoHash string) (consentResolution, error) {
	return effectiveConsentWithFlag(cfg, repoHash, nil)
}

func effectiveConsentWithFlag(cfg Config, repoHash string, flagTier *core.ConsentTier) (consentResolution, error) {
	state, err := readConsentConfig(cfg)
	if err != nil {
		return consentResolution{}, err
	}
	tier := state.Tier
	if repoHash != "" {
		if repoTier, ok := state.RepoTiers[repoHash]; ok {
			tier = repoTier
		}
	}
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if envTier := cfg.Getenv("PROOFSWE_TIER"); envTier != "" {
		normalized := core.NormalizeConsentTier(core.ConsentTier(envTier))
		if envTier == string(normalized) {
			tier = normalized
		} else {
			tier = core.ConsentTierHashesOnly
		}
	}
	if flagTier != nil {
		tier = core.NormalizeConsentTier(*flagTier)
	}
	maxGrant := maxRecordedConsentTier(cfg)
	if core.ConsentTierRank(tier) > core.ConsentTierRank(maxGrant) {
		tier = maxGrant
	}

	categories := core.CategoriesForTier(tier)
	for category, enabled := range state.Categories {
		if !enabled {
			continue
		}
		if core.ConsentTierRank(tierForCategory(category)) <= core.ConsentTierRank(maxGrant) {
			categories = append(categories, category)
		}
	}
	categories = dedupeCategories(categories)
	return consentResolution{Tier: tier, Categories: categories, MaxGrant: maxGrant, Config: state}, nil
}

func maxRecordedConsentTier(cfg Config) core.ConsentTier {
	record, err := readConsentRecord(cfg)
	if err != nil || len(record.Grants) == 0 {
		return core.ConsentTierHashesOnly
	}
	maxTier := core.ConsentTierHashesOnly
	for _, grant := range record.Grants {
		maxTier = core.MaxConsentTier(maxTier, grant.Tier)
	}
	return maxTier
}

func readConsentRecord(cfg Config) (consentRecord, error) {
	data, err := os.ReadFile(consentRecordPath(cfg))
	if err != nil {
		return consentRecord{}, err
	}
	var record consentRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return consentRecord{}, err
	}
	return record, nil
}

func appendConsentGrant(cfg Config, tier core.ConsentTier, repoHash string, declined bool) error {
	record, err := readConsentRecord(cfg)
	if errors.Is(err, os.ErrNotExist) {
		record = consentRecord{
			SchemaVersion: consentSchemaVersion,
			PolicyVersion: consentPolicyVersion,
			InstallID:     "local-" + fmt.Sprint(time.Now().UTC().UnixNano()),
		}
	} else if err != nil {
		record = consentRecord{
			SchemaVersion: consentSchemaVersion,
			PolicyVersion: consentPolicyVersion,
			InstallID:     "local-" + fmt.Sprint(time.Now().UTC().UnixNano()),
		}
	}
	if record.SchemaVersion == 0 {
		record.SchemaVersion = consentSchemaVersion
	}
	if record.PolicyVersion == "" {
		record.PolicyVersion = consentPolicyVersion
	}
	if record.InstallID == "" {
		record.InstallID = "local-" + fmt.Sprint(time.Now().UTC().UnixNano())
	}
	now := time.Now().UTC()
	record.UpdatedAt = now
	grant := consentGrant{Timestamp: now, Tier: tier, RepoHash: repoHash}
	if declined {
		record.Declines = append(record.Declines, grant)
	} else {
		record.Grants = append(record.Grants, grant)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeConsentRecordAtomic(consentRecordPath(cfg), data, 0o600)
}

func writeTierConfig(cfg Config, tier core.ConsentTier, repoHash string, categories map[core.ConsentCategory]bool, declined bool) error {
	data, err := os.ReadFile(proofsweConfigPath(cfg))
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		return err
	}
	lines := filterConsentConfigLines(strings.Split(string(data), "\n"), repoHash)
	if repoHash == "" {
		lines = append(lines, "tier="+string(tier))
	} else {
		lines = append(lines, "repo."+repoHash+".tier="+string(tier))
	}
	for _, category := range sortedEnabledCategories(categories) {
		lines = append(lines, "category."+string(category)+"=true")
	}
	if declined {
		lines = append(lines, "declined=true")
	}
	text := strings.Join(nonEmpty(lines), "\n")
	if text != "" {
		text += "\n"
	}
	return writeFileAtomic(proofsweConfigPath(cfg), []byte(text), 0o600)
}

func filterConsentConfigLines(lines []string, repoHash string) []string {
	out := make([]string, 0, len(lines))
	repoKey := "repo." + repoHash + ".tier"
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		key = strings.TrimSpace(key)
		if ok && ((repoHash == "" && (key == "tier" || strings.HasPrefix(key, "category.") || key == "declined")) || (repoHash != "" && key == repoKey)) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func nonEmpty(lines []string) []string {
	out := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func sortedEnabledCategories(categories map[core.ConsentCategory]bool) []core.ConsentCategory {
	if len(categories) == 0 {
		return nil
	}
	var out []core.ConsentCategory
	for _, category := range core.ConsentCategories() {
		if categories[category] {
			out = append(out, category)
		}
	}
	return out
}

func parseConsentCategory(value string) (core.ConsentCategory, bool) {
	category := core.ConsentCategory(value)
	for _, known := range core.ConsentCategories() {
		if category == known {
			return category, true
		}
	}
	return "", false
}

func parseConfigBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func tierForCategory(category core.ConsentCategory) core.ConsentTier {
	switch category {
	case core.CategoryStartingPrompt, core.CategoryAllPrompts:
		return core.ConsentTierPrompts
	case core.CategoryAssistantMsgs, core.CategoryToolCalls, core.CategoryToolOutputs:
		return core.ConsentTierActions
	case core.CategoryCodeDiffs, core.CategoryRepoLinkage:
		return core.ConsentTierCode
	case core.CategoryFullTranscript:
		return core.ConsentTierFull
	default:
		return core.ConsentTierHashesOnly
	}
}

func dedupeCategories(categories []core.ConsentCategory) []core.ConsentCategory {
	set := core.CategorySet(categories)
	out := make([]core.ConsentCategory, 0, len(set))
	for _, category := range core.ConsentCategories() {
		if set[category] {
			out = append(out, category)
		}
	}
	return out
}

func normalizeRepoFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--repo" {
			out = append(out, "--repo=current")
			continue
		}
		out = append(out, arg)
	}
	return out
}

func currentRepoRemoteHash(cfg Config) (string, error) {
	salt, err := hashing.LoadSalt(proofsweStateDir(cfg))
	if err != nil {
		return "", err
	}
	dir := cfg.WorkDir
	if dir == "" {
		var wdErr error
		dir, wdErr = os.Getwd()
		if wdErr != nil {
			return "", wdErr
		}
	}
	out, err := runGit(dir, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return hashing.New(salt).StringHash(strings.TrimSpace(string(out))), nil
}

func isTTY(r io.Reader) bool {
	file, ok := r.(*os.File)
	if !ok || file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func consentRecordPath(cfg Config) string {
	return filepath.Join(proofsweStateDir(cfg), "consent.json")
}

func purgeTasksToTier(cfg Config, tier core.ConsentTier, repoHash string) error {
	tasksDir := filepath.Join(proofsweStateDir(cfg), "tasks")
	entries, err := os.ReadDir(tasksDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(tasksDir, entry.Name())
		if repoHash == "" {
			errs = append(errs, removeTaskCompanions(dir))
		}
		taskPath := filepath.Join(dir, "task.json")
		task, err := readTaskRecordFile(taskPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", taskPath, err))
			continue
		}
		if repoHash != "" && task.Repo.RemoteHash != repoHash {
			continue
		}
		projected := core.Project(task, tier)
		data, err := marshalTaskJSON(projected)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := writeTaskFileAtomic(taskPath, data); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := removeTaskCompanions(dir); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func removeTaskCompanions(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		if entry.Name() == "task.json" || entry.IsDir() {
			continue
		}
		errs = append(errs, os.Remove(filepath.Join(dir, entry.Name())))
	}
	return errors.Join(errs...)
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	proofsweTag     = "proofswe:v0"
	noticeLine      = "proofswe observing locally; disable: proofswe off"
	codexBlockStart = "# BEGIN proofswe:v0 hooks"
	codexBlockEnd   = "# END proofswe:v0 hooks"
)

var (
	hookHarnesses = map[string]bool{
		"claudecode": true,
		"codex":      true,
	}
	claudeHookEvents = []string{"SessionStart", "SessionEnd", "Stop"}
	codexHookEvents  = []string{"SessionStart", "Stop"}
)

func enableHooks(cfg Config) error {
	if err := upsertClaudeHooks(claudeSettingsPath(cfg), cfg.ExePath); err != nil {
		return fmt.Errorf("enable claude code hooks: %w", err)
	}
	if err := upsertCodexHooks(codexConfigPath(cfg), cfg.ExePath); err != nil {
		return fmt.Errorf("enable codex hooks: %w", err)
	}

	_, err := fmt.Fprintln(cfg.Stdout, "proofswe hooks enabled")
	return err
}

func disableHooks(cfg Config, args []string) error {
	if len(args) != 1 || args[0] != "--hooks" {
		return fmt.Errorf("%w: disable requires --hooks", ErrUsage)
	}
	if err := removeClaudeHooks(claudeSettingsPath(cfg)); err != nil {
		return fmt.Errorf("disable claude code hooks: %w", err)
	}
	if err := removeCodexHooks(codexConfigPath(cfg)); err != nil {
		return fmt.Errorf("disable codex hooks: %w", err)
	}

	_, err := fmt.Fprintln(cfg.Stdout, "proofswe hooks disabled")
	return err
}

func setEnabled(cfg Config, enabled bool) error {
	value := "enabled=false\n"
	label := "off"
	if enabled {
		value = "enabled=true\n"
		label = "on"
	}

	if err := writeFileAtomic(proofsweConfigPath(cfg), []byte(value), 0o600); err != nil {
		return fmt.Errorf("write proofswe config: %w", err)
	}
	_, err := fmt.Fprintf(cfg.Stdout, "proofswe %s\n", label)
	return err
}

func runHook(ctx context.Context, cfg Config, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("%w: hook requires harness and event", ErrUsage)
	}
	if !hookHarnesses[args[0]] {
		return fmt.Errorf("%w: unsupported hook harness %q", ErrUsage, args[0])
	}

	disabled, err := hookDisabled(cfg)
	if err != nil {
		return fmt.Errorf("check hook kill-switch: %w", err)
	}
	if disabled {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if args[1] == "SessionStart" {
		_, err := fmt.Fprintln(cfg.Stdout, noticeLine)
		return err
	}

	return nil
}

func printStatus(cfg Config) error {
	enabled, err := readEnabled(cfg)
	if err != nil {
		return fmt.Errorf("read proofswe config: %w", err)
	}

	claudeWired, err := claudeHooksWired(claudeSettingsPath(cfg))
	if err != nil {
		return fmt.Errorf("read claude code hooks: %w", err)
	}
	codexWired, err := codexHooksWired(codexConfigPath(cfg))
	if err != nil {
		return fmt.Errorf("read codex hooks: %w", err)
	}

	_, err = fmt.Fprintf(cfg.Stdout, "enabled: %t\nclaudecode hooks: %s\ncodex hooks: %s\n", enabled, wiredLabel(claudeWired), wiredLabel(codexWired))
	return err
}

func hookDisabled(cfg Config) (bool, error) {
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.Getenv("PROOFSWE_OFF") == "1" || cfg.Getenv("DO_NOT_TRACK") == "1" {
		return true, nil
	}

	enabled, err := readEnabled(cfg)
	if err != nil {
		return false, err
	}
	if !enabled {
		return true, nil
	}

	ignored, err := repoIgnored(cfg.WorkDir)
	if err != nil {
		return false, err
	}
	return ignored, nil
}

func readEnabled(cfg Config) (bool, error) {
	data, err := os.ReadFile(proofsweConfigPath(cfg))
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "enabled" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "false", "0", "no", "off":
			return false, nil
		default:
			return true, nil
		}
	}

	return true, nil
}

func repoIgnored(workDir string) (bool, error) {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return false, err
		}
	}

	info, err := os.Stat(filepath.Join(workDir, ".proofswe-ignore"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}

func upsertClaudeHooks(path, exePath string) error {
	settings, err := readJSONObject(path)
	if err != nil {
		return err
	}

	hooks := objectAt(settings, "hooks")
	for _, event := range claudeHookEvents {
		groups := filterTaggedGroups(arrayAt(hooks, event))
		groups = append(groups, hookGroup(event, "claudecode", exePath))
		hooks[event] = groups
	}
	settings["hooks"] = hooks

	return writeJSONFile(path, settings)
}

func removeClaudeHooks(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	settings, err := readJSONObject(path)
	if err != nil {
		return err
	}

	rawHooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}

	for _, event := range claudeHookEvents {
		groups := filterTaggedGroups(arrayAt(rawHooks, event))
		if len(groups) == 0 {
			delete(rawHooks, event)
		} else {
			rawHooks[event] = groups
		}
	}
	if len(rawHooks) == 0 {
		delete(settings, "hooks")
	}

	return writeJSONFile(path, settings)
}

func claudeHooksWired(path string) (bool, error) {
	settings, err := readJSONObject(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	rawHooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false, nil
	}
	for _, event := range claudeHookEvents {
		if !hasTaggedGroup(arrayAt(rawHooks, event)) {
			return false, nil
		}
	}
	return true, nil
}

func upsertCodexHooks(path, exePath string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data = nil
	} else if err != nil {
		return err
	}

	trimmed := removeCodexBlock(string(data))
	if strings.TrimSpace(trimmed) != "" && !strings.HasSuffix(trimmed, "\n") {
		trimmed += "\n"
	}
	block := codexHookBlock(exePath)
	return writeFileAtomic(path, []byte(trimmed+block), 0o600)
}

func removeCodexHooks(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(removeCodexBlock(string(data))), 0o600)
}

func codexHooksWired(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	text := string(data)
	return strings.Contains(text, codexBlockStart) && strings.Contains(text, codexBlockEnd), nil
}

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		decoded = map[string]any{}
	}
	return decoded, nil
}

func writeJSONFile(path string, value map[string]any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0o600)
}

func objectAt(parent map[string]any, key string) map[string]any {
	if child, ok := parent[key].(map[string]any); ok {
		return child
	}
	child := map[string]any{}
	parent[key] = child
	return child
}

func arrayAt(parent map[string]any, key string) []any {
	if value, ok := parent[key].([]any); ok {
		return value
	}
	return nil
}

func hookGroup(event, harness, exePath string) map[string]any {
	return map[string]any{
		"matcher": matcherFor(event),
		"hooks": []any{
			map[string]any{
				"type":          "command",
				"command":       hookCommand(exePath, harness, event),
				"statusMessage": "proofswe local capture",
				"proofswe_tag":  proofsweTag,
			},
		},
	}
}

func hasTaggedGroup(groups []any) bool {
	for _, group := range groups {
		if groupHasTag(group) {
			return true
		}
	}
	return false
}

func filterTaggedGroups(groups []any) []any {
	filtered := make([]any, 0, len(groups))
	for _, group := range groups {
		if groupHasTag(group) {
			continue
		}
		filtered = append(filtered, group)
	}
	return filtered
}

func groupHasTag(group any) bool {
	groupMap, ok := group.(map[string]any)
	if !ok {
		return false
	}
	handlers, _ := groupMap["hooks"].([]any)
	for _, handler := range handlers {
		handlerMap, ok := handler.(map[string]any)
		if ok && handlerMap["proofswe_tag"] == proofsweTag {
			return true
		}
	}
	return false
}

func codexHookBlock(exePath string) string {
	var b strings.Builder
	b.WriteString(codexBlockStart + "\n")
	for _, event := range codexHookEvents {
		fmt.Fprintf(&b, "[[hooks.%s]]\n", event)
		if matcher := matcherFor(event); matcher != "" {
			fmt.Fprintf(&b, "matcher = %q\n", matcher)
		}
		fmt.Fprintf(&b, "[[hooks.%s.hooks]]\n", event)
		b.WriteString("type = \"command\"\n")
		fmt.Fprintf(&b, "command = %q\n", hookCommand(exePath, "codex", event))
		b.WriteString("statusMessage = \"proofswe local capture\"\n")
		b.WriteString("proofswe_tag = \"proofswe:v0\"\n\n")
	}
	b.WriteString(codexBlockEnd + "\n")
	return b.String()
}

func removeCodexBlock(text string) string {
	for {
		start := strings.Index(text, codexBlockStart)
		if start < 0 {
			return text
		}
		end := strings.Index(text[start:], codexBlockEnd)
		if end < 0 {
			return text
		}
		end += start + len(codexBlockEnd)
		if end < len(text) && text[end] == '\n' {
			end++
		}
		if start > 0 && text[start-1] == '\n' && end < len(text) && text[end] == '\n' {
			end++
		}
		text = text[:start] + text[end:]
	}
}

func matcherFor(event string) string {
	switch event {
	case "SessionStart":
		return "startup|resume|clear|compact"
	case "SessionEnd":
		return "clear|resume|logout|prompt_input_exit|bypass_permissions_disabled|other"
	default:
		return ""
	}
}

func hookCommand(exePath, harness, event string) string {
	return shellQuote(exePath) + " hook " + shellQuote(harness) + " " + shellQuote(event)
}

func shellQuote(s string) string {
	if s == "" {
		return "proofswe"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	dirFile, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer dirFile.Close()
	_ = dirFile.Sync()
	return nil
}

func wiredLabel(wired bool) string {
	if wired {
		return "wired"
	}
	return "missing"
}

func proofsweConfigPath(cfg Config) string {
	return filepath.Join(homeDir(cfg), ".proofswe", "config")
}

func claudeSettingsPath(cfg Config) string {
	return filepath.Join(homeDir(cfg), ".claude", "settings.json")
}

func codexConfigPath(cfg Config) string {
	return filepath.Join(homeDir(cfg), ".codex", "config.toml")
}

func homeDir(cfg Config) string {
	if cfg.HomeDir != "" {
		return cfg.HomeDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	proofsweTag     = "proofswe:v0"
	noticeLine      = "proofswe observing locally; disable: proofswe off; consent: proofswe consent"
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
	label := "off"
	if enabled {
		label = "on"
	}

	data, err := updateEnabledConfig(proofsweConfigPath(cfg), enabled)
	if err != nil {
		return fmt.Errorf("read proofswe config: %w", err)
	}
	if err := writeFileAtomic(proofsweConfigPath(cfg), data, 0o600); err != nil {
		return fmt.Errorf("write proofswe config: %w", err)
	}
	_, err = fmt.Fprintf(cfg.Stdout, "proofswe %s\n", label)
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

	switch args[1] {
	case "SessionStart":
		if _, err := fmt.Fprintln(cfg.Stderr, noticeLine); err != nil {
			return err
		}
		if resolveErr := resolvePending(cfg, resolveOptions{Maturity: defaultResolveMaturity, Now: time.Now, MaxRecords: hookResolveLimit}); resolveErr != nil {
			// Best-effort: resolving matured records must never disrupt startup.
			_, _ = fmt.Fprintf(cfg.Stderr, "proofswe: resolve skipped: %v\n", resolveErr)
		}
		return nil
	case "SessionEnd", "Stop":
		// Codex fires Stop per turn (no SessionEnd); the record is idempotent, so
		// each fire overwrites with the session's current cumulative diff.
		in, parseErr := parseHookInput(cfg.Stdin)
		if parseErr != nil {
			_, _ = fmt.Fprintf(cfg.Stderr, "proofswe: snapshot skipped (bad hook input): %v\n", parseErr)
			return nil
		}
		if snapErr := snapshot(cfg, args[0], in, time.Now()); snapErr != nil {
			// Best-effort: capture failures must never disrupt the user's session.
			_, _ = fmt.Fprintf(cfg.Stderr, "proofswe: snapshot skipped: %v\n", snapErr)
		}
		return nil
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

	resolved, consentErr := effectiveConsent(cfg, "")
	if consentErr != nil {
		return fmt.Errorf("read proofswe consent: %w", consentErr)
	}

	_, err = fmt.Fprintf(cfg.Stdout, "enabled: %t\nclaudecode hooks: %s\ncodex hooks: %s\nconsent tier: %s\ndefault guarantee: hashes-only stores no raw prompts, code, remotes, session ids, tool outputs, or patches; use `proofswe consent` to inspect opt-in tiers.\n", enabled, wiredLabel(claudeWired), wiredLabel(codexWired), resolved.Tier)
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

func updateEnabledConfig(path string, enabled bool) ([]byte, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if enabled {
			return []byte("enabled=true\n"), nil
		}
		return []byte("enabled=false\n"), nil
	}
	if err != nil {
		return nil, err
	}

	value := "enabled=false"
	if enabled {
		value = "enabled=true"
	}

	lines := strings.SplitAfter(string(data), "\n")
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(key) != "enabled" {
			continue
		}

		lineEnding := ""
		if strings.HasSuffix(line, "\n") {
			lineEnding = "\n"
		}
		lines[i] = value + lineEnding
		replaced = true
		break
	}

	text := strings.Join(lines, "")
	if !replaced {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += value + "\n"
	}

	return []byte(text), nil
}

func repoIgnored(workDir string) (bool, error) {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return false, err
		}
	}

	for {
		info, err := os.Stat(filepath.Join(workDir, ".proofswe-ignore"))
		if err == nil {
			return !info.IsDir(), nil
		}
		if !os.IsNotExist(err) {
			return false, err
		}

		parent := filepath.Dir(workDir)
		if parent == workDir {
			return false, nil
		}
		workDir = parent
	}
}

func upsertClaudeHooks(path, exePath string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data = nil
	} else if err != nil {
		return err
	}

	hooks, err := claudeHooksObject(data)
	if err != nil {
		return err
	}
	for _, event := range claudeHookEvents {
		groups := filterTaggedGroups(arrayAt(hooks, event))
		groups = append(groups, hookGroup(event, "claudecode", exePath))
		hooks[event] = groups
	}

	return writeFileAtomic(path, replaceClaudeHooks(data, hooks), 0o600)
}

func removeClaudeHooks(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	rawHooks, err := claudeHooksObject(data)
	if err != nil {
		return err
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
		next, err := removeClaudeHooksProperty(data)
		if err != nil {
			return err
		}
		return writeFileAtomic(path, next, 0o600)
	}

	return writeFileAtomic(path, replaceClaudeHooks(data, rawHooks), 0o600)
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

func claudeHooksObject(data []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}

	prop, ok, err := findTopLevelJSONProperty(data, "hooks")
	if err != nil || !ok {
		return map[string]any{}, err
	}

	var hooks map[string]any
	if err := json.Unmarshal(data[prop.valueStart:prop.valueEnd], &hooks); err != nil {
		return nil, err
	}
	if hooks == nil {
		hooks = map[string]any{}
	}
	return hooks, nil
}

func replaceClaudeHooks(data []byte, hooks map[string]any) []byte {
	rawHooks := mustMarshalIndented(hooks)
	if len(bytes.TrimSpace(data)) == 0 {
		return []byte("{\n  \"hooks\": " + indentContinuation(string(rawHooks), "  ") + "\n}\n")
	}

	prop, ok, err := findTopLevelJSONProperty(data, "hooks")
	if err == nil && ok {
		next := make([]byte, 0, len(data)-prop.valueEnd+prop.valueStart+len(rawHooks))
		next = append(next, data[:prop.valueStart]...)
		next = append(next, rawHooks...)
		next = append(next, data[prop.valueEnd:]...)
		return next
	}

	insertAt := lastNonSpaceIndex(data)
	if insertAt < 0 || data[insertAt] != '}' {
		return []byte("{\n  \"hooks\": " + indentContinuation(string(rawHooks), "  ") + "\n}\n")
	}

	prefix := ",\n  \"hooks\": "
	if strings.TrimSpace(string(data[:insertAt])) == "{" {
		prefix = "\n  \"hooks\": "
	}

	next := make([]byte, 0, len(data)+len(prefix)+len(rawHooks)+2)
	next = append(next, data[:insertAt]...)
	next = append(next, prefix...)
	next = append(next, []byte(indentContinuation(string(rawHooks), "  "))...)
	next = append(next, '\n')
	next = append(next, data[insertAt:]...)
	return next
}

func removeClaudeHooksProperty(data []byte) ([]byte, error) {
	prop, ok, err := findTopLevelJSONProperty(data, "hooks")
	if err != nil || !ok {
		return data, err
	}

	next := make([]byte, 0, len(data)-(prop.memberEnd-prop.memberStart))
	next = append(next, data[:prop.memberStart]...)
	next = append(next, data[prop.memberEnd:]...)
	return next, nil
}

func mustMarshalIndented(value any) []byte {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return data
}

func indentContinuation(text, indent string) string {
	return strings.ReplaceAll(text, "\n", "\n"+indent)
}

type jsonProperty struct {
	valueStart  int
	valueEnd    int
	memberStart int
	memberEnd   int
}

func findTopLevelJSONProperty(data []byte, key string) (jsonProperty, bool, error) {
	if !json.Valid(data) {
		return jsonProperty{}, false, fmt.Errorf("invalid JSON")
	}

	for i := 0; i < len(data); i++ {
		if data[i] != '"' || jsonDepthAt(data, i) != 1 {
			continue
		}

		keyEnd, err := jsonStringEnd(data, i)
		if err != nil {
			return jsonProperty{}, false, err
		}

		var decodedKey string
		if err := json.Unmarshal(data[i:keyEnd], &decodedKey); err != nil {
			return jsonProperty{}, false, err
		}

		colon := skipJSONSpace(data, keyEnd)
		if colon >= len(data) || data[colon] != ':' {
			i = keyEnd - 1
			continue
		}

		valueStart := skipJSONSpace(data, colon+1)
		valueEnd, err := jsonValueEnd(data, valueStart)
		if err != nil {
			return jsonProperty{}, false, err
		}

		if decodedKey == key {
			memberStart := i
			for j := i - 1; j >= 0; j-- {
				if isJSONSpace(data[j]) {
					continue
				}
				if data[j] == ',' {
					memberStart = j
				}
				break
			}

			memberEnd := valueEnd
			next := skipJSONSpace(data, memberEnd)
			if memberStart == i && next < len(data) && data[next] == ',' {
				memberEnd = next + 1
				if memberEnd < len(data) && data[memberEnd] == '\n' {
					memberEnd++
				}
			}

			return jsonProperty{
				valueStart:  valueStart,
				valueEnd:    valueEnd,
				memberStart: memberStart,
				memberEnd:   memberEnd,
			}, true, nil
		}

		i = valueEnd - 1
	}

	return jsonProperty{}, false, nil
}

func jsonDepthAt(data []byte, pos int) int {
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < pos; i++ {
		c := data[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
	}
	return depth
}

func jsonStringEnd(data []byte, start int) (int, error) {
	escaped := false
	for i := start + 1; i < len(data); i++ {
		c := data[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated JSON string")
}

func jsonValueEnd(data []byte, start int) (int, error) {
	if start >= len(data) {
		return 0, fmt.Errorf("missing JSON value")
	}
	if data[start] == '"' {
		return jsonStringEnd(data, start)
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(data); i++ {
		c := data[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			if depth == 0 {
				return i, nil
			}
			depth--
		case ',':
			if depth == 0 {
				return i, nil
			}
		}
	}

	return len(data), nil
}

func skipJSONSpace(data []byte, start int) int {
	for start < len(data) && isJSONSpace(data[start]) {
		start++
	}
	return start
}

func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t'
}

func lastNonSpaceIndex(data []byte) int {
	for i := len(data) - 1; i >= 0; i-- {
		if !isJSONSpace(data[i]) {
			return i
		}
	}
	return -1
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
	return hookCommandForOS(exePath, harness, event, runtime.GOOS)
}

func hookCommandForOS(exePath, harness, event, goos string) string {
	return shellQuoteForOS(exePath, goos) + " hook " + shellQuoteForOS(harness, goos) + " " + shellQuoteForOS(event, goos)
}

func shellQuoteForOS(s, goos string) string {
	if s == "" {
		return "proofswe"
	}
	if goos == "windows" {
		if !strings.ContainsAny(s, " \t\n\"&|<>^") {
			return s
		}
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	if strings.ContainsAny(s, " \t\n'\"\\$`!#&;()<>|*?[]{}") {
		return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
	}
	return s
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
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Chmod(perm); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false

	dirFile, err := os.Open(dir)
	if err != nil {
		return nil
	}
	_ = dirFile.Sync()
	_ = dirFile.Close()
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

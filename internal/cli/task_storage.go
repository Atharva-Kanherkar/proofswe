package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
)

func marshalTaskJSON(task core.Task) ([]byte, error) {
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func readTaskRecordFile(path string) (core.Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return core.Task{}, err
	}
	var task core.Task
	if err := json.Unmarshal(data, &task); err != nil {
		return core.Task{}, err
	}
	return task, nil
}

func writeTaskFileAtomic(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse to replace symlinked task file %s", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

func taskRecordPath(cfg Config, taskID string) (string, error) {
	component, err := safeTaskPathComponent(taskID)
	if err != nil {
		return "", err
	}
	root := filepath.Join(proofsweStateDir(cfg), "tasks")
	path := filepath.Join(root, component, "task.json")
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if cleanPath != filepath.Join(cleanRoot, component, "task.json") || !strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("task path escapes tasks dir")
	}
	return path, nil
}

func safeTaskPathComponent(taskID string) (string, error) {
	if taskID == "" || strings.ContainsRune(taskID, 0) {
		return "", fmt.Errorf("empty or invalid task id")
	}
	var b strings.Builder
	for _, r := range taskID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r == ':':
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}
	component := b.String()
	for strings.Contains(component, "..") {
		component = strings.ReplaceAll(component, "..", "__")
	}
	if component == "." || component == ".." || filepath.IsAbs(component) {
		return "", fmt.Errorf("unsafe task id %q", taskID)
	}
	return component, nil
}

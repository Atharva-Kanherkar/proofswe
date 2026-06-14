package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/hashing"
)

func runShowCommand(cfg Config, args []string) error {
	cfg = cfg.withDefaults()
	if len(args) != 1 || args[0] == "" {
		return fmt.Errorf("%w: show requires a session id", ErrUsage)
	}
	task, err := findTaskRecordBySession(cfg, args[0])
	if err != nil {
		return err
	}
	resolved, err := effectiveConsent(cfg, task.Repo.RemoteHash)
	if err != nil {
		return err
	}
	projected := core.ProjectWithCategories(task, resolved.Tier, resolved.Categories)
	data, err := marshalTaskJSON(projected)
	if err != nil {
		return err
	}
	_, err = cfg.Stdout.Write(data)
	return err
}

func findTaskRecordBySession(cfg Config, sessionID string) (core.Task, error) {
	tasksDir := filepath.Join(proofsweStateDir(cfg), "tasks")
	entries, err := os.ReadDir(tasksDir)
	if errors.Is(err, os.ErrNotExist) {
		return core.Task{}, fmt.Errorf("task record for session %q not found", sessionID)
	}
	if err != nil {
		return core.Task{}, err
	}
	sessionHash := ""
	if salt, err := hashing.LoadSalt(proofsweStateDir(cfg)); err == nil {
		sessionHash = hashing.New(salt).StringHash(sessionID)
	}
	var corruptErr error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(tasksDir, entry.Name(), "task.json")
		task, err := readTaskRecordFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			corruptErr = fmt.Errorf("read task record %s: %w", path, err)
			continue
		}
		if string(task.Session.ID) == sessionID || (sessionHash != "" && task.Session.IDHash == sessionHash) {
			return task, nil
		}
	}
	if corruptErr != nil {
		return core.Task{}, corruptErr
	}
	return core.Task{}, fmt.Errorf("task record for session %q not found", sessionID)
}

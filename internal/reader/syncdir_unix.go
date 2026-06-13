//go:build !windows

package reader

import (
	"fmt"
	"os"
)

func syncDir(path string) error {
	dirFile, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open dir: %w", err)
	}
	if err := dirFile.Sync(); err != nil {
		_ = dirFile.Close()
		return fmt.Errorf("sync dir: %w", err)
	}
	if err := dirFile.Close(); err != nil {
		return fmt.Errorf("close dir: %w", err)
	}
	return nil
}

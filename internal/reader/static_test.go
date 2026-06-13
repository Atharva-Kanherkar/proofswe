package reader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoForbiddenStreamingAPIs(t *testing.T) {
	root := "."
	forbidden := []string{
		"bufio." + "Scanner",
		"New" + "Scanner",
		"m" + "map",
		"M" + "map",
		"syscall." + "M" + "map",
	}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(data)
		for _, token := range forbidden {
			if strings.Contains(content, token) {
				t.Fatalf("%s contains forbidden streaming API token %q", path, token)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
}

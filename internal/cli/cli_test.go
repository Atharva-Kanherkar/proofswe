package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRunStatus(t *testing.T) {
	var stdout bytes.Buffer

	err := Run(context.Background(), Config{
		Args:    []string{"status"},
		Stdout:  &stdout,
		Version: "test",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := stdout.String(); !strings.Contains(got, "enabled: true") || !strings.Contains(got, "claudecode hooks:") || !strings.Contains(got, "codex hooks:") {
		t.Fatalf("stdout = %q, want status output", got)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	err := Run(context.Background(), Config{
		Args:    []string{"wat"},
		Version: "test",
	})
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("Run() error = %v, want ErrUsage", err)
	}
}

func TestDefaultAdaptersIncludesSupportedHarnesses(t *testing.T) {
	adapters := defaultAdapters()
	if len(adapters) != 2 {
		t.Fatalf("len(defaultAdapters()) = %d, want 2", len(adapters))
	}

	found := map[string]bool{
		"claudecode": false,
		"codex":      false,
	}
	for _, adapter := range adapters {
		adapterType := fmt.Sprintf("%T", adapter)
		for name := range found {
			if adapterType == name+".Adapter" || strings.Contains(adapterType, "/"+name+".") {
				found[name] = true
			}
		}
	}
	for name, ok := range found {
		if !ok {
			t.Fatalf("defaultAdapters() = %#v, want %s adapter", adapters, name)
		}
	}
}

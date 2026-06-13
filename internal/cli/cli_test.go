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

	if got, want := stdout.String(), "proofswe scaffold ready\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
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

func TestDefaultAdaptersIncludesClaudeCode(t *testing.T) {
	adapters := defaultAdapters()
	if len(adapters) != 1 {
		t.Fatalf("len(defaultAdapters()) = %d, want 1", len(adapters))
	}

	var found bool
	for _, adapter := range adapters {
		if adapterType := fmt.Sprintf("%T", adapter); adapterType == "claudecode.Adapter" || strings.Contains(adapterType, "/claudecode.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("defaultAdapters() = %#v, want Claude Code adapter", adapters)
	}
}

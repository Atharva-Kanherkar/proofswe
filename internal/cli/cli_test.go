package cli

import (
	"bytes"
	"context"
	"errors"
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

package main

import (
	"bytes"
	"context"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer

	err := run(context.Background(), []string{"version"}, &stdout)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if stdout.String() == "" {
		t.Fatal("expected version output")
	}
}

func TestRunDropsGoRunSeparator(t *testing.T) {
	var stdout bytes.Buffer

	err := run(context.Background(), []string{"--", "status"}, &stdout)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if got, want := stdout.String(), "proofswe scaffold ready\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

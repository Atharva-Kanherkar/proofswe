package main

import (
	"bytes"
	"context"
	"strings"
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

	if got := stdout.String(); !strings.Contains(got, "enabled: true") {
		t.Fatalf("stdout = %q, want status output", got)
	}
}

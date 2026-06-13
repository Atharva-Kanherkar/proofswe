package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Atharva-Kanherkar/proofswe/internal/core/internal/schemautil"
)

const outputPath = "../../schema/normalized-event.v1.json"

func main() {
	data, err := schemautil.Bytes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate normalized event schema: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create schema directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write schema: %v\n", err)
		os.Exit(1)
	}
}

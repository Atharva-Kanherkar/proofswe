package hashing

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadSaltConcurrentFirstUseSingleSalt(t *testing.T) {
	dir := t.TempDir()
	const workers = 16
	var wg sync.WaitGroup
	results := make([][]byte, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = LoadSalt(dir)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d LoadSalt: %v", i, err)
		}
	}
	persistedHex, err := os.ReadFile(filepath.Join(dir, SaltFileName))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := hex.DecodeString(string(bytes.TrimSpace(persistedHex)))
	if err != nil {
		t.Fatal(err)
	}
	for i, got := range results {
		if !bytes.Equal(got, persisted) {
			t.Fatalf("worker %d returned salt that differs from persisted file", i)
		}
	}
}

func TestCorruptOrTruncatedSaltRegenerates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SaltFileName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("abcd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	salt, err := LoadSalt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(salt) < MinSaltBytes {
		t.Fatalf("salt len = %d, want >= %d", len(salt), MinSaltBytes)
	}
	if bytes.Equal(salt, []byte{0xab, 0xcd}) {
		t.Fatalf("truncated salt was reused")
	}

	beforeHash := New([]byte("old old old old old old old old!")).StringHash("line")
	afterHash := New(salt).StringHash("line")
	if beforeHash == afterHash {
		t.Fatalf("cross-salt confusion: old and regenerated hashes matched")
	}
}

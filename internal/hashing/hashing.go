// Package hashing is the canonical per-install salted content hasher shared by
// the capture phases. The snapshot phase (writing pending line hashes) and the
// resolve phase (re-hashing current lines to test survival) MUST hash identically,
// so both go through this package. The algorithm — HMAC-SHA256 keyed by a secret
// per-install salt that never leaves the machine — matches the adapters' hashers
// byte-for-byte (the adapters carry their own copy for now; they should migrate
// here to drop the duplication).
package hashing

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// SaltFileName is the per-install secret salt under the proofswe state dir.
	SaltFileName = "hash-salt.key"
	saltBytes    = 32
	// MinSaltBytes is the floor below which a salt file is treated as corrupt.
	MinSaltBytes = 16
)

// Hasher salts every content hash with a per-install secret so low-entropy values
// (a shell command, a short prompt, a common code line) are not reversible against
// a public corpus. The salt is the HMAC key.
type Hasher struct {
	salt []byte
}

func New(salt []byte) Hasher {
	return Hasher{salt: salt}
}

// StringHash returns the salted hash of value, or "" for the empty string.
func (h Hasher) StringHash(value string) string {
	if value == "" {
		return ""
	}
	return h.sum([]byte(value))
}

func (h Hasher) sum(data []byte) string {
	mac := hmac.New(sha256.New, h.salt)
	mac.Write(data)
	return "sha256:" + hex.EncodeToString(mac.Sum(nil))
}

// LoadSalt reads the per-install secret salt, generating and persisting a fresh
// one (0600) on first use. The file stays local and is never transmitted; callers
// fail closed if it cannot be obtained so unsalted hashes are never emitted.
func LoadSalt(stateDir string) ([]byte, error) {
	if stateDir == "" {
		return nil, fmt.Errorf("hashing: proofswe state dir unavailable")
	}
	path := filepath.Join(stateDir, SaltFileName)

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if salt, decodeErr := hex.DecodeString(strings.TrimSpace(string(data))); decodeErr == nil && len(salt) >= MinSaltBytes {
			return salt, nil
		}
		// Corrupt or too-short salt: regenerate below.
	case errors.Is(err, os.ErrNotExist):
		// First use: generate below.
	default:
		return nil, fmt.Errorf("hashing: read salt: %w", err)
	}

	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("hashing: generate salt: %w", err)
	}
	if err := writeSaltFile(stateDir, path, salt); err != nil {
		return nil, err
	}
	return salt, nil
}

func writeSaltFile(dir, path string, salt []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("hashing: create state dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".salt-*")
	if err != nil {
		return fmt.Errorf("hashing: create temp salt: %w", err)
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("hashing: chmod salt: %w", err)
	}
	if _, err := tmp.WriteString(hex.EncodeToString(salt) + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("hashing: write salt: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("hashing: sync salt: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("hashing: close salt: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("hashing: rename salt: %w", err)
	}
	removeTmp = false
	return nil
}

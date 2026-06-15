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
	"sync"
	"time"
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

var saltLocks sync.Map

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
	lock := saltLock(stateDir)
	lock.Lock()
	defer lock.Unlock()

	path := filepath.Join(stateDir, SaltFileName)

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if salt, ok := decodeSalt(data); ok {
			return salt, nil
		}
		if salt, raceErr := readRacedSalt(path); raceErr == nil {
			return salt, nil
		}
		// Corrupt or too-short salt: regenerate below.
		_ = os.Remove(path)
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
		if os.IsExist(err) || errors.Is(err, os.ErrExist) {
			return readRacedSalt(path)
		}
		return nil, err
	}
	return readPersistedSalt(path)
}

func saltLock(stateDir string) *sync.Mutex {
	clean := filepath.Clean(stateDir)
	if abs, err := filepath.Abs(clean); err == nil {
		clean = abs
	}
	lock, _ := saltLocks.LoadOrStore(clean, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func decodeSalt(data []byte) ([]byte, bool) {
	salt, err := hex.DecodeString(strings.TrimSpace(string(data)))
	return salt, err == nil && len(salt) >= MinSaltBytes
}

func readPersistedSalt(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("hashing: read persisted salt: %w", err)
	}
	if salt, ok := decodeSalt(data); ok {
		return salt, nil
	}
	return nil, fmt.Errorf("hashing: persisted invalid salt")
}

func readRacedSalt(path string) ([]byte, error) {
	var lastErr error
	for i := 0; i < 6; i++ {
		salt, err := readPersistedSalt(path)
		if err == nil {
			return salt, nil
		}
		lastErr = err
		time.Sleep(time.Duration(1<<i) * time.Millisecond)
	}
	return nil, fmt.Errorf("hashing: read raced salt: %w", lastErr)
}

func writeSaltFile(dir, path string, salt []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("hashing: create state dir: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) || errors.Is(err, os.ErrExist) {
			return err
		}
		return fmt.Errorf("hashing: create salt: %w", err)
	}
	if _, err := file.WriteString(hex.EncodeToString(salt) + "\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("hashing: write salt: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("hashing: sync salt: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("hashing: close salt: %w", err)
	}
	return nil
}

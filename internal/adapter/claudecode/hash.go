package claudecode

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
)

const (
	saltFileName = "hash-salt.key"
	saltBytes    = 32
	minSaltBytes = 16
)

// hasher salts every content hash with a per-install secret so that low-entropy
// values (a shell command, a short prompt, a common code line) cannot be reversed
// against a public corpus. The salt is the HMAC key; it never leaves the machine.
type hasher struct {
	salt []byte
}

func newHasher(salt []byte) hasher {
	return hasher{salt: salt}
}

func (h hasher) rawHash(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return h.sum(raw)
}

func (h hasher) stringHash(value string) string {
	if value == "" {
		return ""
	}
	return h.sum([]byte(value))
}

func (h hasher) sum(data []byte) string {
	mac := hmac.New(sha256.New, h.salt)
	mac.Write(data)
	return "sha256:" + hex.EncodeToString(mac.Sum(nil))
}

// loadOrCreateSalt reads the per-install secret salt, generating and persisting a
// fresh one (0600) on first use. The file stays local and is never transmitted;
// capture fails closed if it cannot be obtained so unsalted hashes are never emitted.
func loadOrCreateSalt(stateDir string) ([]byte, error) {
	if stateDir == "" {
		return nil, core.NewError(core.ErrorKindAdapter, "proofswe state dir unavailable", nil)
	}
	path := filepath.Join(stateDir, saltFileName)

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if salt, decodeErr := hex.DecodeString(strings.TrimSpace(string(data))); decodeErr == nil && len(salt) >= minSaltBytes {
			return salt, nil
		}
		// Corrupt or too-short salt: regenerate below.
	case errors.Is(err, os.ErrNotExist):
		// First use: generate below.
	default:
		return nil, core.NewError(core.ErrorKindAdapter, "read hash salt", err)
	}

	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, core.NewError(core.ErrorKindAdapter, "generate hash salt", err)
	}
	if err := writeSaltFile(stateDir, path, salt); err != nil {
		return nil, err
	}
	return salt, nil
}

func writeSaltFile(dir, path string, salt []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return core.NewError(core.ErrorKindAdapter, "create proofswe state dir", err)
	}

	tmp, err := os.CreateTemp(dir, ".salt-*")
	if err != nil {
		return core.NewError(core.ErrorKindAdapter, "create temp salt", err)
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
		return core.NewError(core.ErrorKindAdapter, "chmod salt", err)
	}
	if _, err := tmp.WriteString(hex.EncodeToString(salt) + "\n"); err != nil {
		_ = tmp.Close()
		return core.NewError(core.ErrorKindAdapter, "write salt", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return core.NewError(core.ErrorKindAdapter, "sync salt", err)
	}
	if err := tmp.Close(); err != nil {
		return core.NewError(core.ErrorKindAdapter, "close salt", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return core.NewError(core.ErrorKindAdapter, "rename salt", err)
	}
	removeTmp = false
	return nil
}

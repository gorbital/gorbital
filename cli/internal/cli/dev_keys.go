package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// encryptionKeysVar encrypts two-factor authentication secrets in Full apps
// (ADR-0043).
const encryptionKeysVar = "AUTH_ENCRYPTION_KEYS"

// ensureEncryptionKey fills an empty AUTH_ENCRYPTION_KEYS in the .env file at
// envPath with a random development key, when the app declares the variable
// in examplePath and the environment doesn't set it. It reports whether it
// wrote a key.
func ensureEncryptionKey(envPath, examplePath string) (bool, error) {
	if os.Getenv(encryptionKeysVar) != "" {
		return false, nil
	}
	example, err := os.ReadFile(examplePath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if !bytes.HasPrefix(example, []byte(encryptionKeysVar+"=")) && !bytes.Contains(example, []byte("\n"+encryptionKeysVar+"=")) {
		return false, nil // an app from before ADR-0043
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		return false, err
	}

	key := make([]byte, 32)
	_, _ = rand.Read(key) // never fails (crypto/rand)
	line := encryptionKeysVar + "=dev:" + base64.StdEncoding.EncodeToString(key)
	lines := strings.Split(string(data), "\n")
	found := false
	for i, l := range lines {
		name, value, ok := strings.Cut(strings.TrimSpace(l), "=")
		if !ok || strings.TrimSpace(strings.TrimPrefix(name, "export ")) != encryptionKeysVar {
			continue
		}
		if strings.Trim(strings.TrimSpace(value), `"'`) != "" {
			return false, nil
		}
		lines[i], found = line, true
	}
	if !found {
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1]
		}
		lines = append(lines, "", "# Encrypts two-factor authentication secrets; written by aps dev.", line, "")
	}
	if err := os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0o600); err != nil { //nolint:gosec // envPath is the app's .env
		return false, err
	}
	// The file now holds a secret: keep it private even if it was created
	// with wider permissions.
	return true, os.Chmod(envPath, 0o600)
}

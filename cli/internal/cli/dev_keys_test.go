package cli

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // TOTP (RFC 6238) uses HMAC-SHA1
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// totpCode is RFC 6238's code for a base32 secret at t, so tests can sign in
// to generated apps with two-factor authentication.
func totpCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		t.Fatalf("2FA key %q: %v", secret, err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30)) //nolint:gosec // positive time steps
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1_000_000)
}

func TestTOTPCodeHelper(t *testing.T) {
	// RFC 6238 appendix B, SHA-1, last six digits.
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	if got := totpCode(t, secret, time.Unix(1111111109, 0)); got != "081804" {
		t.Errorf("totpCode() = %q, want 081804", got)
	}
}

func TestEnsureEncryptionKey(t *testing.T) {
	const example = "DATABASE_URL=postgres://x\nAUTH_ENCRYPTION_KEYS=\n"
	t.Setenv(encryptionKeysVar, "")
	t.Chdir(t.TempDir())
	keyOf := func() string {
		t.Helper()
		for _, l := range strings.Split(readFile(t, ".env"), "\n") {
			if v, ok := strings.CutPrefix(l, encryptionKeysVar+"="); ok {
				return v
			}
		}
		return ""
	}

	writeFile(t, ".env.example", example)
	writeFile(t, ".env", example)
	wrote, err := ensureEncryptionKey(".env", ".env.example")
	key := keyOf()
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(key, "dev:"))
	if err != nil || !wrote || !strings.HasPrefix(key, "dev:") || len(raw) != 32 || !strings.Contains(readFile(t, ".env"), "DATABASE_URL=postgres://x\n") {
		t.Fatalf("ensureEncryptionKey() = %v, %v; .env:\n%s", wrote, err, readFile(t, ".env"))
	}
	if info, _ := os.Stat(".env"); info.Mode().Perm() != 0o600 {
		t.Errorf(".env mode = %v, want 0600", info.Mode().Perm())
	}
	if wrote, err := ensureEncryptionKey(".env", ".env.example"); err != nil || wrote || keyOf() != key {
		t.Errorf("ensureEncryptionKey() with a key = %v, %v; want the key kept", wrote, err)
	}

	// A .env from before the variable existed gets it appended.
	writeFile(t, ".env", "APP_ADDR=127.0.0.1:8080\n")
	if wrote, err := ensureEncryptionKey(".env", ".env.example"); err != nil || !wrote || !strings.HasPrefix(keyOf(), "dev:") || !strings.HasPrefix(readFile(t, ".env"), "APP_ADDR=127.0.0.1:8080\n") {
		t.Errorf("ensureEncryptionKey() appending = %v, %v; .env:\n%s", wrote, err, readFile(t, ".env"))
	}

	// Apps without the variable, and a key set in the environment, are left alone.
	writeFile(t, ".env.example", "DATABASE_URL=postgres://x\n")
	writeFile(t, ".env", "DATABASE_URL=postgres://x\n")
	if wrote, err := ensureEncryptionKey(".env", ".env.example"); err != nil || wrote || keyOf() != "" {
		t.Errorf("ensureEncryptionKey() for an app without 2FA = %v, %v", wrote, err)
	}
	writeFile(t, ".env.example", example)
	writeFile(t, ".env", example)
	t.Setenv(encryptionKeysVar, "k1:from-the-environment")
	if wrote, err := ensureEncryptionKey(".env", ".env.example"); err != nil || wrote || keyOf() != "" {
		t.Errorf("ensureEncryptionKey() with the variable in the environment = %v, %v", wrote, err)
	}
}

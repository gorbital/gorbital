package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// A PasswordChecker rejects a new password by returning an error whose
// message explains why, such as "appears in a list of breached passwords".
// It must return nil when it can't decide, for example when a remote list
// is unreachable.
type PasswordChecker func(ctx context.Context, password string) error

// ValidatePassword applies the password policy to a new password: 12 to 128
// characters, not blank, and checker when not nil. It returns a
// [*PasswordError].
func ValidatePassword(ctx context.Context, password string, checker PasswordChecker) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLength:
		return &PasswordError{Reason: fmt.Sprintf("must be at least %d characters", MinPasswordLength)}
	case n > MaxPasswordLength || len(password) > 4*MaxPasswordLength:
		return &PasswordError{Reason: fmt.Sprintf("must be at most %d characters", MaxPasswordLength)}
	case strings.TrimSpace(password) == "":
		return &PasswordError{Reason: "must not be blank"}
	}
	if checker != nil {
		if err := checker(ctx, password); err != nil {
			return &PasswordError{Reason: err.Error()}
		}
	}
	return nil
}

// argonParams are argon2id parameters, stored in each hash.
type argonParams struct {
	memory      uint32 // KiB
	iterations  uint32
	parallelism uint8
	saltLen     uint32
	keyLen      uint32
}

// defaultParams follow the OWASP recommendation for argon2id.
var defaultParams = argonParams{memory: 19 * 1024, iterations: 2, parallelism: 1, saltLen: 16, keyLen: 32}

// Hasher hashes and verifies passwords with argon2id, bounding how many
// hashes run at once so a burst of logins can't exhaust memory. It is safe
// for concurrent use.
type Hasher struct {
	params argonParams
	slots  chan struct{}
	dummy  string
}

// NewHasher returns a hasher with the current parameters.
func NewHasher() (*Hasher, error) {
	return newHasher(defaultParams)
}

func newHasher(p argonParams) (*Hasher, error) {
	h := &Hasher{params: p, slots: make(chan struct{}, max(4, runtime.NumCPU()))}
	dummy, err := h.Hash(rand.Text())
	if err != nil {
		return nil, err
	}
	h.dummy = dummy
	return h, nil
}

// Hash returns an encoded hash: $argon2id$v=19$m=…,t=…,p=…$salt$key.
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	h.slots <- struct{}{}
	key := argon2.IDKey([]byte(password), salt, h.params.iterations, h.params.memory, h.params.parallelism, h.params.keyLen)
	<-h.slots
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		h.params.memory, h.params.iterations, h.params.parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify reports whether password matches encoded, and whether the hash
// should be replaced (Hash again and store it) because its parameters are
// outdated.
func (h *Hasher) Verify(password, encoded string) (ok, rehash bool) {
	p, salt, key, err := decodeHash(encoded)
	if err != nil {
		return false, false
	}
	h.slots <- struct{}{}
	got := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLen)
	<-h.slots
	if subtle.ConstantTimeCompare(got, key) != 1 {
		return false, false
	}
	return true, p != h.params
}

// VerifyDummy does the work of a failed Verify. Call it when an account
// doesn't exist, so the response takes as long as for a wrong password.
func (h *Hasher) VerifyDummy(password string) {
	h.Verify(password, h.dummy)
}

var errBadHash = errors.New("auth: unrecognised password hash")

func decodeHash(encoded string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return argonParams{}, nil, nil, errBadHash
	}
	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil {
		return argonParams{}, nil, nil, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, errBadHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 || p.memory == 0 || p.iterations == 0 || p.parallelism == 0 {
		return argonParams{}, nil, nil, errBadHash
	}
	p.saltLen, p.keyLen = uint32(len(salt)), uint32(len(key)) //nolint:gosec // lengths are small
	return p, salt, key, nil
}

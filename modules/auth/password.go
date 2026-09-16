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
	"time"
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

// ErrHasherBusy reports that no password hashing slot became free before
// the request's context ended or the hasher's maximum wait passed. Answer
// 503: the server is overloaded, not the request wrong.
var ErrHasherBusy = errors.New("auth: password hashing is busy")

// DefaultHashMaxWait is how long [Hasher] waits for a free slot by default.
const DefaultHashMaxWait = 5 * time.Second

// Hasher hashes and verifies passwords with argon2id, bounding how many
// hashes run at once so a burst of sign-ins can't exhaust memory, and how
// long a request waits for a turn, so a flood can't hold requests without
// limit. It is safe for concurrent use.
type Hasher struct {
	params  argonParams
	slots   chan struct{}
	maxWait time.Duration
	dummy   string
}

// A HasherOption configures a [Hasher].
type HasherOption interface{ apply(*Hasher) }

type hashConcurrencyOption int

func (o hashConcurrencyOption) apply(h *Hasher) {
	if o > 0 {
		h.slots = make(chan struct{}, int(o))
	}
}

// WithHashConcurrency sets how many hashes run at once; each uses 19 MiB.
// Default: GOMAXPROCS, and at least 4.
func WithHashConcurrency(n int) HasherOption { return hashConcurrencyOption(n) }

type hashMaxWaitOption time.Duration

func (o hashMaxWaitOption) apply(h *Hasher) {
	if o > 0 {
		h.maxWait = time.Duration(o)
	}
}

// WithHashMaxWait sets how long hashing waits for a free slot before
// returning [ErrHasherBusy]. Default: [DefaultHashMaxWait].
func WithHashMaxWait(d time.Duration) HasherOption { return hashMaxWaitOption(d) }

// NewHasher returns a hasher with the current parameters and default
// options.
func NewHasher() (*Hasher, error) {
	return newHasher(defaultParams)
}

// NewHasherWith returns a hasher with the current parameters and opts.
func NewHasherWith(opts ...HasherOption) (*Hasher, error) {
	return newHasher(defaultParams, opts...)
}

func newHasher(p argonParams, opts ...HasherOption) (*Hasher, error) {
	h := &Hasher{params: p, slots: make(chan struct{}, max(4, runtime.GOMAXPROCS(0))), maxWait: DefaultHashMaxWait}
	for _, o := range opts {
		o.apply(h)
	}
	dummy, err := h.Hash(rand.Text())
	if err != nil {
		return nil, err
	}
	h.dummy = dummy
	return h, nil
}

// Hash returns an encoded hash: $argon2id$v=19$m=…,t=…,p=…$salt$key. It
// returns [ErrHasherBusy] when no slot frees up within the maximum wait;
// prefer [Hasher.HashContext] in requests.
func (h *Hasher) Hash(password string) (string, error) {
	return h.HashContext(context.Background(), password)
}

// HashContext is [Hasher.Hash], waiting for a free slot only until ctx ends
// ([ErrHasherBusy]).
func (h *Hasher) HashContext(ctx context.Context, password string) (string, error) {
	salt := make([]byte, h.params.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, h.params.iterations, h.params.memory, h.params.parallelism, h.params.keyLen)
	<-h.slots
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		h.params.memory, h.params.iterations, h.params.parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify reports whether password matches encoded, and whether the hash
// should be replaced (Hash again and store it) because its parameters are
// outdated. A busy hasher reports no match; prefer [Hasher.VerifyContext]
// in requests, which tells the two apart.
func (h *Hasher) Verify(password, encoded string) (ok, rehash bool) {
	ok, rehash, _ = h.VerifyContext(context.Background(), password, encoded)
	return ok, rehash
}

// VerifyContext is [Hasher.Verify], waiting for a free slot only until ctx
// ends. It returns [ErrHasherBusy] when it couldn't check the password.
func (h *Hasher) VerifyContext(ctx context.Context, password, encoded string) (ok, rehash bool, err error) {
	p, salt, key, err := decodeHash(encoded)
	if err != nil {
		return false, false, nil
	}
	if err := h.acquire(ctx); err != nil {
		return false, false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLen)
	<-h.slots
	if subtle.ConstantTimeCompare(got, key) != 1 {
		return false, false, nil
	}
	return true, p != h.params, nil
}

// VerifyDummy does the work of a failed Verify. Call it when an account
// doesn't exist, so the response takes as long as for a wrong password.
func (h *Hasher) VerifyDummy(password string) {
	h.Verify(password, h.dummy)
}

// VerifyDummyContext is [Hasher.VerifyDummy] with the waiting of
// [Hasher.VerifyContext]; it returns only [ErrHasherBusy].
func (h *Hasher) VerifyDummyContext(ctx context.Context, password string) error {
	_, _, err := h.VerifyContext(ctx, password, h.dummy)
	return err
}

// acquire takes a hashing slot, waiting until ctx ends or the maximum wait
// passes.
func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.slots <- struct{}{}:
		return nil
	default:
	}
	timer := time.NewTimer(h.maxWait)
	defer timer.Stop()
	select {
	case h.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ErrHasherBusy, context.Cause(ctx))
	case <-timer.C:
		return ErrHasherBusy
	}
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

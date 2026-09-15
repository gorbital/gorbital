package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // HMAC-SHA1 is what RFC 6238 and authenticator apps use; it isn't used for collision resistance
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters every authenticator app supports (RFC 6238).
const (
	TOTPDigits = 6
	TOTPPeriod = 30 * time.Second
	// TOTPSkew is how many steps before and after the current one are
	// accepted, for clocks that drift.
	TOTPSkew = 1
)

// ErrInvalidTOTPSecret reports a secret that isn't base32.
var ErrInvalidTOTPSecret = errors.New("auth: invalid TOTP secret")

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a new TOTP secret: 20 random bytes (160 bits, as RFC
// 4226 recommends) in unpadded base32, the form authenticator apps accept
// when typed or scanned. Store it only encrypted ([Keyring]).
func NewTOTPSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return totpEncoding.EncodeToString(b)
}

// TOTPURI returns the otpauth:// URI authenticator apps read from a QR code:
// issuer is the app's name and account the user's email address.
func TOTPURI(issuer, account, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprint(TOTPDigits))
	v.Set("period", fmt.Sprint(int(TOTPPeriod/time.Second)))
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// TOTPStep returns the time step containing t.
func TOTPStep(t time.Time) int64 {
	return t.Unix() / int64(TOTPPeriod/time.Second)
}

// TOTPCode returns the code for secret at t.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	return hotp(key, TOTPStep(t)), nil
}

// VerifyTOTP reports whether code is valid for secret at now, within
// [TOTPSkew] steps, and returns the matching step. Store the step and accept
// a later code only for a later step, so a code can't be used twice.
// Spaces in code are ignored.
func VerifyTOTP(secret, code string, now time.Time) (step int64, ok bool) {
	key, err := decodeTOTPSecret(secret)
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if err != nil || len(code) != TOTPDigits {
		return 0, false
	}
	current := TOTPStep(now)
	for s := current - TOTPSkew; s <= current+TOTPSkew; s++ {
		if subtle.ConstantTimeCompare([]byte(hotp(key, s)), []byte(code)) == 1 {
			step, ok = s, true
		}
	}
	return step, ok
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	key, err := totpEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil || len(key) < 10 {
		return nil, ErrInvalidTOTPSecret
	}
	return key, nil
}

// hotp is RFC 4226's HOTP with SHA-1 and TOTPDigits digits.
func hotp(key []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter)) //nolint:gosec // time steps are positive
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", TOTPDigits, value%1_000_000)
}

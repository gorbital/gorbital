// Package domain holds phone-code sign-in's rules. It imports only the
// standard library.
package domain

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Purposes of a code.
const (
	PurposeConfirm = "confirm"
	PurposeSignIn  = "sign_in"
)

// Limits of codes.
const (
	// CodeTTL is how long a code works.
	CodeTTL = 10 * time.Minute
	// CodeAttempts is how many guesses one code allows.
	CodeAttempts = 5
	// ResendInterval is the shortest time between two codes to one number.
	ResendInterval = time.Minute
	// CodesPerHour is how many codes of one purpose a number receives in an
	// hour: with CodeAttempts, at most 25 guesses an hour, from any number
	// of clients.
	CodesPerHour = 5
)

// Method names phone-code sign-in in sign-in's audit events and hooks.
const Method = "phone_code"

// Errors of the use cases; module.go maps them to problem codes.
var (
	ErrUnauthenticated = errors.New("phonelogin: a signed-in reader is required")
	ErrInvalidPhone    = errors.New("phonelogin: a phone number is in E.164 form, such as +447700900123")
	// ErrInvalidCode reports a wrong, used, expired or exhausted code, or a
	// number without one: the cases aren't told apart.
	ErrInvalidCode = errors.New("phonelogin: the code is wrong or expired")
	// ErrPhoneTaken reports a number another account confirmed.
	ErrPhoneTaken = errors.New("phonelogin: another account uses this number")
	// ErrSMSUnavailable reports a text message that couldn't be sent.
	ErrSMSUnavailable = errors.New("phonelogin: text messages can't be sent")
)

var e164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// NormalizePhone removes spaces, dashes and brackets, and returns
// ErrInvalidPhone unless the rest is an E.164 number.
func NormalizePhone(s string) (string, error) {
	s = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(s)
	if !e164.MatchString(s) {
		return "", ErrInvalidPhone
	}
	return s, nil
}

// Code is a single-use code sent to a phone.
type Code struct {
	ID        string
	UserID    string
	Phone     string
	Purpose   string
	Hash      []byte
	Attempts  int
	ExpiresAt time.Time
	CreatedAt time.Time
}

// HashCode returns what is stored of a code.
func HashCode(code string) []byte {
	sum := sha256.Sum256([]byte(code))
	return sum[:]
}

// Matches reports, in constant time, whether code is c's.
func (c Code) Matches(code string) bool {
	return subtle.ConstantTimeCompare(HashCode(code), c.Hash) == 1
}

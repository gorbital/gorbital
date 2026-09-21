// Package domain holds Shelfie's reader profiles and their rules. It
// imports only the standard library.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Profile is how a reader appears to others.
type Profile struct {
	UserID      string
	DisplayName string
	// Country is an ISO 3166-1 alpha-2 code, or empty.
	Country string
	// Suspended reports a moderator's suspension: the reader can't sign in.
	Suspended bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Fields are what a reader sets.
type Fields struct {
	DisplayName string
	Country     string
}

// Errors of the profile use cases; module.go maps them to problem codes.
var (
	ErrUnauthenticated = errors.New("profiles: a signed-in reader is required")
	// ErrProfileIncomplete reports a reader without a profile yet, such as
	// one who signed up with Google.
	ErrProfileIncomplete  = errors.New("profiles: the profile isn't complete")
	ErrInvalidDisplayName = errors.New("profiles: a display name is 1 to 50 characters")
	ErrInvalidCountry     = errors.New("profiles: a country is an ISO 3166-1 alpha-2 code")
)

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

// Clean returns f with the display name trimmed, or ErrInvalidDisplayName or
// ErrInvalidCountry.
func (f Fields) Clean() (Fields, error) {
	f.DisplayName = strings.TrimSpace(f.DisplayName)
	switch {
	case f.DisplayName == "" || utf8.RuneCountInString(f.DisplayName) > 50:
		return Fields{}, ErrInvalidDisplayName
	case f.Country != "" && !countryCode.MatchString(f.Country):
		return Fields{}, ErrInvalidCountry
	}
	return f, nil
}

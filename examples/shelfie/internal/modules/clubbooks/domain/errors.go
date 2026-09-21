package domain

import (
	"errors"
	"strings"
)

// Errors of the club books use cases. module.go maps each to an HTTP status
// and a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a member acting in
	// the organisation: a route without guard.OrgMember. An organisation the
	// caller isn't a member of never reaches the use cases; the guard
	// answers org_not_found.
	ErrUnauthenticated = errors.New("clubbooks: a member of the organisation is required")

	// ErrInvalidClubBook reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidClubBook = errors.New("clubbooks: invalid club book")

	// ErrClubBookNotFound reports a club book that doesn't exist in the
	// organisation, such as another organisation's. The two aren't told
	// apart, so IDs can't be probed.
	ErrClubBookNotFound = errors.New("clubbooks: club book not found")

	// ErrClubBookTitleTaken reports a title the organisation already uses,
	// ignoring case.
	ErrClubBookTitleTaken = errors.New("clubbooks: club book title is already taken")

	// ErrClubBookVersionConflict reports an update to a version that is no
	// longer current.
	ErrClubBookVersionConflict = errors.New("clubbooks: club book was changed since it was read")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of a club book.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "clubbooks: invalid club book: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidClubBook.
func (e *ValidationError) Unwrap() error { return ErrInvalidClubBook }

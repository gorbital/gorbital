package domain

import (
	"errors"
	"strings"
)

// Errors of the shelves use cases. module.go maps each to an HTTP status
// and a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a signed-in user: a
	// service account's API key owns no shelves.
	ErrUnauthenticated = errors.New("shelves: a signed-in user is required")

	// ErrInvalidShelf reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidShelf = errors.New("shelves: invalid shelf")

	// ErrShelfNotFound reports a shelf that doesn't exist or belongs to
	// someone else. The two aren't told apart, so IDs can't be probed.
	ErrShelfNotFound = errors.New("shelves: shelf not found")

	// ErrShelfNameTaken reports a name the owner already uses,
	// ignoring case.
	ErrShelfNameTaken = errors.New("shelves: shelf name is already taken")

	// ErrShelfVersionConflict reports an update to a version that is no
	// longer current.
	ErrShelfVersionConflict = errors.New("shelves: shelf was changed since it was read")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of a shelf.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "shelves: invalid shelf: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidShelf.
func (e *ValidationError) Unwrap() error { return ErrInvalidShelf }

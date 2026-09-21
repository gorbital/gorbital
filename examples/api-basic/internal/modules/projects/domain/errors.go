package domain

import (
	"errors"
	"strings"
)

// Errors of the projects use cases. module.go maps each to an HTTP status
// and a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a signed-in user: a
	// service account's API key owns no projects.
	ErrUnauthenticated = errors.New("projects: a signed-in user is required")

	// ErrInvalidProject reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidProject = errors.New("projects: invalid project")

	// ErrProjectNotFound reports a project that doesn't exist or belongs to
	// someone else. The two aren't told apart, so IDs can't be probed.
	ErrProjectNotFound = errors.New("projects: project not found")

	// ErrProjectNameTaken reports a name the owner already uses,
	// ignoring case.
	ErrProjectNameTaken = errors.New("projects: project name is already taken")

	// ErrProjectVersionConflict reports an update to a version that is no
	// longer current.
	ErrProjectVersionConflict = errors.New("projects: project was changed since it was read")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of a project.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "projects: invalid project: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidProject.
func (e *ValidationError) Unwrap() error { return ErrInvalidProject }

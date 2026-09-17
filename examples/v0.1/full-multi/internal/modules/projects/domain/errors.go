package domain

import (
	"errors"
	"strings"
)

// Errors returned by the projects use cases. Their HTTP codes are mapped
// in internal/app/module_projects.go. An organisation the caller doesn't
// belong to is orgs.ErrOrgNotFound, and a role without the permission is
// actor.ErrForbidden.
var (
	// ErrInvalidProject reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidProject = errors.New("invalid project")

	// ErrProjectNotFound reports a project that doesn't exist in the
	// organisation. Projects of other organisations aren't distinguished, so
	// IDs can't be probed.
	ErrProjectNotFound = errors.New("project not found")

	// ErrProjectNameTaken reports a name the organisation already uses,
	// ignoring case.
	ErrProjectNameTaken = errors.New("project name is already taken")

	// ErrProjectVersionConflict reports an update to a version that is no
	// longer current.
	ErrProjectVersionConflict = errors.New("project was changed since it was read")
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
	return "invalid project: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidProject.
func (e *ValidationError) Unwrap() error { return ErrInvalidProject }

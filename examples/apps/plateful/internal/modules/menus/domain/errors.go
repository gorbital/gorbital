package domain

import (
	"errors"
	"strings"
)

// Errors of the menus use cases. module.go maps each to an HTTP status and a
// problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without an acting account: a
	// route without a guard, or a staff route reached without a member of
	// the organisation in the path. An organisation the caller isn't a
	// member of never reaches the use cases; guard.OrgMember answers
	// org_not_found.
	ErrUnauthenticated = errors.New("menus: an authenticated caller is required")

	// ErrInvalidItem reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidItem = errors.New("menus: invalid menu item")

	// ErrItemNotFound reports an item that doesn't exist, or that the caller
	// may not see.
	ErrItemNotFound = errors.New("menus: menu item not found")

	// ErrItemNameTaken reports a name another item of the same organisation
	// already uses, ignoring case.
	ErrItemNameTaken = errors.New("menus: menu item name is already taken")

	// ErrItemVersionConflict reports an update to a version that is no
	// longer current.
	ErrItemVersionConflict = errors.New("menus: menu item was changed since it was read")

	// ErrRestaurantNotFound reports a restaurant a customer asked for the
	// menu of that isn't open: it doesn't exist, it never published, it is
	// paused, or platform staff suspended it. The cases aren't told apart,
	// so a suspension can't be detected by probing IDs.
	ErrRestaurantNotFound = errors.New("menus: restaurant not found")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of a menu item.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "menus: invalid menu item: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidItem.
func (e *ValidationError) Unwrap() error { return ErrInvalidItem }

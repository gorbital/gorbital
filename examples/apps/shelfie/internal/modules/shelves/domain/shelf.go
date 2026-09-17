// Package domain holds the shelves module's shelves and their rules. It
// imports only the standard library.
package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Field limits. The migration's CHECK constraints match them.
const (
	MaxNameLength        = 100
	MaxDescriptionLength = 2000
)

// Visibility is one of the values a shelf's visibility can have.
type Visibility string

// Visibility values.
const (
	VisibilityPrivate Visibility = "private"
	VisibilityShared  Visibility = "shared"
)

// Valid reports whether v is a known visibility.
func (v Visibility) Valid() bool {
	switch v {
	case VisibilityPrivate, VisibilityShared:
		return true
	}
	return false
}

// Shelf belongs to one user, its owner, and only the owner can see or
// change it.
type Shelf struct {
	ID      string
	OwnerID string
	ShelfFields
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ShelfFields are the fields users set.
type ShelfFields struct {
	Name        string
	Description string
	Visibility  Visibility
}

// NewShelf returns a new shelf owned by ownerID, or a *ValidationError.
// Text is trimmed, and an empty choice takes its first value.
func NewShelf(id, ownerID string, f ShelfFields, now time.Time) (Shelf, error) {
	f.Name = strings.TrimSpace(f.Name)
	f.Description = strings.TrimSpace(f.Description)
	if f.Visibility == "" {
		f.Visibility = VisibilityPrivate
	}
	shelf := Shelf{ID: id, OwnerID: ownerID, ShelfFields: f, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := shelf.validate(); err != nil {
		return Shelf{}, err
	}
	return shelf, nil
}

// Changes are the fields an update sets. Nil fields keep their value.
type Changes struct {
	Name        *string
	Description *string
	Visibility  *Visibility
}

// Apply returns shelf with c applied and the names of the fields whose value
// changed, or a *ValidationError. UpdatedAt becomes now only when a field
// changed. The repository increments the version when it saves.
func (shelf Shelf) Apply(c Changes, now time.Time) (Shelf, []string, error) {
	next := shelf
	var changed []string
	if c.Name != nil {
		if value := strings.TrimSpace(*c.Name); value != shelf.Name {
			next.Name, changed = value, append(changed, "name")
		}
	}
	if c.Description != nil {
		if value := strings.TrimSpace(*c.Description); value != shelf.Description {
			next.Description, changed = value, append(changed, "description")
		}
	}
	if c.Visibility != nil && *c.Visibility != shelf.Visibility {
		next.Visibility, changed = *c.Visibility, append(changed, "visibility")
	}
	if err := next.validate(); err != nil {
		return shelf, nil, err
	}
	if len(changed) > 0 {
		next.UpdatedAt = now
	}
	return next, changed, nil
}

func (shelf Shelf) validate() error {
	var errs []FieldError
	if msg := checkText(shelf.Name, 1, MaxNameLength); msg != "" {
		errs = append(errs, FieldError{Field: "name", Message: msg})
	}
	if msg := checkText(shelf.Description, 0, MaxDescriptionLength); msg != "" {
		errs = append(errs, FieldError{Field: "description", Message: msg})
	}
	if !shelf.Visibility.Valid() {
		errs = append(errs, FieldError{Field: "visibility", Message: "must be private or shared"})
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// checkText returns why s isn't a valid text of min to max characters, or "".
func checkText(s string, minLen, maxLen int) string {
	switch n := utf8.RuneCountInString(s); {
	case !utf8.ValidString(s) || strings.ContainsRune(s, 0):
		return "must be valid text"
	case n < minLen:
		return "is required"
	case n > maxLen:
		return fmt.Sprintf("must be at most %d characters", maxLen)
	}
	return ""
}

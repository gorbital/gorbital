// Package domain holds the projects module's entities and rules.
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

// Status is one of the values a project's status can have.
type Status string

// Status values.
const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

// Valid reports whether v is a known status.
func (v Status) Valid() bool {
	switch v {
	case StatusActive, StatusArchived:
		return true
	}
	return false
}

// Project belongs to one user, its owner, and only the owner can see or
// change it.
type Project struct {
	ID      string
	OwnerID string
	ProjectFields
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProjectFields are the fields users set.
type ProjectFields struct {
	Name        string
	Description string
	Status      Status
}

// NewProject returns a new project owned by ownerID, or a *ValidationError.
// Text is trimmed, and an empty choice takes its first value.
func NewProject(id, ownerID string, f ProjectFields, now time.Time) (Project, error) {
	f.Name = strings.TrimSpace(f.Name)
	f.Description = strings.TrimSpace(f.Description)
	if f.Status == "" {
		f.Status = StatusActive
	}
	p := Project{ID: id, OwnerID: ownerID, ProjectFields: f, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := p.validate(); err != nil {
		return Project{}, err
	}
	return p, nil
}

// Changes are the fields an update sets. Nil fields keep their value.
type Changes struct {
	Name        *string
	Description *string
	Status      *Status
}

// Apply returns p with c applied and the names of the fields whose value
// changed, or a *ValidationError. UpdatedAt becomes now only when a field
// changed. The repository increments the version when it saves.
func (p Project) Apply(c Changes, now time.Time) (Project, []string, error) {
	next := p
	var changed []string
	if c.Name != nil {
		if value := strings.TrimSpace(*c.Name); value != p.Name {
			next.Name, changed = value, append(changed, "name")
		}
	}
	if c.Description != nil {
		if value := strings.TrimSpace(*c.Description); value != p.Description {
			next.Description, changed = value, append(changed, "description")
		}
	}
	if c.Status != nil && *c.Status != p.Status {
		next.Status, changed = *c.Status, append(changed, "status")
	}
	if err := next.validate(); err != nil {
		return p, nil, err
	}
	if len(changed) > 0 {
		next.UpdatedAt = now
	}
	return next, changed, nil
}

func (p Project) validate() error {
	var errs []FieldError
	if msg := checkText(p.Name, 1, MaxNameLength); msg != "" {
		errs = append(errs, FieldError{Field: "name", Message: msg})
	}
	if msg := checkText(p.Description, 0, MaxDescriptionLength); msg != "" {
		errs = append(errs, FieldError{Field: "description", Message: msg})
	}
	if !p.Status.Valid() {
		errs = append(errs, FieldError{Field: "status", Message: "must be active or archived"})
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

// Package domain holds the projects module's entities and rules. The
// projects module is the example business resource and the template for
// modules created with aps gen resource (ADR-0039).
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

// Status is where a project is in its life.
type Status string

// Statuses.
const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool { return s == StatusActive || s == StatusArchived }

// Project is something a user works on. It belongs to one user, its owner,
// and only the owner can see or change it.
type Project struct {
	ID          string
	OwnerID     string
	Name        string
	Description string
	Status      Status
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewProject returns a new project owned by ownerID, or a *ValidationError.
// Name and description are trimmed; an empty status means active.
func NewProject(id, ownerID, name, description string, status Status, now time.Time) (Project, error) {
	if status == "" {
		status = StatusActive
	}
	p := Project{
		ID: id, OwnerID: ownerID,
		Name: strings.TrimSpace(name), Description: strings.TrimSpace(description), Status: status,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
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
		if v := strings.TrimSpace(*c.Name); v != p.Name {
			next.Name, changed = v, append(changed, "name")
		}
	}
	if c.Description != nil {
		if v := strings.TrimSpace(*c.Description); v != p.Description {
			next.Description, changed = v, append(changed, "description")
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

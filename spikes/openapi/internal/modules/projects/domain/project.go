// Package domain holds the projects business model. It imports only the
// standard library and knows nothing about HTTP, JSON or databases.
package domain

import (
	"errors"
	"strings"
	"time"
)

// Errors returned by the projects domain.
var (
	ErrNameRequired    = errors.New("project name is required")
	ErrNameTooLong     = errors.New("project name is too long")
	ErrNotFound        = errors.New("project not found")
	ErrNameTaken       = errors.New("project name is already taken")
	ErrAlreadyArchived = errors.New("project is already archived")
)

// MaxNameLength is the longest allowed project name.
const MaxNameLength = 100

// A Project is a unit of work owned by an organisation.
type Project struct {
	ID        string
	OrgID     string
	Name      string
	Archived  bool
	CreatedAt time.Time
}

// NewProject validates the name and returns a new, active project.
func NewProject(id, orgID, name string, now time.Time) (Project, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return Project{}, ErrNameRequired
	case len(name) > MaxNameLength:
		return Project{}, ErrNameTooLong
	}
	return Project{ID: id, OrgID: orgID, Name: name, CreatedAt: now}, nil
}

// Archive marks the project archived. Archiving twice is an error.
func (p *Project) Archive() error {
	if p.Archived {
		return ErrAlreadyArchived
	}
	p.Archived = true
	return nil
}

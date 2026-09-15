// Package usecase holds the projects application logic.
package usecase

import (
	"context"
	"fmt"
	"time"

	projectdomain "gorbital.dev/spikes/openapi/internal/modules/projects/domain"
)

// Service runs the projects use cases.
type Service struct {
	repo  ProjectRepository
	newID func() string
	now   func() time.Time
}

// NewService returns a Service using repo for storage.
func NewService(repo ProjectRepository, newID func() string, now func() time.Time) *Service {
	return &Service{repo: repo, newID: newID, now: now}
}

// Create adds a project to the organisation.
func (s *Service) Create(ctx context.Context, orgID, name string) (projectdomain.Project, error) {
	p, err := projectdomain.NewProject(s.newID(), orgID, name, s.now())
	if err != nil {
		return projectdomain.Project{}, err
	}
	if err := s.repo.Insert(ctx, p); err != nil {
		return projectdomain.Project{}, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

// Get returns one project of the organisation.
func (s *Service) Get(ctx context.Context, orgID, id string) (projectdomain.Project, error) {
	return s.repo.Get(ctx, orgID, id)
}

// List returns the organisation's projects, oldest first.
func (s *Service) List(ctx context.Context, orgID string) ([]projectdomain.Project, error) {
	return s.repo.List(ctx, orgID)
}

// Archive archives a project of the organisation.
func (s *Service) Archive(ctx context.Context, orgID, id string) (projectdomain.Project, error) {
	p, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return projectdomain.Project{}, err
	}
	if err := p.Archive(); err != nil {
		return projectdomain.Project{}, err
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return projectdomain.Project{}, fmt.Errorf("archive project: %w", err)
	}
	return p, nil
}

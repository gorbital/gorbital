package usecase

import (
	"context"

	"example.com/plateful/internal/modules/projects/domain"
)

// CreateProject adds a project to the organisation orgID, created by the member
// acting in it.
func (s *Service) CreateProject(ctx context.Context, orgID string, f domain.ProjectFields) (domain.Project, error) {
	member, err := memberID(ctx, orgID)
	if err != nil {
		return domain.Project{}, err
	}
	project, err := domain.NewProject(s.newID(), orgID, member, f, s.clock())
	if err != nil {
		return domain.Project{}, err
	}
	created, err := s.store.InsertProject(ctx, project)
	if err != nil {
		return domain.Project{}, storeError("create", err)
	}
	s.audit(ctx, ActionCreated, created.ID, nil)
	return created, nil
}

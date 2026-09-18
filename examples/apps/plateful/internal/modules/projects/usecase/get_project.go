package usecase

import (
	"context"

	"example.com/plateful/internal/modules/projects/domain"
)

// GetProject returns one of the organisation orgID's projects.
func (s *Service) GetProject(ctx context.Context, orgID, id string) (domain.Project, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Project{}, err
	}
	project, err := s.store.SelectProject(ctx, orgID, id, false)
	if err != nil {
		return domain.Project{}, storeError("get", err)
	}
	return project, nil
}

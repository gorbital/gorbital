package usecase

import (
	"context"

	"example.com/acme-api/internal/modules/projects/domain"
)

// GetProject returns one of the signed-in user's projects.
func (s *Service) GetProject(ctx context.Context, id string) (domain.Project, error) {
	owner, err := ownerID(ctx)
	if err != nil {
		return domain.Project{}, err
	}
	project, err := s.store.SelectProject(ctx, owner, id, false)
	if err != nil {
		return domain.Project{}, storeError("get", err)
	}
	return project, nil
}

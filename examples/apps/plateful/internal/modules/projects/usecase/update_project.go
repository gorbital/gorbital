package usecase

import (
	"context"

	"example.com/plateful/internal/modules/projects/domain"
)

// UpdateProjectInput changes a project. Version is the version the caller read.
type UpdateProjectInput struct {
	Version int64
	Changes domain.Changes
}

// UpdateProject changes one of the organisation orgID's projects. It returns
// ErrProjectVersionConflict when in.Version is no longer current. An update
// that changes nothing returns the project as it is.
func (s *Service) UpdateProject(ctx context.Context, orgID, id string, in UpdateProjectInput) (domain.Project, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Project{}, err
	}
	var updated domain.Project
	var changed []string
	err := s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectProject(ctx, orgID, id, true)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrProjectVersionConflict
		}
		next, fields, err := current.Apply(in.Changes, s.clock())
		if err != nil {
			return err
		}
		if len(fields) == 0 {
			updated = current
			return nil
		}
		changed = fields
		updated, err = tx.UpdateProject(ctx, next)
		return err
	})
	if err != nil {
		return domain.Project{}, storeError("update", err)
	}
	if len(changed) > 0 {
		// Field names only: values may be personal data.
		s.audit(ctx, ActionUpdated, id, map[string]any{"fields": changed})
	}
	return updated, nil
}

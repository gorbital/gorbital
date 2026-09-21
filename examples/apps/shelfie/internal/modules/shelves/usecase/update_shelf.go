package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/shelves/domain"
)

// UpdateShelfInput changes a shelf. Version is the version the caller read.
type UpdateShelfInput struct {
	Version int64
	Changes domain.Changes
}

// UpdateShelf changes one of the signed-in user's shelves. It returns
// ErrShelfVersionConflict when in.Version is no longer current. An update
// that changes nothing returns the shelf as it is.
func (s *Service) UpdateShelf(ctx context.Context, id string, in UpdateShelfInput) (domain.Shelf, error) {
	owner, err := ownerID(ctx)
	if err != nil {
		return domain.Shelf{}, err
	}
	var updated domain.Shelf
	var changed []string
	err = s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectShelf(ctx, owner, id, true)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrShelfVersionConflict
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
		updated, err = tx.UpdateShelf(ctx, next)
		return err
	})
	if err != nil {
		return domain.Shelf{}, storeError("update", err)
	}
	if len(changed) > 0 {
		// Field names only: values may be personal data.
		s.audit(ctx, ActionUpdated, id, map[string]any{"fields": changed})
	}
	return updated, nil
}

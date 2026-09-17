package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

// UpdateClubBookInput changes a club book. Version is the version the caller read.
type UpdateClubBookInput struct {
	Version int64
	Changes domain.Changes
}

// UpdateClubBook changes one of the organisation orgID's club books. It returns
// ErrClubBookVersionConflict when in.Version is no longer current. An update
// that changes nothing returns the club book as it is.
func (s *Service) UpdateClubBook(ctx context.Context, orgID, id string, in UpdateClubBookInput) (domain.ClubBook, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.ClubBook{}, err
	}
	var updated domain.ClubBook
	var changed []string
	err := s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectClubBook(ctx, orgID, id, true)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrClubBookVersionConflict
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
		updated, err = tx.UpdateClubBook(ctx, next)
		return err
	})
	if err != nil {
		return domain.ClubBook{}, storeError("update", err)
	}
	if len(changed) > 0 {
		// Field names only: values may be personal data.
		s.audit(ctx, ActionUpdated, id, map[string]any{"fields": changed})
	}
	return updated, nil
}

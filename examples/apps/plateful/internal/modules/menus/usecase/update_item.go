package usecase

import (
	"context"

	"example.com/plateful/internal/modules/menus/domain"
)

// UpdateItemInput changes a menu item. Version is the version the caller
// read.
type UpdateItemInput struct {
	Version int64
	Changes domain.Changes
}

// UpdateItem changes one of the organisation orgID's menu items. It returns
// ErrItemVersionConflict when in.Version is no longer current, so two
// members editing the same dish can't silently overwrite each other. An
// update that changes nothing returns the item as it is.
func (s *Service) UpdateItem(ctx context.Context, orgID, id string, in UpdateItemInput) (domain.Item, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Item{}, err
	}
	var updated domain.Item
	var changed []string
	err := s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectItem(ctx, orgID, id, true)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrItemVersionConflict
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
		updated, err = tx.UpdateItem(ctx, next)
		return err
	})
	if err != nil {
		return domain.Item{}, storeError("update", err)
	}
	if len(changed) > 0 {
		// Field names only: a dish's description is the restaurant's own
		// copy, and the audit trail is not the place to keep a copy of it.
		s.audit(ctx, ActionUpdated, id, map[string]any{"fields": changed})
	}
	return updated, nil
}

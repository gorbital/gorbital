package usecase

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// UpdateOrderInput changes an order. Version is the version the caller read.
type UpdateOrderInput struct {
	Version int64
	Changes domain.Changes
}

// UpdateOrder changes one of the organisation orgID's orders. It returns
// ErrOrderVersionConflict when in.Version is no longer current. An update
// that changes nothing returns the order as it is.
func (s *Service) UpdateOrder(ctx context.Context, orgID, id string, in UpdateOrderInput) (domain.Order, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Order{}, err
	}
	var updated domain.Order
	var changed []string
	err := s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectOrder(ctx, orgID, id, true)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrOrderVersionConflict
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
		updated, err = tx.UpdateOrder(ctx, next)
		return err
	})
	if err != nil {
		return domain.Order{}, storeError("update", err)
	}
	if len(changed) > 0 {
		// Field names only: values may be personal data.
		s.audit(ctx, ActionUpdated, id, map[string]any{"fields": changed})
	}
	return updated, nil
}

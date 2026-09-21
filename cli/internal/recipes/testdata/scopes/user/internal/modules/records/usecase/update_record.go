package usecase

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

// UpdateRecordInput changes a record. Version is the version the caller read.
type UpdateRecordInput struct {
	Version int64
	Changes domain.Changes
}

// UpdateRecord changes one of the signed-in user's records. It returns
// ErrRecordVersionConflict when in.Version is no longer current. An update
// that changes nothing returns the record as it is.
func (s *Service) UpdateRecord(ctx context.Context, id string, in UpdateRecordInput) (domain.Record, error) {
	owner, err := ownerID(ctx)
	if err != nil {
		return domain.Record{}, err
	}
	var updated domain.Record
	var changed []string
	err = s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectRecord(ctx, owner, id, true)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrRecordVersionConflict
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
		updated, err = tx.UpdateRecord(ctx, next)
		return err
	})
	if err != nil {
		return domain.Record{}, storeError("update", err)
	}
	if len(changed) > 0 {
		// Field names only: values may be personal data.
		s.audit(ctx, ActionUpdated, id, map[string]any{"fields": changed})
	}
	return updated, nil
}

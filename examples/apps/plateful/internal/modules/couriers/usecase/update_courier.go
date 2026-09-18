package usecase

import (
	"context"

	"example.com/plateful/internal/modules/couriers/domain"
)

// UpdateCourierInput changes the caller's own profile. Version is the
// version they read.
type UpdateCourierInput struct {
	Version int64
	Changes domain.Changes
}

// docs:start update-availability

// UpdateCourier changes the signed-in account's own courier profile: their
// display name, their vehicle, and whether they are on duty.
//
// Availability is the interesting one, and it is why this reads the row
// inside a transaction with the row locked. A courier who is carrying an
// order may not go off duty: they would leave a diner's food in a bag on a
// bicycle and an order with nobody attached to it. The rule is the domain's
// (Courier.Apply returns ErrCourierOnDelivery, mapped to 409
// courier_on_delivery), but it is only sound if the order the courier is
// carrying can't be assigned between reading the row and writing it. The
// orders module assigns a courier with SELECT ... FOR UPDATE on this same
// row inside its own transaction, so locking here puts the two operations in
// a queue: whichever gets the lock first wins, and the other sees the result
// rather than a stale read. The version check on top of that is for the
// courier's own two devices, which are a different race with a different
// answer (409 courier_version_conflict).
func (s *Service) UpdateCourier(ctx context.Context, in UpdateCourierInput) (domain.Courier, error) {
	caller, err := callerID(ctx)
	if err != nil {
		return domain.Courier{}, err
	}
	var updated domain.Courier
	var changed []string
	err = s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectCourierByUser(ctx, caller, true)
		if err != nil {
			return err
		}
		// The ownership check that guard.OrgMember would be doing if a
		// courier had an organisation. It runs after the read, because the
		// only thing that says whose row this is, is in the row.
		if !current.OwnedBy(caller) {
			return domain.ErrCourierNotFound
		}
		if current.Version != in.Version {
			return domain.ErrCourierVersionConflict
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
		updated, err = tx.UpdateCourier(ctx, next)
		return err
	})
	if err != nil {
		return domain.Courier{}, storeError("update", err)
	}
	if len(changed) > 0 {
		// Field names only: a display name is personal data.
		s.audit(ctx, ActionUpdated, updated.ID, map[string]any{"fields": changed})
	}
	return updated, nil
}

// docs:end update-availability

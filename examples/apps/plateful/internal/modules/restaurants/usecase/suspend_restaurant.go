package usecase

import (
	"context"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// docs:start suspend-restaurant

// SuspendRestaurant stops a restaurant taking orders and records why.
//
// This is the platform's operation, not a tenant's: the route carries no
// organisation at all (/v1/platform/restaurants/{id}/suspend) and is guarded
// by guard.Permission(PermSuspend), which only the platform_admin role
// holds. A restaurant's own staff can never reach it, and the domain refuses
// the suspended status from an ordinary profile save
// (ErrSuspensionIsPlatformOnly), so there is no second way in.
//
// New orders stop as soon as this commits: the orders module's guard reads
// the restaurant's status on every order placed, so nothing has to be
// pushed anywhere for the suspension to take effect.
func (s *Service) SuspendRestaurant(ctx context.Context, id, reason string) (domain.Restaurant, error) {
	return s.setSuspension(ctx, id, reason, true)
}

// LiftSuspension lets a suspended restaurant work again. It comes back
// paused, never open: its own staff decide when it takes orders.
func (s *Service) LiftSuspension(ctx context.Context, id string) (domain.Restaurant, error) {
	return s.setSuspension(ctx, id, "", false)
}

func (s *Service) setSuspension(ctx context.Context, id, reason string, suspend bool) (domain.Restaurant, error) {
	if _, err := callerID(ctx); err != nil {
		return domain.Restaurant{}, err
	}
	var saved domain.Restaurant
	err := s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectRestaurant(ctx, id, true)
		if err != nil {
			return err
		}
		var next domain.Restaurant
		if suspend {
			next, err = current.Suspend(reason, s.clock())
		} else {
			next, err = current.Lift(s.clock())
		}
		if err != nil {
			return err
		}
		saved, err = tx.UpdateRestaurant(ctx, next)
		return err
	})
	if err != nil {
		return domain.Restaurant{}, storeError("suspend", err)
	}
	action := ActionUnsuspended
	metadata := map[string]any{"status": string(saved.Status)}
	if suspend {
		// The reason is what an operator wrote about a business, so the
		// audit event keeps it: that is the record of why.
		action, metadata["reason"] = ActionSuspended, reason
	}
	s.audit(ctx, action, saved.ID, metadata)
	return saved, nil
}

// docs:end suspend-restaurant

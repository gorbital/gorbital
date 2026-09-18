package usecase

import (
	"context"

	"example.com/plateful/internal/modules/couriers/domain"
)

// GetCourier returns the signed-in account's own courier profile, or
// ErrCourierNotFound when the account hasn't registered as one.
//
// The route is /v1/couriers/me and not /v1/couriers/{id} for the same reason
// the guard is guard.Permission: with no organisation in the path there is
// nothing that could make another courier's ID safe to accept, so the
// account is the only key the operation takes.
func (s *Service) GetCourier(ctx context.Context) (domain.Courier, error) {
	caller, err := callerID(ctx)
	if err != nil {
		return domain.Courier{}, err
	}
	c, err := s.store.SelectCourierByUser(ctx, caller, false)
	if err != nil {
		return domain.Courier{}, storeError("get", err)
	}
	// The query already selected by the account, so this holds; it is
	// written out because it is the rule, and on a courier the rule has no
	// guard to live in. A row that isn't the caller's is answered with the
	// same 404 as one that doesn't exist.
	if !c.OwnedBy(caller) {
		return domain.Courier{}, domain.ErrCourierNotFound
	}
	return c, nil
}

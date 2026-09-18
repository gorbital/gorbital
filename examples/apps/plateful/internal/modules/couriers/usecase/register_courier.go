package usecase

import (
	"context"

	"example.com/plateful/internal/modules/couriers/domain"
)

// RegisterCourierInput is the profile somebody registers with. There is no
// availability in it: a new courier starts off duty and goes on duty with an
// update, so registering never puts an order on a stranger.
type RegisterCourierInput struct {
	DisplayName string
	Vehicle     domain.Vehicle
}

// docs:start register-courier

// RegisterCourier creates the signed-in account's courier profile and
// returns it, or ErrCourierAlreadyRegistered when the account has one
// already.
//
// Look at what stands in for a tenant check here. An org-scoped use case
// takes an orgID from the path and calls memberID, which trusts
// guard.OrgMember to have proved the caller belongs there. This one takes no
// ID from the request at all: the owner of the new profile is the caller,
// full stop, so the account cannot register a courier for anybody else even
// if it asks. The uniqueness of user_id in the migration is what makes the
// second attempt a conflict rather than a second identity, and two racing
// requests get the same answer, because the database decides and not a
// read-then-write in this function.
func (s *Service) RegisterCourier(ctx context.Context, in RegisterCourierInput) (domain.Courier, error) {
	caller, err := callerID(ctx)
	if err != nil {
		return domain.Courier{}, err
	}
	c, err := domain.NewCourier(s.newID(), caller, in.DisplayName, in.Vehicle, s.clock())
	if err != nil {
		return domain.Courier{}, err
	}
	saved, err := s.store.InsertCourier(ctx, c)
	if err != nil {
		return domain.Courier{}, storeError("register", err)
	}
	s.audit(ctx, ActionRegistered, saved.ID, map[string]any{"vehicle": string(saved.Vehicle)})
	return saved, nil
}

// docs:end register-courier

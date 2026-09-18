package usecase

import (
	"context"

	"example.com/plateful/internal/modules/couriers/domain"
)

// Dispatch list sizes. A dispatcher is picking one courier from a short
// list, not paginating a directory, so the list is capped rather than
// cursored.
const (
	DefaultDispatchLimit = 20
	MaxDispatchLimit     = 100
)

// docs:start list-available-couriers

// ListAvailableCouriers returns the couriers the restaurant of organisation
// orgID could send an order out with right now: on duty, and carrying
// nothing.
//
// This is a cross-tenant read, and it is deliberate. The rows it returns
// belong to no organisation, so there is no org_id to filter by and no
// tenant boundary to stay inside; two restaurants asking at the same moment
// see the same couriers, which is the truth of the business — a courier
// works for whoever offers them the next job.
//
// What guard.OrgMember(PermDispatch) buys on this route is therefore not
// isolation but standing: it proves the caller is staff of a real restaurant
// on the platform, so the list of who is out working tonight isn't readable
// by every account that ever signed up. The organisation in the path is
// checked (memberID) and then plays no part in the query, which is worth
// saying out loud because it is the opposite of every other org route in
// this app.
//
// The consequence is that what comes back has to be narrowed by hand: the
// repository selects only what a restaurant needs to choose a courier, and
// the courier's sign-in account never leaves this module.
func (s *Service) ListAvailableCouriers(ctx context.Context, orgID string, limit int) ([]domain.Courier, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return nil, err
	}
	switch {
	case limit <= 0:
		limit = DefaultDispatchLimit
	case limit > MaxDispatchLimit:
		limit = MaxDispatchLimit
	}
	couriers, err := s.store.SelectAvailableCouriers(ctx, AvailableQuery{Limit: limit})
	if err != nil {
		return nil, storeError("list available", err)
	}
	return couriers, nil
}

// docs:end list-available-couriers

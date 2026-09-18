package usecase

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// GetOrgOrder returns one of the restaurant's orders to its staff. The
// organisation is part of the query, so there is nothing to check
// afterwards: a row of another restaurant simply isn't in the result.
func (s *Service) GetOrgOrder(ctx context.Context, orgID, id string) (domain.Order, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Order{}, err
	}
	order, err := s.store.SelectOrgOrder(ctx, orgID, id, false)
	if err != nil {
		return domain.Order{}, storeError("get", err)
	}
	return s.withLines(ctx, order)
}

// docs:start get-order

// GetOrder returns one order to the customer who placed it or the courier
// carrying it.
//
// There is no organisation in the path, and there could not be: neither
// caller is a member of one. The query therefore can't be scoped the way a
// restaurant's is — it reads the order by ID across the whole platform and
// then asks whether this caller is part of it. That is the shape to reach
// for whenever a row belongs to a tenant but the person allowed to see it
// does not, and the two things to get right are both here: the check is in
// the use case, where a job or a command would also reach it, and an order
// the caller has nothing to do with answers ErrOrderNotFound, exactly as one
// that doesn't exist, so IDs can't be probed.
func (s *Service) GetOrder(ctx context.Context, id string) (domain.Order, error) {
	caller, err := callerID(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	order, err := s.store.SelectOrder(ctx, id, false)
	if err != nil {
		return domain.Order{}, storeError("get", err)
	}
	if order.CustomerID != caller && !s.carriedBy(ctx, order, caller) {
		return domain.Order{}, domain.ErrOrderNotFound
	}
	return s.withLines(ctx, order)
}

// carriedBy reports whether the order is assigned to the account's courier
// profile. An account with no profile is simply not a courier.
func (s *Service) carriedBy(ctx context.Context, o domain.Order, userID string) bool {
	if o.CourierID == "" {
		return false
	}
	courier, err := s.store.CourierOfUser(ctx, userID)
	return err == nil && courier == o.CourierID
}

// docs:end get-order

// withLines fills one order's lines.
func (s *Service) withLines(ctx context.Context, o domain.Order) (domain.Order, error) {
	lines, err := s.store.SelectLines(ctx, []string{o.ID})
	if err != nil {
		return domain.Order{}, storeError("lines", err)
	}
	o.Lines = lines[o.ID]
	return o, nil
}

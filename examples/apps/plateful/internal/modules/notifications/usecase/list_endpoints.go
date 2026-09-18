package usecase

import (
	"context"

	"example.com/plateful/internal/modules/notifications/domain"
)

// ListEndpoints returns every endpoint of the organisation orgID, oldest
// first.
//
// There is no pagination: a restaurant has a handful of channels, not a
// feed, and a cursor here would be ceremony. The endpoints come back with
// the host each points at and without its URL — the repository asks
// PostgreSQL for the host and never selects the url column — so the secret
// is not in this process's memory at all on the path that renders a
// response.
func (s *Service) ListEndpoints(ctx context.Context, orgID string) ([]domain.Endpoint, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return nil, err
	}
	endpoints, err := s.store.SelectEndpoints(ctx, orgID)
	if err != nil {
		return nil, storeError("list", err)
	}
	return endpoints, nil
}

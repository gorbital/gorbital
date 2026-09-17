package repository

import (
	"context"
	"time"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/books/usecase"
)

var _ usecase.SubscriptionStore = (*Store)(nil)

// docs:start select-subscription

const selectSubscriptionExpirySQL = `SELECT expires_at FROM subscriptions WHERE user_id = $1`

// SelectSubscriptionExpiry returns when userID's plan ends, or the zero time
// when there is no row: a reader who never subscribed isn't an error, they
// simply have no plan.
func (s *Store) SelectSubscriptionExpiry(ctx context.Context, userID string) (time.Time, error) {
	var expires time.Time
	switch err := s.db.QueryRow(ctx, selectSubscriptionExpirySQL, userID).Scan(&expires); {
	case postgres.IsNoRows(err):
		return time.Time{}, nil
	case err != nil:
		return time.Time{}, err
	}
	return expires.UTC(), nil
}

// docs:end select-subscription

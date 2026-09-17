package usecase

import (
	"context"
	"fmt"
	"time"
)

// docs:start subscriptions

// SubscriptionStore reads a reader's subscription; repository.Store
// implements it with SQL. Shelfie has no billing module yet: when one
// arrives, this port is what moves behind it.
type SubscriptionStore interface {
	// SelectSubscriptionExpiry returns when userID's plan ends, or the zero
	// time when they never subscribed.
	SelectSubscriptionExpiry(ctx context.Context, userID string) (time.Time, error)
}

// Subscriptions answers whether the signed-in reader's plan is active. The
// guard on the export route asks it on every request, so the question stays
// one indexed lookup by primary key.
type Subscriptions struct {
	store SubscriptionStore
	now   func() time.Time
}

// NewSubscriptions returns the subscription questions answered from store,
// which may be nil while the OpenAPI document is exported.
func NewSubscriptions(store SubscriptionStore) *Subscriptions {
	return &Subscriptions{store: store, now: time.Now}
}

// Active reports whether the signed-in reader's subscription hasn't expired.
func (s *Subscriptions) Active(ctx context.Context) (bool, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return false, err
	}
	expires, err := s.store.SelectSubscriptionExpiry(ctx, reader)
	if err != nil {
		return false, fmt.Errorf("books: read subscription: %v", err) //nolint:errorlint // driver errors aren't API
	}
	return expires.After(s.now()), nil
}

// docs:end subscriptions

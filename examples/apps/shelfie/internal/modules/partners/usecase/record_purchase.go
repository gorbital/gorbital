package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/partners/domain"
)

// docs:start record-purchase

// RecordPurchase stores the purchase partner reported. A sender retries, and
// a verified request can be replayed inside the signature's tolerance, so
// the operation is idempotent: a delivery whose event ID the partner already
// sent returns the purchase stored then, unchanged and without a second
// audit event.
func (s *Service) RecordPurchase(ctx context.Context, partner string, e domain.Event) (domain.Purchase, error) {
	p, err := domain.NewPurchase(s.newID(), partner, e, s.clock())
	if err != nil {
		return domain.Purchase{}, err
	}
	stored, created, err := s.store.InsertPurchase(ctx, p)
	if err != nil {
		return domain.Purchase{}, storeError("record", err)
	}
	if created {
		s.audit(ctx, ActionRecorded, stored.ID, map[string]any{"partner": partner, "event_id": stored.EventID})
	}
	return stored, nil
}

// docs:end record-purchase

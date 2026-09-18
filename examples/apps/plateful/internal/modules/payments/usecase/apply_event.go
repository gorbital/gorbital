package usecase

import (
	"context"

	"example.com/plateful/internal/modules/payments/domain"
)

// docs:start apply-payment-event

// ApplyEvent applies what a verified delivery says happened, and reports
// whether this call was the one that applied it. It returns the payment as
// it now stands.
//
// Three things can arrive, and only one of them changes anything.
//
//   - An event type this app doesn't act on. It is accepted and ignored, so
//     the provider stops retrying it: a provider's vocabulary grows without
//     warning, and refusing the words this app hasn't learned yet would buy
//     an endless retry loop and nothing else.
//   - A redelivery. The provider retries a delivery it isn't sure arrived,
//     signing it afresh each time, so the signature says nothing about
//     whether it has been applied. The event ID does: it is the primary key
//     of payment_events, and the insert happens in the same transaction as
//     the status change, so a redelivery loses the race and leaves the
//     payment alone.
//   - A new event. The event ID is recorded and the payment moves, both or
//     neither.
func (s *Service) ApplyEvent(ctx context.Context, e domain.Event) (domain.Payment, bool, error) {
	status, known := domain.StatusFor(e.Kind)
	if !known {
		s.logger.InfoContext(ctx, "payment event ignored", "event_kind", e.Kind, "event_id", e.ID)
		return domain.Payment{}, false, nil
	}
	e, err := e.Clean()
	if err != nil {
		return domain.Payment{}, false, err
	}
	e.ReceivedAt = s.clock()

	var moved domain.Payment
	applied := false
	err = s.store.InTx(ctx, func(tx Store) error {
		// Locked first, so two deliveries about one payment queue up rather
		// than read the same version and both try to write it.
		current, err := tx.SelectPayment(ctx, e.PaymentID, true)
		if err != nil {
			return err
		}
		moved = current
		// The event ID, not the signature, is what stops a redelivery being
		// applied twice.
		fresh, err := tx.InsertEvent(ctx, e)
		if err != nil || !fresh {
			return err
		}
		next, err := current.MoveTo(status, e.Reason, s.clock())
		if err != nil {
			return err
		}
		applied = true
		moved, err = tx.UpdatePayment(ctx, next)
		return err
	})
	if err != nil {
		return domain.Payment{}, false, storeError("apply event", err)
	}
	if applied {
		event := serviceEvent(actionFor(moved.Status), moved)
		event.Metadata = map[string]any{"event_id": e.ID, "event_kind": e.Kind, "order_id": moved.OrderID}
		if moved.FailureReason != "" {
			event.Metadata["failure_reason"] = moved.FailureReason
		}
		s.audit(ctx, event)
	}
	return moved, applied, nil
}

// docs:end apply-payment-event

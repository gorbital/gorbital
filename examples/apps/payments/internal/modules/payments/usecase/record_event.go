package usecase

import (
	"context"
	"errors"

	"example.com/payments/internal/modules/payments/domain"
)

// docs:start record-event

// RecordEvent records what a verified delivery says happened and enqueues
// its receipt, and reports whether this call was the one that recorded it.
// A delivery the provider sends again changes nothing and returns the
// payment recorded the first time, with false. An event type the app
// doesn't act on is ignored: the zero payment, and false.
func (s *Service) RecordEvent(ctx context.Context, e domain.Event) (domain.Payment, bool, error) {
	status, ok := domain.StatusFor(e.Type)
	if !ok {
		s.logger.InfoContext(ctx, "payment event ignored", "event_type", e.Type, "event_id", e.ID)
		return domain.Payment{}, false, nil
	}
	payment, err := domain.NewPayment(s.newID(), status, e, s.clock())
	if err != nil {
		return domain.Payment{}, false, err
	}

	var recorded domain.Payment
	var applied bool
	err = s.tx.InTx(ctx, func(tx Tx) error {
		var err error
		if recorded, applied, err = tx.InsertPayment(ctx, payment); err != nil {
			if errors.Is(err, domain.ErrDeliveryInProgress) {
				return err
			}
			return storeError("record", err)
		}
		if !applied {
			return nil // already recorded: the receipt was enqueued then
		}
		// In the transaction that inserted the payment, so a rollback takes
		// the job with it and a commit can't lose it.
		if err := tx.Enqueue(ctx, ReceiptArgs{PaymentID: recorded.ID}); err != nil {
			return storeError("enqueue receipt", err)
		}
		return nil
	})
	if err != nil {
		return domain.Payment{}, false, err
	}
	return recorded, applied, nil
}

// docs:end record-event

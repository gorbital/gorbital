package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/payments/internal/modules/payments/domain"
)

// docs:start insert-payment

// insertPaymentSQL records a payment unless its delivery is recorded
// already, and returns the row either way: the inserted one with applied
// true, the one already there with false. One statement, so a replay never
// needs a second round trip, and the unique index decides the race rather
// than a read the writer takes on trust.
const insertPaymentSQL = `
	WITH new AS (
		INSERT INTO payments (` + paymentColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING ` + paymentColumns + `
	)
	SELECT ` + paymentColumns + `, true AS applied FROM new
	UNION ALL
	SELECT ` + paymentColumns + `, false FROM payments
	WHERE event_id = $2 AND NOT EXISTS (SELECT 1 FROM new)`

// InsertPayment records p unless its EventID is already recorded, and
// reports which happened. It returns domain.ErrDeliveryInProgress when
// another transaction is inserting the same delivery and hasn't committed:
// this statement's snapshot can't see that row, so there is nothing
// truthful to return, and the provider's next retry finds it committed.
func (s *Store) InsertPayment(ctx context.Context, p domain.Payment) (domain.Payment, bool, error) {
	rows, err := s.db.Query(ctx, insertPaymentSQL,
		p.ID, p.EventID, p.ProviderPaymentID, p.AmountMinor, p.Currency, string(p.Status), p.OccurredAt, p.RecordedAt)
	if err != nil {
		return domain.Payment{}, false, err
	}
	got, err := pgx.CollectExactlyOneRow(rows, scanInsertedPayment)
	if postgres.IsNoRows(err) {
		return domain.Payment{}, false, domain.ErrDeliveryInProgress
	}
	if err != nil {
		return domain.Payment{}, false, err
	}
	return got.payment, got.applied, nil
}

// docs:end insert-payment

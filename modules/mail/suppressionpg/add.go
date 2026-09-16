package suppressionpg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// upsertSuppressionSQL adds an address, or records a new event for one
// already on the list: the latest event's reason, source and detail win.
// inserted is true for a new address.
const upsertSuppressionSQL = `
	INSERT INTO mail_suppressions AS s (email, reason, source, detail)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (email) DO UPDATE
	SET reason = EXCLUDED.reason, source = EXCLUDED.source, detail = EXCLUDED.detail, updated_at = statement_timestamp()
	RETURNING ` + suppressionColumns + `, (xmax = 0) AS inserted`

// deleteExpiredDeliveriesSQL removes some deliveries past their expiry, so
// the table holds only recent ones without a cleanup job.
const deleteExpiredDeliveriesSQL = `
	DELETE FROM mail_webhook_deliveries WHERE key IN (
		SELECT key FROM mail_webhook_deliveries WHERE expires_at < statement_timestamp() LIMIT 100
	)`

// insertDeliverySQL records a delivery, or renews one that expired; it
// affects no row for a delivery applied before and not yet expired. A
// concurrent transaction with the same key waits for this one.
const insertDeliverySQL = `
	INSERT INTO mail_webhook_deliveries AS d (key, expires_at) VALUES ($1, $2)
	ON CONFLICT (key) DO UPDATE SET expires_at = EXCLUDED.expires_at
	WHERE d.expires_at < statement_timestamp()`

// Add puts entries on the list and returns the addresses that weren't on it
// before. An address already on it keeps its ID and creation time, and takes
// the entry's reason, source and detail. Entries are applied in one
// transaction; at most 100 per call.
func (s *Store) Add(ctx context.Context, entries ...Entry) ([]Suppression, error) {
	valid, err := validEntries(entries)
	if err != nil {
		return nil, err
	}
	var added []Suppression
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		added, err = upsert(ctx, tx, valid)
		return err
	})
	if err != nil {
		return nil, err
	}
	return added, nil
}

// AddOnce is [Store.Add] for one provider delivery, such as a signed
// webhook request: key names it (for example "resend:" and its ID) and is
// remembered until expiresAt. A key applied before and not yet expired
// changes nothing and returns [ErrDuplicateDelivery]. The key and the
// entries commit together, so a delivery that failed can be retried.
func (s *Store) AddOnce(ctx context.Context, key string, expiresAt time.Time, entries ...Entry) ([]Suppression, error) {
	if key == "" || len(key) > maxKeyLength {
		return nil, fmt.Errorf("suppressionpg: a delivery key of 1 to %d bytes is required", maxKeyLength)
	}
	valid, err := validEntries(entries)
	if err != nil {
		return nil, err
	}
	var added []Suppression
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, deleteExpiredDeliveriesSQL); err != nil {
			return dbError("delete expired deliveries", err)
		}
		tag, err := tx.Exec(ctx, insertDeliverySQL, key, expiresAt)
		if err != nil {
			return dbError("record delivery", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrDuplicateDelivery
		}
		added, err = upsert(ctx, tx, valid)
		return err
	})
	if err != nil {
		return nil, err
	}
	return added, nil
}

func validEntries(entries []Entry) ([]Entry, error) {
	if len(entries) > maxEntries {
		return nil, fmt.Errorf("%w: at most %d entries at once", ErrInvalidEntry, maxEntries)
	}
	valid := make([]Entry, len(entries))
	var errs []error
	for i, e := range entries {
		v, err := e.validate()
		if err != nil {
			errs = append(errs, fmt.Errorf("entry %d: %w", i, err))
		}
		valid[i] = v
	}
	return valid, errors.Join(errs...)
}

func upsert(ctx context.Context, tx pgx.Tx, entries []Entry) ([]Suppression, error) {
	added := []Suppression{}
	for _, e := range entries {
		var (
			sup      Suppression
			inserted bool
		)
		err := tx.QueryRow(ctx, upsertSuppressionSQL, e.Email, string(e.Reason), e.Source, e.Detail).
			Scan(append(sup.fields(), &inserted)...)
		if err != nil {
			return nil, dbError("add address", err)
		}
		if inserted {
			added = append(added, sup.utc())
		}
	}
	return added, nil
}

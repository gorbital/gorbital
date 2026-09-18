package repository

import (
	"context"
	"time"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/orders/domain"
)

// NewStoreOn returns a store running every statement on db, for code that
// already holds a transaction of its own — such as the registration hook,
// which writes the customer's profile in the transaction that creates their
// account.
func NewStoreOn(db postgres.DBTX) *Store { return newStoreOn(db) }

// UpsertCustomer stores a customer's profile, replacing what is there.
func (s *Store) UpsertCustomer(ctx context.Context, c domain.Customer, now time.Time) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO customers (user_id, display_name, address, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (user_id) DO UPDATE
		SET display_name = excluded.display_name, address = excluded.address, updated_at = excluded.updated_at`,
		c.UserID, c.DisplayName, c.Address, now)
	return err
}

// SelectCustomerAddress returns the address a customer usually wants an
// order taken to, or "" when they have none and when they have no profile
// at all: an account created by a first Google sign-in never filled the
// registration form.
func (s *Store) SelectCustomerAddress(ctx context.Context, userID string) (string, error) {
	var address string
	err := s.db.QueryRow(ctx, `SELECT address FROM customers WHERE user_id = $1`, userID).Scan(&address)
	if postgres.IsNoRows(err) {
		return "", nil
	}
	return address, err
}

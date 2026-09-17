package repository

import (
	"context"

	"gorbital.dev/modules/postgres"
)

const selectConfirmedUserSQL = `SELECT user_id FROM phone_numbers WHERE phone = $1 AND confirmed_at IS NOT NULL`

// SelectConfirmedUser returns the account that confirmed phone, or false.
func (s *Store) SelectConfirmedUser(ctx context.Context, phone string) (string, bool, error) {
	var userID string
	err := s.db.QueryRow(ctx, selectConfirmedUserSQL, phone).Scan(&userID)
	if postgres.IsNoRows(err) {
		return "", false, nil
	}
	return userID, err == nil, err
}

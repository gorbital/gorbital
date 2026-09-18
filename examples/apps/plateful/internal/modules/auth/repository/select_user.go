package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

// Deleted accounts are never selected.
const (
	selectUserByEmailSQL = `SELECT ` + userColumns + ` FROM auth_users WHERE email_normalized = $1 AND deleted_at IS NULL`
	selectUserByIDSQL    = `SELECT ` + userColumns + ` FROM auth_users WHERE id = $1 AND deleted_at IS NULL`
	forUpdate            = ` FOR UPDATE`
)

// SelectUserByEmail returns the account for a normalized address, or
// ErrUserNotFound. lock locks the row until the transaction ends.
func (s *Store) SelectUserByEmail(ctx context.Context, normalizedEmail string, lock bool) (authdomain.User, error) {
	return s.selectUser(ctx, selectUserByEmailSQL, normalizedEmail, lock)
}

// SelectUserByID returns an account, or ErrUserNotFound.
func (s *Store) SelectUserByID(ctx context.Context, id string, lock bool) (authdomain.User, error) {
	return s.selectUser(ctx, selectUserByIDSQL, id, lock)
}

func (s *Store) selectUser(ctx context.Context, sql, arg string, lock bool) (authdomain.User, error) {
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, arg)
	if err != nil {
		return authdomain.User{}, err
	}
	u, err := pgx.CollectExactlyOneRow(rows, scanUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.User{}, authdomain.ErrUserNotFound
	}
	return u, err
}

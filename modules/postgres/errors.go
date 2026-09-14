package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SQLSTATE codes classified by this package.
// https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	codeNotNullViolation     = "23502"
	codeForeignKeyViolation  = "23503"
	codeUniqueViolation      = "23505"
	codeCheckViolation       = "23514"
	codeSerializationFailure = "40001"
	codeDeadlockDetected     = "40P01"
)

// IsNoRows reports whether err means a query returned no rows, as from
// QueryRow(...).Scan. Repositories return their not-found domain error.
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// UniqueViolation reports whether err is a unique constraint violation and
// returns the constraint's name, so repositories can map it to a domain
// error such as ErrDuplicateEmail.
func UniqueViolation(err error) (constraint string, ok bool) {
	return violation(err, codeUniqueViolation)
}

// ForeignKeyViolation reports whether err is a foreign key violation and
// returns the constraint's name.
func ForeignKeyViolation(err error) (constraint string, ok bool) {
	return violation(err, codeForeignKeyViolation)
}

// CheckViolation reports whether err is a check constraint violation and
// returns the constraint's name.
func CheckViolation(err error) (constraint string, ok bool) {
	return violation(err, codeCheckViolation)
}

// NotNullViolation reports whether err is a not-null violation and returns
// the column's name.
func NotNullViolation(err error) (column string, ok bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == codeNotNullViolation {
		return pgErr.ColumnName, true
	}
	return "", false
}

// IsRetryable reports whether err is a serialization failure or deadlock,
// after which the whole transaction can be retried.
func IsRetryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == codeSerializationFailure || pgErr.Code == codeDeadlockDetected)
}

func violation(err error, code string) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == code {
		return pgErr.ConstraintName, true
	}
	return "", false
}

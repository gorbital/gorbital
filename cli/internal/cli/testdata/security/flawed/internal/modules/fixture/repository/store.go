// Package repository is the flawed fixture's data access. Every statement
// here is a planted mistake; the near-miss fixture holds the same code
// written properly. Neither is compiled: they are read by orb doctor
// --security's tests.
package repository

import "context"

type Store struct{ db DB }

// DB is the little of pgx the fixture needs.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (any, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

// Row is one row.
type Row interface{ Scan(dest ...any) error }

// credential-stored-unhashed reports the column, once, against the
// migration that created it: this statement is the same mistake and must
// not be a second finding.
const insertSessionSQL = `
	INSERT INTO fixture_sessions (id, user_id, token, created_at)
	VALUES ($1, $2, $3, $4)`

func (s *Store) InsertSession(ctx context.Context, id, userID, token string, now any) error {
	_, err := s.db.Exec(ctx, insertSessionSQL, id, userID, token, now)
	return err
}

// credential-stored-unhashed: a table no migration here declares, so the
// statement is the only place it can be reported.
const insertKeySQL = `INSERT INTO library_api_keys (id, secret, created_at) VALUES ($1, $2, $3)`

func (s *Store) InsertKey(ctx context.Context, id, secret string, now any) error {
	_, err := s.db.Exec(ctx, insertKeySQL, id, secret, now)
	return err
}

// password-without-kdf: what reaches password_hash is the password.
const insertUserSQL = `INSERT INTO fixture_users (id, email, password_hash) VALUES ($1, $2, $3)`

func (s *Store) InsertUser(ctx context.Context, id, email, password string) error {
	_, err := s.db.Exec(ctx, insertUserSQL, id, email, password)
	return err
}

// scope-query-without-soft-delete: a deleted tenant's members keep their
// role.
const memberRoleSQL = `
	SELECT m.role FROM fixture_members m JOIN fixture_tenants t ON t.id = m.tenant_id
	WHERE m.tenant_id = $1 AND m.user_id = $2`

func (s *Store) MemberRole(ctx context.Context, tenantID, userID string) (string, error) {
	var role string
	err := s.db.QueryRow(ctx, memberRoleSQL, tenantID, userID).Scan(&role)
	return role, err
}

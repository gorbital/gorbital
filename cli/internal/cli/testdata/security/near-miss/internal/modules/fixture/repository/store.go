// Package repository is the near-miss fixture's data access: the flawed
// fixture's statements written the way they should be, plus the shapes
// that look like a mistake and are not. A finding here is a false
// positive, which is the failure this rule set most has to avoid.
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

const insertSessionSQL = `
	INSERT INTO fixture_sessions (id, user_id, token_hash, created_at)
	VALUES ($1, $2, $3, $4)`

func (s *Store) InsertSession(ctx context.Context, id, userID string, tokenHash []byte, now any) error {
	_, err := s.db.Exec(ctx, insertSessionSQL, id, userID, tokenHash, now)
	return err
}

// The value reaching password_hash is named for what it is.
const insertUserSQL = `INSERT INTO fixture_users (id, email, password_hash) VALUES ($1, $2, $3)`

func (s *Store) InsertUser(ctx context.Context, id, email, passwordHash string) error {
	_, err := s.db.Exec(ctx, insertUserSQL, id, email, passwordHash)
	return err
}

// And here it is derived in the function itself, from a plaintext
// argument: the name of the argument is not what decides it.
const updatePasswordSQL = `UPDATE fixture_users SET password_hash = $2 WHERE id = $1`

func (s *Store) UpdatePassword(ctx context.Context, id, password string) error {
	hashed := hashPassword(password)
	_, err := s.db.Exec(ctx, updatePasswordSQL, id, hashed)
	return err
}

func hashPassword(password string) string { return password }

// A membership query that asks whether the tenant is still there.
const memberRoleSQL = `
	SELECT m.role FROM fixture_members m JOIN fixture_tenants t ON t.id = m.tenant_id
	WHERE m.tenant_id = $1 AND m.user_id = $2 AND t.deleted_at IS NULL`

func (s *Store) MemberRole(ctx context.Context, tenantID, userID string) (string, error) {
	var role string
	err := s.db.QueryRow(ctx, memberRoleSQL, tenantID, userID).Scan(&role)
	return role, err
}

// A query on the members alone, deliberately including the members of
// deleted tenants: the tenant table isn't in it, so the rule says nothing.
const selectMembersSQL = `
	SELECT m.user_id, m.role FROM fixture_members m JOIN fixture_users u ON u.id = m.user_id
	WHERE m.tenant_id = $1`

func (s *Store) Members(ctx context.Context, tenantID string) (string, error) {
	var out string
	err := s.db.QueryRow(ctx, selectMembersSQL, tenantID).Scan(&out)
	return out, err
}

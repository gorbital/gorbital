package repository

import (
	"context"
	"time"
)

// Codes sent since createdBefore mean someone is still verifying the
// address; accounts with a Google, Apple or GitHub identity are in use.
const markUnverifiedUsersDeletedSQL = `
	WITH expired AS (
		SELECT u.id FROM auth_users u
		WHERE u.deleted_at IS NULL AND u.email_verified_at IS NULL AND u.created_at < $1
		  AND NOT EXISTS (SELECT 1 FROM auth_identities i WHERE i.user_id = u.id)
		  AND NOT EXISTS (SELECT 1 FROM auth_codes c WHERE c.user_id = u.id AND c.created_at >= $1)
		ORDER BY u.created_at
		LIMIT $3
		FOR UPDATE OF u SKIP LOCKED
	)
	UPDATE auth_users u SET deleted_at = $2, updated_at = $2
	FROM expired WHERE u.id = expired.id
	RETURNING u.id`

// MarkUnverifiedUsersDeleted soft-deletes up to limit accounts created
// before createdBefore whose address was never verified, which have no
// Google, Apple or GitHub identity and no code sent since, and returns their IDs.
func (s *Store) MarkUnverifiedUsersDeleted(ctx context.Context, createdBefore, now time.Time, limit int) ([]string, error) {
	rows, err := s.db.Query(ctx, markUnverifiedUsersDeletedSQL, createdBefore, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

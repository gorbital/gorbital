package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

// Revoking a revoked key keeps its first revocation; the row lock makes two
// revocations at once report one. Exactly one of $2 (a user) and $3 (a
// service account) is set.
//
//nolint:gosec // SQL text, not a credential
const revokeAPIKeySQL = `
	WITH found AS (
		SELECT id, revoked_at IS NULL AS active FROM auth_api_keys
		WHERE id = $1 AND (user_id = NULLIF($2, '') OR service_account_id = NULLIF($3, ''))
		FOR UPDATE
	)
	UPDATE auth_api_keys k
	SET revoked_at = COALESCE(k.revoked_at, $4), revoked_reason = CASE WHEN found.active THEN $5 ELSE k.revoked_reason END
	FROM found WHERE k.id = found.id
	RETURNING ` + apiKeyColumnsK + `, found.active`

// RevokeAPIKey revokes a key of a user or a service account and returns it,
// reporting whether this call revoked it; or ErrAPIKeyNotFound.
func (s *Store) RevokeAPIKey(ctx context.Context, id, userID, serviceAccountID string, now time.Time, reason string) (authdomain.APIKey, bool, error) {
	var (
		k       authdomain.APIKey
		revoked bool
	)
	rows, err := s.db.Query(ctx, revokeAPIKeySQL, id, userID, serviceAccountID, now, reason)
	if err != nil {
		return k, false, err
	}
	_, err = pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (struct{}, error) {
		return struct{}{}, row.Scan(append(apiKeyTargets(&k), &revoked)...)
	})
	if postgres.IsNoRows(err) {
		return authdomain.APIKey{}, false, authdomain.ErrAPIKeyNotFound
	}
	return k, revoked, err
}

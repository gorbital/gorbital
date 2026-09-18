package repository

import (
	"github.com/jackc/pgx/v5"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

const serviceAccountColumns = `id, COALESCE(org_id, ''), name, description, roles, created_by, created_at, updated_at, disabled_at`

func scanServiceAccount(row pgx.CollectableRow) (authdomain.ServiceAccount, error) {
	var a authdomain.ServiceAccount
	err := row.Scan(&a.ID, &a.OrgID, &a.Name, &a.Description, &a.Roles, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt, &a.DisabledAt)
	a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
	return a, err
}

// apiKeyColumns and apiKeyColumnsK (for the alias k) are read by
// apiKeyTargets, in this order.
//
//nolint:gosec // SQL text, not a credential
const (
	apiKeyColumns = `id, lookup_id, secret_hash, COALESCE(user_id, ''), COALESCE(service_account_id, ''), name, scopes,
		expires_at, created_by, created_at, last_used_at, revoked_at, revoked_reason`
	apiKeyColumnsK = `k.id, k.lookup_id, k.secret_hash, COALESCE(k.user_id, ''), COALESCE(k.service_account_id, ''), k.name, k.scopes,
		k.expires_at, k.created_by, k.created_at, k.last_used_at, k.revoked_at, k.revoked_reason`
)

func apiKeyTargets(k *authdomain.APIKey) []any {
	return []any{&k.ID, &k.LookupID, &k.SecretHash, &k.UserID, &k.ServiceAccountID, &k.Name, &k.Scopes,
		&k.ExpiresAt, &k.CreatedBy, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt, &k.RevokedReason}
}

func scanAPIKey(row pgx.CollectableRow) (authdomain.APIKey, error) {
	var k authdomain.APIKey
	err := row.Scan(apiKeyTargets(&k)...)
	k.ExpiresAt, k.CreatedAt = k.ExpiresAt.UTC(), k.CreatedAt.UTC()
	return k, err
}

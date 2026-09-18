package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const selectOAuthStateByTokenHashSQL = `
	SELECT id, token_hash, browser_hash, provider, nonce, pkce_verifier, return_to,
	       coalesce(link_user_id, ''), coalesce(link_session_id, ''), expires_at, consumed_at, created_at
	FROM auth_oauth_states WHERE token_hash = $1 FOR UPDATE`

// SelectOAuthStateByTokenHash locks the web sign-in or link with a state's
// hash.
func (s *Store) SelectOAuthStateByTokenHash(ctx context.Context, tokenHash []byte) (authdomain.OAuthState, bool, error) {
	rows, err := s.db.Query(ctx, selectOAuthStateByTokenHashSQL, tokenHash)
	if err != nil {
		return authdomain.OAuthState{}, false, err
	}
	st, err := pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (authdomain.OAuthState, error) {
		var st authdomain.OAuthState
		err := row.Scan(&st.ID, &st.TokenHash, &st.BrowserHash, &st.Provider, &st.Nonce, &st.Verifier, &st.ReturnTo, &st.LinkUserID, &st.LinkSessionID, &st.ExpiresAt, &st.ConsumedAt, &st.CreatedAt)
		return st, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.OAuthState{}, false, nil
	}
	return st, err == nil, err
}

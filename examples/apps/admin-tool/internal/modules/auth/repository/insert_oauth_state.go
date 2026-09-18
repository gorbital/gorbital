package repository

import (
	"context"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const insertOAuthStateSQL = `
	INSERT INTO auth_oauth_states (id, token_hash, browser_hash, provider, nonce, pkce_verifier, return_to, link_user_id, link_session_id, expires_at, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10, $11)`

// InsertOAuthState stores a started web sign-in or link.
func (s *Store) InsertOAuthState(ctx context.Context, st authdomain.OAuthState) error {
	_, err := s.db.Exec(ctx, insertOAuthStateSQL, st.ID, st.TokenHash, st.BrowserHash, st.Provider, st.Nonce, st.Verifier, st.ReturnTo,
		st.LinkUserID, st.LinkSessionID, st.ExpiresAt, st.CreatedAt)
	return err
}

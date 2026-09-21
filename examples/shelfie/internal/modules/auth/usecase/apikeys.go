package usecase

import (
	"context"
	"errors"
	"slices"
	"time"

	"gorbital.dev/actor"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/ratelimit"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

// API keys (ADR-0058) authenticate programs as a user, with at most the
// user's permissions, or as a service account. They never carry permissions
// of roles that require two-factor authentication, can't manage the
// account's sign-in methods or create more keys, and are shown once.

const (
	// DefaultAPIKeyFailures is how many failed API key authentications one
	// client network may make per minute without a shared limiter.
	DefaultAPIKeyFailures = 30
	// expiredKeysBatch is how many expired keys one statement records, and
	// expiredKeysBatches how many statements one cleanup runs.
	expiredKeysBatch   = 500
	expiredKeysBatches = 20
)

// APIKeyInput describes a new API key.
type APIKeyInput struct {
	Name string
	// Scopes limit the key to these permissions, which its owner must hold
	// without two-factor authentication; empty: every permission the owner
	// holds that way, now and later.
	Scopes []string
	// ExpiresAt is required: at least an hour away and at most
	// auth.api_key_max_ttl.
	ExpiresAt time.Time
}

// CreatedAPIKey is a new API key.
type CreatedAPIKey struct {
	APIKey authdomain.APIKey
	// Key is sent as "Authorization: Bearer <key>". It is shown once: only
	// its hash is stored.
	Key string
}

// AuthenticateAPIKey returns the principal for an API key, and implements
// auth.APIKeyAuthenticator for the request middleware. It returns
// auth.ErrUnauthenticated for a malformed, unknown, revoked or expired key,
// a deleted user's key and a disabled service account's key, and an
// *auth.RateLimitError when the client's network failed too often.
//
// A user's key gets the permissions of the user's current roles and
// RoleUser, a platform service account's key those of its roles, both
// without roles that require two-factor authentication and limited to the
// key's scopes. An
// organisation's service account gets its permissions from
// orgs.RequireMember, in its organisation only. Only the lookup ID is ever
// logged.
func (s *Service) AuthenticateAPIKey(ctx context.Context, key string) (authlib.Principal, error) {
	lookupID, err := authlib.ParseAPIKey(key)
	if err != nil {
		return authlib.Principal{}, s.apiKeyFailed(ctx, "", "malformed")
	}
	k, account, err := s.store.SelectAPIKeyByLookupID(ctx, lookupID)
	if errors.Is(err, authdomain.ErrAPIKeyNotFound) {
		authlib.APIKeyMatches(key, nil) // the same work as a wrong key
		return authlib.Principal{}, s.apiKeyFailed(ctx, lookupID, "unknown")
	}
	if err != nil {
		return authlib.Principal{}, dbError("authenticate API key", err)
	}
	if !authlib.APIKeyMatches(key, k.SecretHash) {
		return authlib.Principal{}, s.apiKeyFailed(ctx, lookupID, "wrong_secret")
	}
	// The secret matched: a refusal from here on isn't a guess, so it
	// doesn't count toward the client's limit.
	now := s.now()
	refused := ""
	switch {
	case k.RevokedAt != nil:
		refused = "revoked"
	case !k.ActiveAt(now):
		refused = "expired"
	case k.ServiceAccountID != "" && account.Disabled():
		refused = "service_account_disabled"
	}
	if refused != "" {
		s.logger.InfoContext(ctx, "API key refused", "lookup_id", lookupID, "reason", refused)
		return authlib.Principal{}, authlib.ErrUnauthenticated
	}

	p := authlib.Principal{APIKeyID: k.ID, Scopes: k.Scopes}
	// Roles requiring two-factor authentication grant an API key nothing:
	// it can't sign in with a second factor.
	var granted []string
	if k.ServiceAccountID != "" {
		p.ServiceAccountID, p.OrgID = account.ID, account.OrgID
		if account.OrgID == "" {
			// An organisation's role applies only through orgs.RequireMember.
			granted, _ = s.catalog.PermissionsFor(account.Roles, false)
		}
	} else {
		p.UserID = k.UserID
		roles, err := s.store.SelectUserRoles(ctx, k.UserID)
		if err != nil {
			return authlib.Principal{}, dbError("authenticate API key", err)
		}
		granted, _ = s.userPermissions(roles, false)
	}
	p.Permissions, p.StepUp = p.Restrict(granted, nil)

	if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) >= authlib.APIKeyTouchInterval {
		if err := s.store.TouchAPIKey(ctx, k.ID, now, now.Add(-authlib.APIKeyTouchInterval)); err != nil {
			s.logger.WarnContext(ctx, "update API key last used", "lookup_id", lookupID, "err", err)
		}
	}
	return p, nil
}

// apiKeyFailed charges a failed authentication to the client's network (an
// IPv4 address or IPv6 /64) and returns auth.ErrUnauthenticated, or an
// *auth.RateLimitError once the network has failed too often. The key is
// never logged; lookupID may be empty for a malformed key.
func (s *Service) apiKeyFailed(ctx context.Context, lookupID, reason string) error {
	s.logger.DebugContext(ctx, "API key authentication failed", "lookup_id", lookupID, "reason", reason)
	client := authlib.ClientInfoFromContext(ctx)
	if ok, retry := s.allow(ctx, s.apiKeyLimiter, "api_key "+ratelimit.ClientKey(client.IP)); !ok {
		s.logger.WarnContext(ctx, "API key authentications limited for a client network", "retry_after", retry.String())
		return &authlib.RateLimitError{RetryAfter: retry}
	}
	return authlib.ErrUnauthenticated
}

// ListAPIKeys returns the signed-in user's API keys, newest first, with the
// expired and revoked ones cleanup hasn't removed yet.
func (s *Service) ListAPIKeys(ctx context.Context) ([]authdomain.APIKey, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := s.store.SelectAPIKeys(ctx, p.UserID, "")
	if err != nil {
		return nil, dbError("list API keys", err)
	}
	return keys, nil
}

// CreateAPIKey creates an API key for the signed-in user, whose address must
// be verified. It checks the user as confirmUser does (the password, or a
// second factor verified in the last 10 minutes). It returns
// ErrInvalidAPIKeyName, ErrInvalidAPIKeyExpiry, ErrInvalidAPIKeyScopes,
// ErrAPIKeyLimitReached, ErrEmailNotVerified, ErrInvalidCredentials,
// ErrInvalidMFA, ErrSessionRequired or a *RateLimitError when the user's
// re-authentication budget is spent.
func (s *Service) CreateAPIKey(ctx context.Context, password string, in APIKeyInput) (CreatedAPIKey, error) {
	if _, err := requirePrincipal(ctx); err != nil {
		return CreatedAPIKey{}, err
	}
	in, err := s.apiKeyInput(ctx, in)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	p, err := s.reauthPrincipal(ctx)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	var (
		out   CreatedAPIKey
		state error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if err != nil {
			return err // ErrUserNotFound is handled below
		}
		if !u.EmailVerified() {
			state = authdomain.ErrEmailNotVerified
			return nil
		}
		if state, err = s.confirmUser(ctx, tx, p, u, password); err != nil || state != nil {
			return err
		}
		roles, err := tx.SelectUserRoles(ctx, u.ID)
		if err != nil {
			return err
		}
		granted, _ := s.userPermissions(roles, false)
		if !scopesAllowed(in.Scopes, granted, s.orgScopes()) {
			state = authdomain.ErrInvalidAPIKeyScopes
			return nil
		}
		out, state, err = s.insertAPIKey(ctx, tx, authdomain.APIKey{UserID: u.ID}, in)
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return CreatedAPIKey{}, authlib.ErrUnauthenticated
	case err != nil:
		return CreatedAPIKey{}, dbError("create API key", err)
	case state != nil:
		return CreatedAPIKey{}, s.reauthFailed(ctx, p.UserID, state)
	}
	s.audit(ctx, apiKeyEvent("auth.api_key.created", out.APIKey, ""))
	return out, nil
}

// RevokeAPIKey revokes one of the signed-in user's API keys at once.
// Revoking a revoked key changes nothing. It returns ErrAPIKeyNotFound for a
// key of someone else.
func (s *Service) RevokeAPIKey(ctx context.Context, id string) error {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return err
	}
	return s.revokeAPIKey(ctx, authdomain.APIKey{ID: id, UserID: p.UserID}, "", authdomain.RevokedByOwner)
}

// revokeAPIKey revokes the key k.ID of k's owner and records it.
func (s *Service) revokeAPIKey(ctx context.Context, k authdomain.APIKey, orgID, reason string) error {
	k, revoked, err := s.store.RevokeAPIKey(ctx, k.ID, k.UserID, k.ServiceAccountID, s.now(), reason)
	switch {
	case errors.Is(err, authdomain.ErrAPIKeyNotFound):
		return err
	case err != nil:
		return dbError("revoke API key", err)
	case revoked:
		e := apiKeyEvent("auth.api_key.revoked", k, orgID)
		e.Metadata["reason"] = reason
		s.audit(ctx, e)
	}
	return nil
}

// apiKeyInput validates a new key's name, scopes and expiry.
func (s *Service) apiKeyInput(ctx context.Context, in APIKeyInput) (APIKeyInput, error) {
	var err error
	if in.Name, err = authdomain.APIKeyName(in.Name); err != nil {
		return in, err
	}
	if in.Scopes, err = authdomain.APIKeyScopes(in.Scopes); err != nil {
		return in, err
	}
	now := s.now()
	maxTTL := authlib.APIKeyTTLLimits.Clamp(s.apiKeyMaxTTL.Get(ctx))
	if in.ExpiresAt.Before(now.Add(authdomain.MinAPIKeyLifetime)) || in.ExpiresAt.After(now.Add(maxTTL)) {
		return in, authdomain.ErrInvalidAPIKeyExpiry
	}
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	return in, nil
}

// insertAPIKey creates a key for owner (k's UserID or ServiceAccountID) from
// validated input, unless the owner has MaxAPIKeys usable keys, which it
// returns as state.
func (s *Service) insertAPIKey(ctx context.Context, tx Store, k authdomain.APIKey, in APIKeyInput) (out CreatedAPIKey, state, err error) {
	now := s.now()
	n, err := tx.CountActiveAPIKeys(ctx, k.UserID, k.ServiceAccountID, now)
	if err != nil || n >= authdomain.MaxAPIKeys {
		if err == nil {
			state = authdomain.ErrAPIKeyLimitReached
		}
		return out, state, err
	}
	key, lookupID, hash := authlib.NewAPIKey()
	k.ID, k.LookupID, k.SecretHash = authlib.NewID("key"), lookupID, hash
	k.Name, k.Scopes, k.ExpiresAt = in.Name, in.Scopes, in.ExpiresAt
	k.CreatedBy, k.CreatedAt = by(ctx), now.UTC().Truncate(time.Microsecond)
	if err := tx.InsertAPIKey(ctx, k); err != nil {
		return out, nil, err
	}
	return CreatedAPIKey{APIKey: k, Key: key}, nil, nil
}

// keysRevoked records that n keys of an owner were revoked together.
func (s *Service) keysRevoked(ctx context.Context, ownerType, ownerID, orgID string, n int64, reason string) {
	if n == 0 {
		return
	}
	s.audit(ctx, auditEvent{
		Action: "auth.api_key.revoked", OrgID: orgID, ResourceType: ownerType, ResourceID: ownerID,
		Metadata: map[string]any{"reason": reason, "count": n},
	})
}

// userPermissions returns the permissions a user's roles grant, with those
// of RoleUser, which every user holds without a grant. Every signed-in
// operation that needs no other role checks one of RoleUser's permissions,
// so an API key's scopes limit it too (ADR-0058).
func (s *Service) userPermissions(roles []string, mfaVerified bool) (granted, stepUp []string) {
	return s.catalog.PermissionsFor(append(slices.Clone(roles), RoleUser), mfaVerified)
}

// scopesAllowed reports whether every scope is in one of the allowed lists.
func scopesAllowed(scopes []string, allowed ...[]string) bool {
	for _, scope := range scopes {
		if !slices.ContainsFunc(allowed, func(list []string) bool { return slices.Contains(list, scope) }) {
			return false
		}
	}
	return true
}

// orgScopes returns the organisation permissions any organisation role
// grants without two-factor authentication: a user's key may be limited to
// them, since the user's roles differ between organisations and are checked
// on every request.
func (s *Service) orgScopes() []string {
	if s.orgs == nil {
		return nil
	}
	c := s.orgs.Catalog()
	var roles []string
	for _, r := range c.Roles() {
		roles = append(roles, r.Name)
	}
	granted, _ := c.PermissionsFor(roles, false)
	return granted
}

// cleanUpAPIKeys records each key that expired since the last cleanup as
// auth.api_key.expired, then removes keys expired or revoked longer ago than
// auth.APIKeyRetention.
func (s *Service) cleanUpAPIKeys(ctx context.Context, now time.Time) (expired, deleted int64, err error) {
	for range expiredKeysBatches {
		keys, err := s.store.MarkAPIKeysExpired(ctx, now, expiredKeysBatch)
		if err != nil {
			return expired, 0, dbError("record expired API keys", err)
		}
		for _, k := range keys {
			e := apiKeyEvent("auth.api_key.expired", k, "")
			e.ActorKind, e.ActorID = actor.KindSystem, "auth_cleanup"
			s.audit(ctx, e)
		}
		expired += int64(len(keys))
		if len(keys) < expiredKeysBatch {
			break
		}
	}
	if deleted, err = s.store.DeleteOldAPIKeys(ctx, now.Add(-authlib.APIKeyRetention)); err != nil {
		return expired, 0, dbError("clean up API keys", err)
	}
	return expired, deleted, nil
}

// apiKeyEvent is an event about key k. Its metadata never holds the key or
// its hash: the lookup ID identifies it in logs.
func apiKeyEvent(action string, k authdomain.APIKey, orgID string) auditEvent {
	return auditEvent{
		Action: action, OrgID: orgID, ResourceType: "api_key", ResourceID: k.ID,
		Metadata: map[string]any{
			"owner_type": k.OwnerType(), "owner_id": k.OwnerID(), "lookup_id": k.LookupID,
			"scopes": k.Scopes, "expires_at": k.ExpiresAt,
		},
	}
}

// by names the actor in ctx for created_by columns.
func by(ctx context.Context) string {
	a := actor.FromOrAnonymous(ctx)
	return string(a.Kind) + ":" + a.ID
}

package usecase

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/actor"
	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// Operators' view of accounts, for the ops APIs under /ops/auth/users and
// the Dev Portal's Authentication screen (ADR-0070). Every method needs an
// actor holding PermOpsAuthRead or PermOpsAuthWrite; the platform
// administrator's role, and the development operator, hold both. They
// never need a session: the dev console's system actor uses them too.

// Permissions of the operators' account APIs. PermOpsAuthRead is the same
// permission the ops module's GET /ops/auth/providers uses.
const (
	PermOpsAuthRead  = "ops.auth.read"
	PermOpsAuthWrite = "ops.auth.write"
)

// UserFilter selects accounts for ListUsers.
type UserFilter struct {
	// Query matches a substring of the normalized address, or an ID.
	Query string
	// Cursor continues an earlier page; Limit is 1 to 100 (default 50).
	Cursor string
	Limit  int
}

// UserPage is a page of accounts, newest first.
type UserPage struct {
	Users []authdomain.User
	// NextCursor continues after the last user; empty on the last page.
	NextCursor string
}

// MFAStatus describes a user's second factors.
type MFAStatus struct {
	// TOTP reports a confirmed authenticator app.
	TOTP bool
	// RecoveryCodes counts unused recovery codes.
	RecoveryCodes int
	Passkeys      int
}

// UserDetail is everything an operator sees about an account.
type UserDetail struct {
	User       authdomain.User
	Sessions   []authdomain.Session
	Passkeys   []authdomain.Passkey
	Identities []authdomain.Identity
	MFA        MFAStatus
	// Codes are the usable verification and reset codes, without the codes
	// themselves: they are stored hashed, and arrive by email.
	Codes []authdomain.Code
}

const revokedAccountBanned = "account_banned"

// AuthorizeOps checks the actor holds permission (PermOpsAuthRead or
// PermOpsAuthWrite), for callers that pair it with an operator method
// that needs only an actor, such as GrantRole or ResetMFA.
func (s *Service) AuthorizeOps(ctx context.Context, permission string) error {
	return authorizeOps(ctx, permission)
}

// authorizeOps checks the actor holds permission, mapping actor errors to
// the module's.
func authorizeOps(ctx context.Context, permission string) error {
	switch err := actor.Require(ctx, permission); {
	case err == nil:
		return nil
	case errors.Is(err, actor.ErrUnauthenticated):
		return authdomain.ErrActorRequired
	case errors.Is(err, actor.ErrStepUpRequired):
		return authdomain.ErrStepUpRequired
	default:
		return authdomain.ErrForbidden
	}
}

// ListUsers lists accounts for an operator.
func (s *Service) ListUsers(ctx context.Context, f UserFilter) (UserPage, error) {
	if err := authorizeOps(ctx, PermOpsAuthRead); err != nil {
		return UserPage{}, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query := strings.ToLower(strings.TrimSpace(f.Query))
	if len(query) > 200 {
		query = query[:200]
	}
	var afterTime *time.Time
	var afterID string
	if f.Cursor != "" {
		t, id, err := decodeUserCursor(f.Cursor)
		if err != nil {
			return UserPage{}, err
		}
		afterTime, afterID = &t, id
	}
	users, err := s.store.SelectUsers(ctx, query, afterTime, afterID, limit+1)
	if err != nil {
		return UserPage{}, dbError("list users", err)
	}
	page := UserPage{Users: users}
	if len(users) > limit {
		page.Users = users[:limit]
		last := page.Users[limit-1]
		page.NextCursor = encodeUserCursor(last.CreatedAt, last.ID)
	}
	if page.Users == nil {
		page.Users = []authdomain.User{}
	}
	for i := range page.Users {
		if page.Users[i].Roles, err = s.store.SelectUserRoles(ctx, page.Users[i].ID); err != nil {
			return UserPage{}, dbError("list users", err)
		}
	}
	return page, nil
}

func encodeUserCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(t.UnixNano(), 10) + "|" + id))
}

func decodeUserCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", authdomain.ErrInvalidCursor
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return time.Time{}, "", authdomain.ErrInvalidCursor
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return time.Time{}, "", authdomain.ErrInvalidCursor
	}
	return time.Unix(0, n).UTC(), id, nil
}

// UserDetail returns an account with its sessions, passkeys, identities,
// second factors and usable codes, or ErrUserNotFound.
func (s *Service) UserDetail(ctx context.Context, userID string) (UserDetail, error) {
	if err := authorizeOps(ctx, PermOpsAuthRead); err != nil {
		return UserDetail{}, err
	}
	u, err := s.User(ctx, userID)
	if err != nil {
		return UserDetail{}, err
	}
	now := s.now()
	d := UserDetail{User: u}
	if d.Sessions, err = s.store.SelectActiveSessions(ctx, userID, now); err != nil {
		return UserDetail{}, dbError("user detail", err)
	}
	if d.Passkeys, err = s.store.SelectPasskeys(ctx, userID); err != nil {
		return UserDetail{}, dbError("user detail", err)
	}
	if d.Identities, err = s.store.SelectIdentities(ctx, userID); err != nil {
		return UserDetail{}, dbError("user detail", err)
	}
	if d.Codes, err = s.store.SelectUserCodes(ctx, userID, now); err != nil {
		return UserDetail{}, dbError("user detail", err)
	}
	totp, found, err := s.store.SelectTOTP(ctx, userID, false)
	if err != nil {
		return UserDetail{}, dbError("user detail", err)
	}
	d.MFA.TOTP = found && totp.ConfirmedAt != nil
	if d.MFA.RecoveryCodes, err = s.store.CountUnusedRecoveryCodes(ctx, userID); err != nil {
		return UserDetail{}, dbError("user detail", err)
	}
	d.MFA.Passkeys = len(d.Passkeys)
	for _, list := range []any{&d.Sessions, &d.Passkeys, &d.Identities, &d.Codes} {
		switch l := list.(type) {
		case *[]authdomain.Session:
			if *l == nil {
				*l = []authdomain.Session{}
			}
		case *[]authdomain.Passkey:
			if *l == nil {
				*l = []authdomain.Passkey{}
			}
		case *[]authdomain.Identity:
			if *l == nil {
				*l = []authdomain.Identity{}
			}
		case *[]authdomain.Code:
			if *l == nil {
				*l = []authdomain.Code{}
			}
		}
	}
	return d, nil
}

// VerifyUserEmail marks an address verified on an operator's behalf, as a
// code from the inbox would.
func (s *Service) VerifyUserEmail(ctx context.Context, userID string) error {
	if err := authorizeOps(ctx, PermOpsAuthWrite); err != nil {
		return err
	}
	u, err := s.User(ctx, userID)
	if err != nil {
		return err
	}
	if u.EmailVerified() {
		return nil
	}
	if err := s.store.MarkEmailVerified(ctx, u.ID, s.now()); err != nil {
		return dbError("verify email", err)
	}
	s.audit(ctx, userEvent("auth.email.verified_by_operator", u.ID, authlib.ClientInfo{}))
	return nil
}

// BanUser bans an account: it can't sign in, and its sessions and API keys
// are revoked. Banning again changes the reason.
func (s *Service) BanUser(ctx context.Context, userID, reason string) error {
	if err := authorizeOps(ctx, PermOpsAuthWrite); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 500 {
		reason = reason[:500]
	}
	var revoked int64
	err := s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, userID, true)
		if err != nil {
			return err
		}
		now := s.now()
		if err := tx.SetUserBan(ctx, u.ID, &now, reason, now); err != nil {
			return err
		}
		if revoked, err = tx.RevokeOwnerAPIKeys(ctx, u.ID, "", now, revokedAccountBanned); err != nil {
			return err
		}
		_, err = tx.RevokeUserSessions(ctx, u.ID, "", now, revokedAccountBanned)
		return err
	})
	if errors.Is(err, authdomain.ErrUserNotFound) {
		return err
	}
	if err != nil {
		return dbError("ban user", err)
	}
	e := userEvent("auth.user.banned", userID, authlib.ClientInfo{})
	e.Metadata = map[string]any{"reason": reason}
	s.audit(ctx, e)
	s.keysRevoked(ctx, authdomain.OwnerUser, userID, "", revoked, revokedAccountBanned)
	return nil
}

// UnbanUser lifts a ban; the account can sign in again.
func (s *Service) UnbanUser(ctx context.Context, userID string) error {
	if err := authorizeOps(ctx, PermOpsAuthWrite); err != nil {
		return err
	}
	u, err := s.User(ctx, userID)
	if err != nil {
		return err
	}
	if !u.Banned() {
		return nil
	}
	if err := s.store.SetUserBan(ctx, u.ID, nil, "", s.now()); err != nil {
		return dbError("unban user", err)
	}
	s.audit(ctx, userEvent("auth.user.unbanned", u.ID, authlib.ClientInfo{}))
	return nil
}

// DeleteUser deletes an account on an operator's behalf: what DeleteAccount
// does for the owner, without their password. The organisation hooks
// apply (a sole owner of an organisation can't be deleted).
func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	if err := authorizeOps(ctx, PermOpsAuthWrite); err != nil {
		return err
	}
	if _, err := s.User(ctx, userID); err != nil {
		return err
	}
	if err := s.checkAccountDeletion(ctx, userID); err != nil {
		return err
	}
	var revoked int64
	err := s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, userID, true)
		if err != nil {
			return err
		}
		now := s.now()
		if err := tx.MarkUserDeleted(ctx, u.ID, now); err != nil {
			return err
		}
		identities, err := tx.DeleteIdentities(ctx, u.ID)
		if err != nil {
			return err
		}
		if err := s.queueRevocations(ctx, tx, identities...); err != nil {
			return err
		}
		if revoked, err = tx.RevokeOwnerAPIKeys(ctx, u.ID, "", now, authdomain.RevokedAccountDeleted); err != nil {
			return err
		}
		_, err = tx.RevokeUserSessions(ctx, u.ID, "", now, "account_deleted")
		return err
	})
	if errors.Is(err, authdomain.ErrUserNotFound) {
		return err
	}
	if err != nil {
		return dbError("delete user", err)
	}
	s.audit(ctx, userEvent("auth.user.deleted_by_operator", userID, authlib.ClientInfo{}))
	s.keysRevoked(ctx, authdomain.OwnerUser, userID, "", revoked, authdomain.RevokedAccountDeleted)
	s.accountDeleted(ctx, userID)
	return nil
}

// RevokeUserSession ends one of a user's sessions, or ErrSessionNotFound.
func (s *Service) RevokeUserSession(ctx context.Context, userID, sessionID string) error {
	if err := authorizeOps(ctx, PermOpsAuthWrite); err != nil {
		return err
	}
	ok, err := s.store.RevokeSession(ctx, sessionID, userID, s.now(), "revoked_by_operator")
	if err != nil {
		return dbError("revoke session", err)
	}
	if !ok {
		return authdomain.ErrSessionNotFound
	}
	e := userEvent("auth.session.revoked_by_operator", userID, authlib.ClientInfo{})
	e.Metadata = map[string]any{"session_id": sessionID}
	s.audit(ctx, e)
	return nil
}

// RevokeUserSessions ends every session of a user and returns how many.
func (s *Service) RevokeUserSessions(ctx context.Context, userID string) (int64, error) {
	if err := authorizeOps(ctx, PermOpsAuthWrite); err != nil {
		return 0, err
	}
	if _, err := s.User(ctx, userID); err != nil {
		return 0, err
	}
	n, err := s.store.RevokeUserSessions(ctx, userID, "", s.now(), "revoked_by_operator")
	if err != nil {
		return 0, dbError("revoke sessions", err)
	}
	e := userEvent("auth.session.revoked_by_operator", userID, authlib.ClientInfo{})
	e.Metadata = map[string]any{"sessions": n}
	s.audit(ctx, e)
	return n, nil
}

// RemoveUserPasskey deletes one of a user's passkeys, or
// ErrPasskeyNotFound.
func (s *Service) RemoveUserPasskey(ctx context.Context, userID, passkeyID string) error {
	if err := authorizeOps(ctx, PermOpsAuthWrite); err != nil {
		return err
	}
	ok, err := s.store.DeletePasskey(ctx, passkeyID, userID)
	if err != nil {
		return dbError("remove passkey", err)
	}
	if !ok {
		return authdomain.ErrPasskeyNotFound
	}
	e := userEvent("auth.passkey.removed_by_operator", userID, authlib.ClientInfo{})
	e.Metadata = map[string]any{"passkey_id": passkeyID}
	s.audit(ctx, e)
	return nil
}

// RemoveUserIdentity unlinks one of a user's Google, Apple or GitHub
// identities, or ErrIdentityNotFound.
func (s *Service) RemoveUserIdentity(ctx context.Context, userID, identityID string) error {
	if err := authorizeOps(ctx, PermOpsAuthWrite); err != nil {
		return err
	}
	var provider string
	err := s.store.InTx(ctx, func(tx Store) error {
		identity, ok, err := tx.DeleteIdentity(ctx, identityID, userID)
		if err != nil {
			return err
		}
		if !ok {
			return authdomain.ErrIdentityNotFound
		}
		provider = identity.Provider
		return s.queueRevocations(ctx, tx, identity)
	})
	if errors.Is(err, authdomain.ErrIdentityNotFound) {
		return err
	}
	if err != nil {
		return dbError("remove identity", err)
	}
	e := userEvent("auth.identity.removed_by_operator", userID, authlib.ClientInfo{})
	e.Metadata = map[string]any{"identity_id": identityID, "provider": provider}
	s.audit(ctx, e)
	return nil
}

// Impersonate starts a session for a user on an operator's behalf, in
// development only (ErrImpersonationOff otherwise): the Dev Portal's route
// tester and auth playground act as that user. With mfaVerified the
// session counts as signed in with a second factor. The audit log records
// the operator and the session.
func (s *Service) Impersonate(ctx context.Context, userID string, mfaVerified bool) (LoginResult, error) {
	if err := authorizeOps(ctx, PermOpsAuthWrite); err != nil {
		return LoginResult{}, err
	}
	if !s.impersonation {
		return LoginResult{}, authdomain.ErrImpersonationOff
	}
	var res LoginResult
	err := s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, userID, false)
		if err != nil {
			return err
		}
		res, err = s.startSession(ctx, tx, u, mfaVerified, authlib.ClientInfo{UserAgent: "dev portal (impersonation)"})
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound), errors.Is(err, authdomain.ErrAccountBanned):
		return LoginResult{}, err
	case err != nil:
		return LoginResult{}, dbError("impersonate", err)
	}
	e := userEvent("auth.user.impersonated", userID, authlib.ClientInfo{})
	e.Metadata = map[string]any{"session_id": res.Session.ID, "mfa_verified": mfaVerified}
	s.audit(ctx, e)
	return res, nil
}

// String forms for the ops API.
func (m MFAStatus) String() string {
	return fmt.Sprintf("totp=%v recovery=%d passkeys=%d", m.TOTP, m.RecoveryCodes, m.Passkeys)
}

package usecase

import (
	"context"
	"errors"
	"time"

	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

// DeleteAccount deletes the signed-in user's account after checking the
// password (for an account without one, a recent sign-in), and a second
// factor when two-factor authentication is on (a code, a recovery code, or a
// passkey's response to BeginPasskeyVerification), and ends every session.
// It unlinks Google and Apple identities and queues Apple's tokens for
// revocation. The
// address can register again at once; Cleanup removes the account's data
// after the retention period. It returns ErrInvalidCredentials, ErrInvalidMFA,
// ErrMFAUnavailable or a *RateLimitError when the user's re-authentication
// budget is spent.
func (s *Service) DeleteAccount(ctx context.Context, password string, factor authdomain.SecondFactor) error {
	p, err := s.reauthPrincipal(ctx)
	if err != nil {
		return err
	}
	// Checked before the password and second factor, so a refusal doesn't
	// use up a one-time code.
	if err := s.checkAccountDeletion(ctx, p.UserID); err != nil {
		return err
	}
	var (
		state   error
		revoked int64
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if err != nil {
			return err // ErrUserNotFound is handled below
		}
		if ok, err := s.passwordOrRecentSignIn(ctx, p, u, password); err != nil || !ok {
			state = authdomain.ErrInvalidCredentials
			return err
		}
		if state, err = s.requireSecondFactor(ctx, tx, u.ID, factor); err != nil || state != nil {
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
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return authdomain.ErrInvalidCredentials
	case err != nil:
		return dbError("delete account", err)
	case state != nil:
		return s.reauthFailed(ctx, p.UserID, state)
	}
	s.audit(ctx, userEvent("auth.account.deleted", p.UserID, authlib.ClientInfoFromContext(ctx)))
	s.keysRevoked(ctx, authdomain.OwnerUser, p.UserID, "", revoked, authdomain.RevokedAccountDeleted)
	s.accountDeleted(ctx, p.UserID)
	return nil
}

// CreateUser creates an account for an operator, for example the first
// administrator. The actor in ctx is recorded. It returns
// auth.ErrInvalidEmail, an *auth.PasswordError, ErrEmailTaken,
// ErrActorRequired, or the OnRegister hook's *domain.Refusal, which rolls
// the account back.
func (s *Service) CreateUser(ctx context.Context, email, password string, emailVerified bool) (authdomain.User, error) {
	if _, err := requireActor(ctx); err != nil {
		return authdomain.User{}, err
	}
	email, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return authdomain.User{}, err
	}
	if err := authlib.ValidatePassword(ctx, password, s.checker); err != nil {
		return authdomain.User{}, err
	}
	hash, err := s.hasher.HashContext(ctx, password)
	if err != nil {
		return authdomain.User{}, err
	}
	now := s.now()
	var verifiedAt *time.Time
	if emailVerified {
		verifiedAt = &now
	}
	var u authdomain.User
	err = s.store.InTx(ctx, func(tx Store) error {
		var err error
		u, err = tx.InsertUser(ctx, authdomain.User{
			ID: authlib.NewID("usr"), Email: email, NormalizedEmail: normalized, PasswordHash: hash, EmailVerifiedAt: verifiedAt, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		return s.onRegister(ctx, tx, NewAccount{User: u, Method: authdomain.MethodOperator}, nil) // the operator's client isn't the account's
	})
	if errors.Is(err, authdomain.ErrEmailTaken) {
		return authdomain.User{}, err
	}
	if r, ok := refusal(err); ok {
		return authdomain.User{}, r
	}
	if err != nil {
		return authdomain.User{}, dbError("create user", err)
	}
	e := userEvent("auth.user.created", u.ID, authlib.ClientInfo{})
	e.Metadata = map[string]any{"email_verified": emailVerified}
	s.audit(ctx, e)
	s.accountCreated(ctx, u.ID)
	return u, nil
}

// User returns an account with its roles, or ErrUserNotFound.
func (s *Service) User(ctx context.Context, id string) (authdomain.User, error) {
	return s.withRoles(ctx, "find user", func() (authdomain.User, error) { return s.store.SelectUserByID(ctx, id, false) })
}

// UserByEmail returns the account for an address with its roles, or
// ErrUserNotFound. It is for operators; public endpoints must not reveal
// whether an address has an account.
func (s *Service) UserByEmail(ctx context.Context, email string) (authdomain.User, error) {
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return authdomain.User{}, authdomain.ErrUserNotFound
	}
	return s.withRoles(ctx, "find user", func() (authdomain.User, error) { return s.store.SelectUserByEmail(ctx, normalized, false) })
}

func (s *Service) withRoles(ctx context.Context, op string, find func() (authdomain.User, error)) (authdomain.User, error) {
	u, err := find()
	if errors.Is(err, authdomain.ErrUserNotFound) {
		return authdomain.User{}, err
	}
	if err != nil {
		return authdomain.User{}, dbError(op, err)
	}
	if u.Roles, err = s.store.SelectUserRoles(ctx, u.ID); err != nil {
		return authdomain.User{}, dbError(op, err)
	}
	return u, nil
}

// GrantRole gives a user a platform role. Granting a role the user has
// changes nothing. It returns ErrUnknownRole (also for RoleUser, which every
// user holds), ErrUserNotFound, ErrEmailNotVerified for an account whose
// address isn't verified, or ErrActorRequired.
func (s *Service) GrantRole(ctx context.Context, userID, role string) error {
	a, err := requireActor(ctx)
	if err != nil {
		return err
	}
	if !s.catalog.HasRole(role) || role == RoleUser {
		// Every user holds RoleUser without a grant.
		return authdomain.ErrUnknownRole
	}
	u, err := s.store.SelectUserByID(ctx, userID, false)
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return err
	case err != nil:
		return dbError("grant role", err)
	case !u.EmailVerified():
		// Whoever verifies the address later may not be who registered it:
		// verification removes what came before, but a role would stay.
		return authdomain.ErrEmailNotVerified
	}
	added, err := s.store.InsertUserRole(ctx, userID, role, string(a.Kind)+":"+a.ID, s.now())
	if err != nil {
		return dbError("grant role", err)
	}
	if added {
		e := userEvent("auth.role.granted", userID, authlib.ClientInfo{})
		e.Metadata = map[string]any{"role": role}
		s.audit(ctx, e)
	}
	return nil
}

// RevokeRole removes a platform role from a user. Revoking a role the user
// doesn't have changes nothing.
func (s *Service) RevokeRole(ctx context.Context, userID, role string) error {
	if _, err := requireActor(ctx); err != nil {
		return err
	}
	removed, err := s.store.DeleteUserRole(ctx, userID, role)
	if err != nil {
		return dbError("revoke role", err)
	}
	if removed {
		e := userEvent("auth.role.revoked", userID, authlib.ClientInfo{})
		e.Metadata = map[string]any{"role": role}
		s.audit(ctx, e)
	}
	return nil
}

// Cleanup removes sessions that ended more than 7 days ago, codes older
// than a day, API keys expired or revoked more than 30 days ago (recording
// each key's expiry first), and accounts deleted longer ago than the
// retention period. It
// deletes accounts still unverified after auth.unverified_account_ttl, like
// account deletion, so an abandoned or unowned registration doesn't hold an
// address forever; accounts with a Google, Apple or GitHub identity are kept. The
// auth_cleanup job runs it.
func (s *Service) Cleanup(ctx context.Context) (authdomain.CleanupResult, error) {
	now := s.now()
	var res authdomain.CleanupResult
	var err error
	if res.Unverified, err = s.expireUnverified(ctx, now); err != nil {
		return res, err
	}
	if res.ExpiredAPIKeys, res.APIKeys, err = s.cleanUpAPIKeys(ctx, now); err != nil {
		return res, err
	}
	if res.Sessions, err = s.store.DeleteEndedSessions(ctx, now.Add(-authlib.EndedSessionRetention)); err != nil {
		return res, dbError("clean up sessions", err)
	}
	if res.Codes, err = s.store.DeleteOldCodes(ctx, now.Add(-authlib.CodeRetention)); err != nil {
		return res, dbError("clean up codes", err)
	}
	if res.Challenges, err = s.store.DeleteOldMFAChallenges(ctx, now.Add(-authlib.CodeRetention)); err != nil {
		return res, dbError("clean up sign-in challenges", err)
	}
	// Ceremonies go as soon as they expire: public endpoints start them.
	ceremonies, err := s.store.DeleteOldWebAuthnCeremonies(ctx, now)
	if err != nil {
		return res, dbError("clean up passkey ceremonies", err)
	}
	res.Challenges += ceremonies
	socialRequests, err := s.store.DeleteOldSocialRequests(ctx, now)
	if err != nil {
		return res, dbError("clean up Google and Apple sign-ins", err)
	}
	res.Challenges += socialRequests
	retention := authlib.DeletedRetentionLimits.Clamp(s.retention.Get(ctx))
	if res.Users, err = s.store.DeleteDeletedUsers(ctx, now.Add(-retention)); err != nil {
		return res, dbError("purge deleted accounts", err)
	}
	if res.Users > 0 {
		s.audit(ctx, auditEvent{Action: "auth.accounts.purged", ActorKind: "system", ActorID: "auth_cleanup", ResourceType: "user", Metadata: map[string]any{"count": res.Users}})
	}
	return res, nil
}

// unverifiedBatch is how many unverified accounts one statement deletes, and
// unverifiedBatches how many statements one cleanup runs; the next run
// continues.
const (
	unverifiedBatch   = 500
	unverifiedBatches = 20
)

// expireUnverified soft-deletes accounts whose address stayed unverified
// past auth.unverified_account_ttl, runs the AccountDeleted hook for each
// and records one audit event with the count. Such accounts can't sign in,
// so they own nothing another module must check first.
func (s *Service) expireUnverified(ctx context.Context, now time.Time) (int64, error) {
	before := now.Add(-authlib.UnverifiedAccountLimits.Clamp(s.unverifiedTTL.Get(ctx)))
	var total int64
	for range unverifiedBatches {
		ids, err := s.store.MarkUnverifiedUsersDeleted(ctx, before, now, unverifiedBatch)
		if err != nil {
			return total, dbError("delete unverified accounts", err)
		}
		for _, id := range ids {
			s.accountDeleted(ctx, id)
		}
		total += int64(len(ids))
		if len(ids) < unverifiedBatch {
			break
		}
	}
	if total > 0 {
		s.audit(ctx, auditEvent{Action: "auth.accounts.unverified_expired", ActorKind: "system", ActorID: "auth_cleanup", ResourceType: "user", Metadata: map[string]any{"count": total}})
	}
	return total, nil
}

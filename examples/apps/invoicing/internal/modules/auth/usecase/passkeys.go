package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorbital.dev/audit"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/passkey"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// PasskeyCeremony is a started passkey ceremony, for the client.
type PasskeyCeremony struct {
	// Token identifies the ceremony when it finishes. It is shown once; only
	// its hash is stored.
	Token string
	// Options are for navigator.credentials.create or get.
	Options   json.RawMessage
	ExpiresAt time.Time
}

// PasskeyRegistration is a new passkey.
type PasskeyRegistration struct {
	Passkey authdomain.Passkey
	// RecoveryCodes are set when the passkey is the account's first second
	// factor. Show them once.
	RecoveryCodes []string
}

// BeginPasskeyRegistration starts adding a passkey for the signed-in user.
// It checks the user as confirmUser does. It returns ErrInvalidCredentials,
// ErrInvalidMFA, ErrPasskeyLimitReached, ErrPasskeysUnavailable or a
// *RateLimitError when the user's re-authentication budget is spent.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, password string) (PasskeyCeremony, error) {
	p, err := s.reauthPrincipal(ctx)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	if s.passkeys == nil {
		return PasskeyCeremony{}, authdomain.ErrPasskeysUnavailable
	}
	var (
		out   PasskeyCeremony
		state error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if err != nil {
			return err // ErrUserNotFound is handled below
		}
		if state, err = s.confirmUser(ctx, tx, p, u, password); err != nil || state != nil {
			return err
		}
		existing, err := tx.SelectPasskeys(ctx, u.ID)
		if err != nil {
			return err
		}
		if len(existing) >= authdomain.MaxPasskeys {
			state = authdomain.ErrPasskeyLimitReached
			return nil
		}
		handle, err := tx.SetWebAuthnUserHandle(ctx, u.ID, passkey.NewUserHandle())
		if err != nil {
			return err
		}
		c, err := s.passkeys.BeginRegistration(passkeyUser(u, handle, existing))
		if err != nil {
			return err
		}
		out, err = s.startCeremony(ctx, tx, authdomain.CeremonyRegister, u.ID, "", c)
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return PasskeyCeremony{}, authlib.ErrUnauthenticated
	case err != nil:
		return PasskeyCeremony{}, dbError("start passkey registration", err)
	case state != nil:
		return PasskeyCeremony{}, s.reauthFailed(ctx, p.UserID, state)
	}
	return out, nil
}

// FinishPasskeyRegistration verifies the client's response to a registration
// the signed-in user started, and stores the passkey. The first second factor
// of an account also creates its recovery codes and ends the user's other
// sessions, as turning on the authenticator app does. It marks the session
// verified with a second factor. It returns ErrInvalidPasskey (including a
// used or expired ceremony), ErrInvalidPasskeyName, ErrPasskeyLimitReached
// or ErrPasskeysUnavailable.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, ceremonyToken, name string, credential []byte) (PasskeyRegistration, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return PasskeyRegistration{}, err
	}
	if s.passkeys == nil {
		return PasskeyRegistration{}, authdomain.ErrPasskeysUnavailable
	}
	if name, err = authdomain.PasskeyName(name); err != nil {
		return PasskeyRegistration{}, err
	}
	var (
		out   PasskeyRegistration
		to    string
		state error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		c, ok, err := s.takeCeremony(ctx, tx, ceremonyToken, authdomain.CeremonyRegister)
		if err != nil {
			return err
		}
		if !ok || c.UserID != p.UserID {
			state = authdomain.ErrInvalidPasskey
			return nil // commit the used ceremony
		}
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if err != nil {
			return err
		}
		existing, err := tx.SelectPasskeys(ctx, u.ID)
		if err != nil {
			return err
		}
		if len(existing) >= authdomain.MaxPasskeys {
			state = authdomain.ErrPasskeyLimitReached
			return nil
		}
		cred, err := s.passkeys.FinishRegistration(passkeyUser(u, u.WebAuthnUserHandle, existing), c.SessionData, credential)
		if errors.Is(err, passkey.ErrInvalidResponse) {
			state = authdomain.ErrInvalidPasskey
			return nil
		}
		if err != nil {
			return err
		}
		hasFactor, err := s.hasSecondFactor(ctx, tx, u.ID)
		if err != nil {
			return err
		}

		now := s.now()
		out.Passkey = authdomain.Passkey{
			ID: authlib.NewID("pky"), UserID: u.ID, CredentialID: cred.ID, Record: cred.Record, Name: name, AAGUID: cred.AAGUID,
			BackupEligible: cred.BackupEligible, BackupState: cred.BackupState, SignCount: int64(cred.SignCount), CreatedAt: now,
		}
		if err := tx.InsertPasskey(ctx, out.Passkey); err != nil {
			return err
		}
		if !hasFactor {
			out.RecoveryCodes = authlib.NewRecoveryCodes()
			if err := s.replaceRecoveryCodes(ctx, tx, u.ID, out.RecoveryCodes, now); err != nil {
				return err
			}
			if _, err := tx.RevokeUserSessions(ctx, u.ID, p.SessionID, now, "mfa_enabled"); err != nil {
				return err
			}
		}
		to = u.Email
		return tx.MarkSessionMFAVerified(ctx, p.SessionID, now)
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return PasskeyRegistration{}, authlib.ErrUnauthenticated
	case err != nil:
		return PasskeyRegistration{}, dbError("register passkey", err)
	case state != nil:
		return PasskeyRegistration{}, state
	}
	s.sent(ctx, "passkey_added", s.emails.SendPasskeyAdded(ctx, to, name))
	e := userEvent("auth.passkey.registered", p.UserID, authlib.ClientInfoFromContext(ctx))
	e.Metadata = map[string]any{"passkey_id": out.Passkey.ID, "backed_up": out.Passkey.BackupState}
	s.audit(ctx, e)
	return out, nil
}

// ListPasskeys returns the signed-in user's passkeys, oldest first.
func (s *Service) ListPasskeys(ctx context.Context) ([]authdomain.Passkey, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	pks, err := s.store.SelectPasskeys(ctx, p.UserID)
	if err != nil {
		return nil, dbError("list passkeys", err)
	}
	return pks, nil
}

// RenamePasskey renames one of the signed-in user's passkeys. It returns
// ErrPasskeyNotFound or ErrInvalidPasskeyName.
func (s *Service) RenamePasskey(ctx context.Context, id, name string) error {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return err
	}
	if name, err = authdomain.PasskeyName(name); err != nil {
		return err
	}
	ok, err := s.store.RenamePasskey(ctx, id, p.UserID, name)
	switch {
	case err != nil:
		return dbError("rename passkey", err)
	case !ok:
		return authdomain.ErrPasskeyNotFound
	}
	e := userEvent("auth.passkey.renamed", p.UserID, authlib.ClientInfoFromContext(ctx))
	e.Metadata = map[string]any{"passkey_id": id}
	s.audit(ctx, e)
	return nil
}

// RemovePasskey removes one of the signed-in user's passkeys. It checks the
// user as confirmUser does, and refuses to remove the last second factor
// while a role requires one. Removing the last second factor also deletes
// the recovery codes. It returns ErrInvalidCredentials, ErrInvalidMFA,
// ErrPasskeyNotFound, ErrMFARequiredByRole or a *RateLimitError.
func (s *Service) RemovePasskey(ctx context.Context, id, password string) error {
	p, err := s.reauthPrincipal(ctx)
	if err != nil {
		return err
	}
	var (
		to, name string
		state    error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if err != nil {
			return err // ErrUserNotFound is handled below
		}
		if state, err = s.confirmUser(ctx, tx, p, u, password); err != nil || state != nil {
			return err
		}
		existing, err := tx.SelectPasskeys(ctx, u.ID)
		if err != nil {
			return err
		}
		i := indexOfPasskey(existing, id)
		if i < 0 {
			state = authdomain.ErrPasskeyNotFound
			return nil
		}
		totp, found, err := tx.SelectTOTP(ctx, u.ID, false)
		if err != nil {
			return err
		}
		roles, err := tx.SelectUserRoles(ctx, u.ID)
		if err != nil {
			return err
		}
		otherFactor := len(existing) > 1 || (found && totp.Confirmed())
		if !otherFactor && s.catalog.RequiresMFA(roles...) {
			state = authdomain.ErrMFARequiredByRole
			return nil
		}
		if _, err := tx.DeletePasskey(ctx, id, u.ID); err != nil {
			return err
		}
		if !otherFactor {
			if err := tx.DeleteRecoveryCodes(ctx, u.ID); err != nil {
				return err
			}
		}
		to, name = u.Email, existing[i].Name
		return nil
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return authlib.ErrUnauthenticated
	case err != nil:
		return dbError("remove passkey", err)
	case state != nil:
		return s.reauthFailed(ctx, p.UserID, state)
	}
	s.sent(ctx, "passkey_removed", s.emails.SendPasskeyRemoved(ctx, to, name))
	e := userEvent("auth.passkey.removed", p.UserID, authlib.ClientInfoFromContext(ctx))
	e.Metadata = map[string]any{"passkey_id": id}
	s.audit(ctx, e)
	return nil
}

// BeginPasskeyLogin starts a passwordless sign-in. It returns
// ErrPasskeysUnavailable.
func (s *Service) BeginPasskeyLogin(ctx context.Context) (PasskeyCeremony, error) {
	if s.passkeys == nil {
		return PasskeyCeremony{}, authdomain.ErrPasskeysUnavailable
	}
	c, err := s.passkeys.BeginDiscoverableLogin()
	if err != nil {
		return PasskeyCeremony{}, err
	}
	var out PasskeyCeremony
	err = s.store.InTx(ctx, func(tx Store) error {
		var err error
		out, err = s.startCeremony(ctx, tx, authdomain.CeremonyLogin, "", "", c)
		return err
	})
	if err != nil {
		return PasskeyCeremony{}, dbError("start passkey sign-in", err)
	}
	return out, nil
}

// FinishPasskeyLogin verifies a passkey's response to a passwordless sign-in
// and starts a session verified with a second factor. An unknown passkey, a
// failed verification, a used or expired ceremony, a counter that didn't
// increase and an unverified email address all return ErrInvalidPasskey.
func (s *Service) FinishPasskeyLogin(ctx context.Context, ceremonyToken string, credential []byte) (LoginResult, error) {
	client := authlib.ClientInfoFromContext(ctx)
	if s.passkeys == nil {
		return LoginResult{}, authdomain.ErrPasskeysUnavailable
	}
	var (
		res            LoginResult
		user           authdomain.User
		stored         authdomain.Passkey
		clone, invalid bool
	)
	err := s.store.InTx(ctx, func(tx Store) error {
		c, ok, err := s.takeCeremony(ctx, tx, ceremonyToken, authdomain.CeremonyLogin)
		if err != nil || !ok {
			invalid = !ok
			return err
		}
		_, cred, err := s.passkeys.FinishLogin(c.SessionData, credential, func(handle, credentialID []byte) (passkey.User, error) {
			u, err := tx.SelectUserByWebAuthnHandle(ctx, handle)
			if errors.Is(err, authdomain.ErrUserNotFound) {
				return passkey.User{}, passkey.ErrUnknownCredential
			}
			if err != nil {
				return passkey.User{}, err
			}
			pk, found, err := tx.SelectPasskeyByCredentialID(ctx, credentialID)
			if err != nil {
				return passkey.User{}, err
			}
			if !found || pk.UserID != u.ID {
				return passkey.User{}, passkey.ErrUnknownCredential
			}
			all, err := tx.SelectPasskeys(ctx, u.ID)
			if err != nil {
				return passkey.User{}, err
			}
			user, stored = u, pk
			return passkeyUser(u, u.WebAuthnUserHandle, all), nil
		})
		switch {
		case errors.Is(err, passkey.ErrCloneWarning):
			clone, invalid = true, true
			return nil
		case errors.Is(err, passkey.ErrInvalidResponse):
			invalid = true
			return nil
		case err != nil:
			return err
		case !user.EmailVerified():
			invalid = true
			return nil
		}
		if err := tx.UpdatePasskeyUse(ctx, stored.ID, cred.Record, int64(cred.SignCount), cred.BackupState, s.now()); err != nil {
			return err
		}
		res, err = s.startSignIn(ctx, tx, user, true, client, authdomain.MethodPasskey, "")
		return err
	})
	if err != nil {
		return LoginResult{}, s.signInFailed(ctx, "passkey sign-in", user.ID, authdomain.MethodPasskey, err, client)
	}
	if clone {
		s.cloneWarning(ctx, user.ID, stored.ID)
	}
	if invalid {
		s.loginFailed(ctx, user.ID, "invalid_passkey", client)
		return LoginResult{}, authdomain.ErrInvalidPasskey
	}
	s.loginSucceeded(ctx, res, authdomain.MethodPasskey, authdomain.MFAMethodPasskey)
	s.afterLogin(ctx, res, authdomain.MethodPasskey, "")
	return res, nil
}

// BeginPasskeySecondFactor starts a passkey ceremony to finish a sign-in that
// Login answered with a challenge, limited to the account's passkeys. Finish
// it with LoginMFA. An unknown, used or expired challenge, or an account
// without passkeys, returns ErrInvalidMFA.
func (s *Service) BeginPasskeySecondFactor(ctx context.Context, challengeToken string) (PasskeyCeremony, error) {
	if s.passkeys == nil {
		return PasskeyCeremony{}, authdomain.ErrPasskeysUnavailable
	}
	if challengeToken == "" || len(challengeToken) > 256 {
		return PasskeyCeremony{}, authdomain.ErrInvalidMFA
	}
	var (
		out     PasskeyCeremony
		invalid bool
	)
	err := s.store.InTx(ctx, func(tx Store) error {
		ch, found, err := tx.SelectMFAChallengeByTokenHash(ctx, authlib.HashToken(challengeToken))
		if err != nil || !found || !ch.UsableAt(s.now()) {
			invalid = err == nil
			return err
		}
		u, err := tx.SelectUserByID(ctx, ch.UserID, false)
		if err != nil {
			return err // ErrUserNotFound is handled below
		}
		pks, err := tx.SelectPasskeys(ctx, u.ID)
		if err != nil || len(pks) == 0 {
			invalid = err == nil
			return err
		}
		c, err := s.passkeys.BeginUserLogin(passkeyUser(u, u.WebAuthnUserHandle, pks))
		if err != nil {
			return err
		}
		out, err = s.startCeremony(ctx, tx, authdomain.CeremonySecondFactor, u.ID, ch.ID, c)
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound), err == nil && invalid:
		return PasskeyCeremony{}, authdomain.ErrInvalidMFA
	case err != nil:
		return PasskeyCeremony{}, dbError("start passkey second factor", err)
	}
	return out, nil
}

// BeginPasskeyVerification starts a passkey ceremony limited to the signed-in
// user's passkeys, to confirm a sensitive change: deleting the account,
// turning off the authenticator app or replacing recovery codes take its
// response as their second factor. It returns ErrMFANotEnabled for an
// account without passkeys, or ErrPasskeysUnavailable.
func (s *Service) BeginPasskeyVerification(ctx context.Context) (PasskeyCeremony, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	if s.passkeys == nil {
		return PasskeyCeremony{}, authdomain.ErrPasskeysUnavailable
	}
	var (
		out   PasskeyCeremony
		state error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, false)
		if err != nil {
			return err // ErrUserNotFound is handled below
		}
		pks, err := tx.SelectPasskeys(ctx, u.ID)
		if err != nil {
			return err
		}
		if len(pks) == 0 {
			state = authdomain.ErrMFANotEnabled
			return nil
		}
		c, err := s.passkeys.BeginUserLogin(passkeyUser(u, u.WebAuthnUserHandle, pks))
		if err != nil {
			return err
		}
		out, err = s.startCeremony(ctx, tx, authdomain.CeremonyReauth, u.ID, "", c)
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return PasskeyCeremony{}, authlib.ErrUnauthenticated
	case err != nil:
		return PasskeyCeremony{}, dbError("start passkey verification", err)
	case state != nil:
		return PasskeyCeremony{}, state
	}
	return out, nil
}

// confirmUser checks that the person using the session p is the account's
// owner before a change to its sign-in methods (ADR-0044). A session that
// verified a second factor within auth.RecentVerification needs nothing
// more. Otherwise the password is required (for an account without one, a
// recent sign-in), and an account with two-factor authentication on also
// needs a session verified with a second factor. It returns
// ErrInvalidCredentials or ErrInvalidMFA as state.
func (s *Service) confirmUser(ctx context.Context, tx Store, p authlib.Principal, u authdomain.User, password string) (state, err error) {
	if p.RecentlyVerified(s.now()) {
		return state, nil
	}
	switch ok, err := s.passwordOrRecentSignIn(ctx, p, u, password); {
	case err != nil:
		return state, err
	case !ok:
		return authdomain.ErrInvalidCredentials, nil
	case p.MFAVerified:
		return state, nil
	}
	has, err := s.hasSecondFactor(ctx, tx, u.ID)
	if err != nil || !has {
		return state, err
	}
	return authdomain.ErrInvalidMFA, nil
}

// checkPasskeyFactor verifies a passkey's response to a second-factor
// ceremony of the sign-in challenge challengeID or, without a challenge, to
// a verification ceremony of the signed-in user, and updates the passkey.
func (s *Service) checkPasskeyFactor(ctx context.Context, tx Store, userID, challengeID string, a authdomain.PasskeyAssertion) (method string, remaining int, valid bool, err error) {
	method = authdomain.MFAMethodPasskey
	if s.passkeys == nil {
		return method, 0, false, nil
	}
	purpose := authdomain.CeremonySecondFactor
	if challengeID == "" {
		purpose = authdomain.CeremonyReauth
	}
	c, ok, err := s.takeCeremony(ctx, tx, a.CeremonyToken, purpose)
	if err != nil || !ok || c.UserID != userID || c.MFAChallengeID != challengeID {
		return method, 0, false, err
	}
	u, err := tx.SelectUserByID(ctx, userID, false)
	if err != nil {
		return method, 0, false, err
	}
	pks, err := tx.SelectPasskeys(ctx, userID)
	if err != nil {
		return method, 0, false, err
	}
	var stored authdomain.Passkey
	_, cred, err := s.passkeys.FinishLogin(c.SessionData, a.Credential, func(handle, credentialID []byte) (passkey.User, error) {
		if !bytes.Equal(handle, u.WebAuthnUserHandle) {
			return passkey.User{}, passkey.ErrUnknownCredential
		}
		if i := indexOfCredential(pks, credentialID); i >= 0 {
			stored = pks[i]
			return passkeyUser(u, handle, pks), nil
		}
		return passkey.User{}, passkey.ErrUnknownCredential
	})
	switch {
	case errors.Is(err, passkey.ErrCloneWarning):
		s.cloneWarning(ctx, userID, stored.ID)
		return method, 0, false, nil
	case errors.Is(err, passkey.ErrInvalidResponse):
		return method, 0, false, nil
	case err != nil:
		return method, 0, false, err
	}
	return method, 0, true, tx.UpdatePasskeyUse(ctx, stored.ID, cred.Record, int64(cred.SignCount), cred.BackupState, s.now())
}

// startCeremony stores a started ceremony and returns its token and options.
func (s *Service) startCeremony(ctx context.Context, tx Store, purpose, userID, challengeID string, c passkey.Ceremony) (PasskeyCeremony, error) {
	now := s.now()
	token, tokenHash := authlib.NewToken()
	expires := now.Add(passkey.CeremonyTTL)
	err := tx.InsertWebAuthnCeremony(ctx, authdomain.WebAuthnCeremony{
		ID: authlib.NewID("wac"), TokenHash: tokenHash, UserID: userID, Purpose: purpose, MFAChallengeID: challengeID,
		SessionData: c.State, ExpiresAt: expires, CreatedAt: now,
	})
	return PasskeyCeremony{Token: token, Options: c.Options, ExpiresAt: expires}, err
}

// takeCeremony locks and uses up the ceremony with token, and reports whether
// it could still be finished for purpose. Callers commit the transaction even
// when it can't, so a ceremony is never used twice.
func (s *Service) takeCeremony(ctx context.Context, tx Store, token, purpose string) (authdomain.WebAuthnCeremony, bool, error) {
	if token == "" || len(token) > 256 {
		return authdomain.WebAuthnCeremony{}, false, nil
	}
	c, found, err := tx.SelectWebAuthnCeremonyByTokenHash(ctx, authlib.HashToken(token))
	if err != nil || !found {
		return c, false, err
	}
	usable := c.UsableAt(s.now()) && c.Purpose == purpose
	if c.ConsumedAt == nil {
		if err := tx.ConsumeWebAuthnCeremony(ctx, c.ID, s.now()); err != nil {
			return c, false, err
		}
	}
	return c, usable, nil
}

// cloneWarning records a passkey whose signature counter didn't increase.
func (s *Service) cloneWarning(ctx context.Context, userID, passkeyID string) {
	s.logger.WarnContext(ctx, "passkey signature counter didn't increase; it may have been copied", "user_id", userID, "passkey_id", passkeyID)
	e := userEvent("auth.passkey.clone_warning", userID, authlib.ClientInfoFromContext(ctx))
	e.Outcome, e.Metadata = audit.OutcomeFailure, map[string]any{"passkey_id": passkeyID}
	s.audit(ctx, e)
}

// passkeyUser is u as the passkey library sees it.
func passkeyUser(u authdomain.User, handle []byte, pks []authdomain.Passkey) passkey.User {
	creds := make([]passkey.Credential, len(pks))
	for i, pk := range pks {
		creds[i] = passkey.Credential{ID: pk.CredentialID, Record: pk.Record}
	}
	return passkey.User{Handle: handle, Name: u.Email, DisplayName: u.Email, Credentials: creds}
}

func indexOfPasskey(pks []authdomain.Passkey, id string) int {
	for i, pk := range pks {
		if pk.ID == id {
			return i
		}
	}
	return -1
}

func indexOfCredential(pks []authdomain.Passkey, credentialID []byte) int {
	for i, pk := range pks {
		if bytes.Equal(pk.CredentialID, credentialID) {
			return i
		}
	}
	return -1
}

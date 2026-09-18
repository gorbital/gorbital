package usecase

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/social"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// SocialStart is a started web sign-in with Google, Apple or GitHub, or a
// started link of GitHub to the signed-in user.
type SocialStart struct {
	// URL is the provider page to send the browser to.
	URL string
	// BrowserToken goes in the browser's __Host-oauth cookie; only its hash is
	// stored, and the callback requires it.
	BrowserToken string
	ExpiresAt    time.Time
}

// SocialResult is a finished web sign-in: a session or a second-factor
// challenge, and the address to send the browser back to. ReturnTo is set
// even when the sign-in fails, once its state is known.
type SocialResult struct {
	LoginResult
	ReturnTo string
	// Linked reports a finished link started with StartIdentityLink: the
	// provider was linked to the account that started it, and no one was
	// signed in.
	Linked bool
}

// StartSocialSignIn starts a web sign-in with provider that returns to
// returnTo (DefaultReturnTo when empty). It returns ErrSocialUnavailable or
// ErrInvalidReturnTo.
func (s *Service) StartSocialSignIn(ctx context.Context, provider, returnTo string) (SocialStart, error) {
	p := s.providers[provider]
	if p == nil || !p.Web() {
		return SocialStart{}, authdomain.ErrSocialUnavailable
	}
	returnTo, err := s.checkReturnTo(returnTo)
	if err != nil {
		return SocialStart{}, err
	}
	return s.startWeb(ctx, p, returnTo, authlib.Principal{})
}

// StartIdentityLink starts linking GitHub to the signed-in user through the
// web flow, since GitHub has no ID token for LinkIdentity (ADR-0059). It
// checks the user as confirmUser does first. The flow is bound twice: to the
// browser, by BrowserToken in the __Host-oauth cookie of this response, as
// for a sign-in, so a stranger's callback link can't finish it; and to this
// session, which must still be active when GitHub returns, so signing out
// or ending the session cancels it. The callback links the GitHub account to
// this user and signs no one in. It returns ErrSocialUnavailable,
// ErrInvalidReturnTo, ErrInvalidCredentials, ErrInvalidMFA or a
// *RateLimitError.
func (s *Service) StartIdentityLink(ctx context.Context, provider, returnTo, password string) (SocialStart, error) {
	p, err := s.reauthPrincipal(ctx)
	if err != nil {
		return SocialStart{}, err
	}
	pr := s.providers[provider]
	if pr == nil || !pr.Web() || provider != social.GitHub {
		return SocialStart{}, authdomain.ErrSocialUnavailable
	}
	if returnTo, err = s.checkReturnTo(returnTo); err != nil {
		return SocialStart{}, err
	}
	u, err := s.store.SelectUserByID(ctx, p.UserID, false)
	if errors.Is(err, authdomain.ErrUserNotFound) || p.SessionID == "" {
		return SocialStart{}, authlib.ErrUnauthenticated
	}
	if err != nil {
		return SocialStart{}, dbError("link sign-in method", err)
	}
	state, err := s.confirmUser(ctx, s.store, p, u, password)
	if err != nil {
		return SocialStart{}, dbError("link sign-in method", err)
	}
	if state != nil {
		return SocialStart{}, s.reauthFailed(ctx, p.UserID, state)
	}
	return s.startWeb(ctx, pr, returnTo, p)
}

// startWeb stores a web flow's state and returns the provider URL: a sign-in,
// or a link to the account of linkTo's session when it has one.
func (s *Service) startWeb(ctx context.Context, p *social.Provider, returnTo string, linkTo authlib.Principal) (SocialStart, error) {
	provider := p.Name()
	now := s.now()
	state, stateHash := authlib.NewToken()
	browser, browserHash := authlib.NewToken()
	nonce, _ := authlib.NewToken()
	verifier := social.NewPKCEVerifier()
	st := authdomain.OAuthState{
		ID: authlib.NewID("oas"), TokenHash: stateHash, BrowserHash: browserHash, Provider: provider, Nonce: nonce, Verifier: verifier,
		ReturnTo: returnTo, LinkUserID: linkTo.UserID, LinkSessionID: linkTo.SessionID, ExpiresAt: now.Add(authdomain.OAuthStateTTL), CreatedAt: now,
	}
	if err := s.store.InsertOAuthState(ctx, st); err != nil {
		return SocialStart{}, dbError("start sign-in", err)
	}
	return SocialStart{URL: p.AuthCodeURL(s.callbackURL(provider), state, nonce, verifier), BrowserToken: browser, ExpiresAt: st.ExpiresAt}, nil
}

// FinishSocialSignIn finishes a web sign-in the provider sent back with code
// and stateToken, in the browser holding browserToken. name is Apple's
// first-time name, when sent. Like Login, it returns a session, or a
// challenge for an account with two-factor authentication (ADR-0046). A flow
// started with StartIdentityLink links instead (Linked) and also returns
// ErrUnauthenticated when its session has ended, or ErrIdentityInUse. It
// returns ErrSocialUnavailable, ErrInvalidState, ErrInvalidSocialToken,
// ErrSocialEmailUnverified, ErrSocialLinkRequired or ErrMFAUnavailable.
func (s *Service) FinishSocialSignIn(ctx context.Context, provider, stateToken, browserToken, code, name string) (SocialResult, error) {
	res := SocialResult{ReturnTo: s.defaultReturnTo}
	p := s.providers[provider]
	if p == nil || !p.Web() {
		return res, authdomain.ErrSocialUnavailable
	}
	client := authlib.ClientInfoFromContext(ctx)
	if stateToken == "" || len(stateToken) > 256 || len(browserToken) > 256 {
		s.loginFailed(ctx, "", "invalid_state", client)
		return res, authdomain.ErrInvalidState
	}
	var (
		st    authdomain.OAuthState
		valid bool
	)
	err := s.store.InTx(ctx, func(tx Store) error {
		var (
			found bool
			err   error
		)
		st, found, err = tx.SelectOAuthStateByTokenHash(ctx, authlib.HashToken(stateToken))
		if err != nil || !found {
			return err
		}
		res.ReturnTo = st.ReturnTo
		valid = st.UsableAt(s.now()) && st.Provider == provider && browserToken != "" &&
			subtle.ConstantTimeCompare(st.BrowserHash, authlib.HashToken(browserToken)) == 1
		if st.ConsumedAt != nil {
			return nil
		}
		return tx.ConsumeOAuthState(ctx, st.ID, s.now()) // used up even when invalid
	})
	switch {
	case err != nil:
		return res, dbError("finish sign-in", err)
	case !valid:
		s.loginFailed(ctx, "", "invalid_state", client)
		return res, authdomain.ErrInvalidState
	case code == "":
		s.loginFailed(ctx, "", "invalid_social_token", client)
		return res, authdomain.ErrInvalidSocialToken
	}
	tok, err := p.Exchange(ctx, s.callbackURL(provider), code, st.Verifier, st.Nonce)
	if err != nil {
		return res, s.socialFailed(ctx, provider, err)
	}
	if tok.Identity.Name == "" {
		tok.Identity.Name = name
	}
	if st.LinkUserID != "" {
		res.Linked, err = true, s.finishLink(ctx, st, tok.Identity, tok.RefreshToken)
		return res, err
	}
	res.LoginResult, err = s.signInWithIdentity(ctx, tok.Identity, tok.RefreshToken)
	return res, err
}

// finishLink links the identity of a web flow started with
// StartIdentityLink to the account that started it, provided the session
// that started it is still active.
func (s *Service) finishLink(ctx context.Context, st authdomain.OAuthState, id social.Identity, refreshToken string) error {
	identity, err := s.newIdentity(ctx, id, refreshToken)
	if err != nil {
		return err
	}
	_, _, err = s.addIdentity(ctx, st.LinkUserID, identity, "web", func(tx Store) error {
		sessions, err := tx.SelectActiveSessions(ctx, st.LinkUserID, s.now())
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(sessions, func(ses authdomain.Session) bool { return ses.ID == st.LinkSessionID }) {
			return authlib.ErrUnauthenticated
		}
		return nil
	})
	if err != nil && !errors.Is(err, authlib.ErrUnauthenticated) && !errors.Is(err, authdomain.ErrIdentityInUse) {
		return dbError("link sign-in method", err)
	}
	return err
}

// SocialNonce returns a single-use nonce for a native app to put in its
// Google or Apple sign-in request, valid for 5 minutes. Apple's iOS SDK takes
// its SHA-256 in hex. It returns ErrSocialUnavailable.
func (s *Service) SocialNonce(ctx context.Context, provider string) (string, time.Time, error) {
	if s.providers[provider] == nil || provider == social.GitHub {
		return "", time.Time{}, authdomain.ErrSocialUnavailable
	}
	now := s.now()
	nonce, hash := authlib.NewToken()
	n := authdomain.SocialNonce{ID: authlib.NewID("snc"), TokenHash: hash, Provider: provider, ExpiresAt: now.Add(authdomain.SocialNonceTTL), CreatedAt: now}
	if err := s.store.InsertSocialNonce(ctx, n); err != nil {
		return "", time.Time{}, dbError("create sign-in nonce", err)
	}
	return nonce, n.ExpiresAt, nil
}

// SignInWithIDToken signs in with an ID token a native app got from Google's
// or Apple's SDK, requested with a nonce from SocialNonce. For Apple,
// authorizationCode (when sent) is exchanged for a refresh token kept to
// revoke later, and name is the first-time name. Like Login, it returns a
// session or a challenge. It returns ErrSocialUnavailable,
// ErrInvalidSocialToken (including a used or expired nonce),
// ErrSocialEmailUnverified or ErrMFAUnavailable.
func (s *Service) SignInWithIDToken(ctx context.Context, provider, idToken, nonce, authorizationCode, name string) (LoginResult, error) {
	p := s.providers[provider]
	if p == nil || provider == social.GitHub {
		return LoginResult{}, authdomain.ErrSocialUnavailable
	}
	client := authlib.ClientInfoFromContext(ctx)
	if idToken == "" || len(idToken) > 16_384 || nonce == "" || len(nonce) > 256 {
		s.loginFailed(ctx, "", "invalid_social_token", client)
		return LoginResult{}, authdomain.ErrInvalidSocialToken
	}
	used, err := s.store.UseSocialNonce(ctx, provider, authlib.HashToken(nonce), s.now())
	if err != nil {
		return LoginResult{}, dbError("sign in", err)
	}
	if !used {
		s.loginFailed(ctx, "", "invalid_nonce", client)
		return LoginResult{}, authdomain.ErrInvalidSocialToken
	}
	expected := nonce
	if provider == social.Apple {
		sum := sha256.Sum256([]byte(nonce))
		expected = hex.EncodeToString(sum[:])
	}
	id, err := p.VerifyIDToken(ctx, idToken, expected)
	if err != nil {
		return LoginResult{}, s.socialFailed(ctx, provider, err)
	}
	if id.Name == "" {
		id.Name = name
	}
	var refresh string
	if provider == social.Apple && authorizationCode != "" {
		if refresh, err = p.ExchangeNativeCode(ctx, authorizationCode, id.Audience); err != nil {
			return LoginResult{}, s.socialFailed(ctx, provider, err)
		}
	}
	return s.signInWithIdentity(ctx, id, refresh)
}

// newIdentity returns the identity to store for id, with Apple's refresh
// token encrypted when there is one.
func (s *Service) newIdentity(ctx context.Context, id social.Identity, refreshToken string) (authdomain.Identity, error) {
	identity := authdomain.Identity{
		ID: authlib.NewID("idn"), Provider: id.Provider, Subject: id.Subject, Email: id.Email, PrivateEmail: id.PrivateEmail, Name: id.Name,
	}
	if refreshToken == "" {
		return identity, nil
	}
	if s.keyring == nil {
		s.logger.WarnContext(ctx, "Apple's refresh token isn't kept without AUTH_ENCRYPTION_KEYS, so it can't be revoked when the account is deleted")
		return identity, nil
	}
	keyID, ciphertext, err := s.keyring.Encrypt([]byte(refreshToken), identityAAD(id.Provider, id.Subject))
	if err != nil {
		return authdomain.Identity{}, err
	}
	identity.RefreshKeyID, identity.RefreshTokenCiphertext, identity.RefreshClientID = keyID, ciphertext, id.Audience
	return identity, nil
}

// signInWithIdentity finds or creates the account of a verified identity,
// then starts a session or a second-factor challenge. An unknown identity
// with a provider-verified email creates an account when the address has
// none; the account's address counts as verified only when the provider is
// authoritative for it. It links to the address's existing account only when the provider is
// authoritative for the address (social.Identity.AuthoritativeEmail):
// otherwise the address may have changed hands since the provider verified
// it, and the account's owner links the provider with LinkIdentity instead
// (ErrSocialLinkRequired, security review AUTH-M-1). Linking an account that
// never verified its email removes its password and whatever was added
// before (claimAddress), so whoever registered the address without owning it
// loses access (ADR-0046).
func (s *Service) signInWithIdentity(ctx context.Context, id social.Identity, refreshToken string) (LoginResult, error) {
	client := authlib.ClientInfoFromContext(ctx)
	identity, err := s.newIdentity(ctx, id, refreshToken)
	if err != nil {
		return LoginResult{}, err
	}
	var (
		u               authdomain.User
		linked, created bool
	)
	now := s.now()
	link := func(tx Store) error {
		existing, found, err := tx.SelectIdentity(ctx, id.Provider, id.Subject, true)
		if err != nil {
			return err
		}
		if found {
			if u, err = tx.SelectUserByID(ctx, existing.UserID, true); err != nil {
				return err // ErrUserNotFound is handled below
			}
			email := cmpOr(id.Email, existing.Email)
			if err := tx.UpdateIdentityUse(ctx, existing.ID, email, id.PrivateEmail, now); err != nil {
				return err
			}
			if identity.RefreshTokenCiphertext != nil {
				return tx.SetIdentityRefreshToken(ctx, existing.ID, identity.RefreshKeyID, identity.RefreshTokenCiphertext, identity.RefreshClientID)
			}
			return nil
		}
		if !id.EmailVerified {
			return authdomain.ErrSocialEmailUnverified // nothing written yet
		}
		email, normalized, err := authlib.NormalizeEmail(id.Email)
		if err != nil {
			return authdomain.ErrSocialEmailUnverified
		}
		u, err = tx.SelectUserByEmail(ctx, normalized, true)
		switch {
		case errors.Is(err, authdomain.ErrUserNotFound) && s.closed:
			return authdomain.ErrRegistrationClosed // nothing written yet
		case errors.Is(err, authdomain.ErrUserNotFound):
			// Only a provider authoritative for the address proves it; otherwise
			// the account stays unverified, so whoever proves the address later
			// by email claims it and removes this identity (claimAddress).
			var verifiedAt *time.Time
			if id.AuthoritativeEmail() {
				verifiedAt = &now
			}
			u, err = tx.InsertUser(ctx, authdomain.User{ID: authlib.NewID("usr"), Email: email, NormalizedEmail: normalized, EmailVerifiedAt: verifiedAt, CreatedAt: now})
			if err != nil {
				return err
			}
			created = true
		case err != nil:
			return err
		case !id.AuthoritativeEmail():
			return authdomain.ErrSocialLinkRequired // nothing written yet
		case !u.EmailVerified():
			if err := tx.VerifyEmailRemovePassword(ctx, u.ID, now); err != nil {
				return err
			}
			if err := s.claimAddress(ctx, tx, u.ID, now, "unverified_account_linked"); err != nil {
				return err
			}
			u.EmailVerifiedAt, u.PasswordHash, linked = &now, "", true
		default:
			linked = true
		}
		identity.UserID = u.ID
		identity.CreatedAt = now
		if err := tx.InsertIdentity(ctx, identity); err != nil || !created {
			return err
		}
		return s.onRegister(ctx, tx, NewAccount{User: u, Method: id.Provider, Name: id.Name, Client: client}, nil)
	}
	// Two first sign-ins of one person can race to create the account or the
	// identity: the loser's transaction rolls back, and its retry finds what
	// the winner committed. The loser may also find the winner's account
	// before its identity (each statement sees what has committed when it
	// starts), which looks like an account to link.
	for range 2 {
		linked, created = false, false
		err = s.store.InTx(ctx, link)
		if !errors.Is(err, authdomain.ErrEmailTaken) && !errors.Is(err, authdomain.ErrIdentityTaken) && !errors.Is(err, authdomain.ErrSocialLinkRequired) {
			break
		}
	}
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		s.loginFailed(ctx, "", "account_deleted", client)
		return LoginResult{}, authdomain.ErrInvalidSocialToken
	case errors.Is(err, authdomain.ErrSocialEmailUnverified):
		s.loginFailed(ctx, "", "social_email_unverified", client)
		return LoginResult{}, err
	case errors.Is(err, authdomain.ErrRegistrationClosed):
		e := userEvent("auth.login.failed", "", client)
		e.Outcome, e.Metadata = audit.OutcomeFailure, map[string]any{"reason": "registration_closed", "provider": id.Provider}
		s.audit(ctx, e)
		return LoginResult{}, err
	case err != nil && isRefusal(err):
		r, _ := refusal(err)
		e := userEvent("auth.login.failed", "", client)
		e.Outcome, e.Metadata = audit.OutcomeFailure, map[string]any{"reason": "refused", "code": r.Code, "provider": id.Provider, "new_account": true}
		s.audit(ctx, e)
		return LoginResult{}, r
	case errors.Is(err, authdomain.ErrSocialLinkRequired):
		e := userEvent("auth.login.failed", u.ID, client)
		e.Outcome, e.Metadata = audit.OutcomeFailure, map[string]any{"reason": "social_link_required", "provider": id.Provider}
		s.audit(ctx, e)
		return LoginResult{}, err
	case err != nil:
		return LoginResult{}, dbError("sign in", err)
	}
	if created {
		e := userEvent("auth.user.registered", u.ID, client)
		e.Metadata = map[string]any{"provider": id.Provider}
		s.audit(ctx, e)
		s.accountCreated(ctx, u.ID)
	}
	if created || linked {
		e := userEvent("auth.identity.linked", u.ID, client)
		e.Metadata = map[string]any{"provider": id.Provider, "identity_id": identity.ID, "new_account": created}
		s.audit(ctx, e)
	}
	if linked {
		s.sent(ctx, "sign_in_method_added", s.emails.SendSignInMethodAdded(ctx, u.Email, providerName(id.Provider)))
	}
	return s.startSocialSession(ctx, u, id.Provider)
}

// LinkIdentity links a Google or Apple account to the signed-in user, with
// an ID token the client got from the provider's SDK (native apps) or
// library (Google Identity Services or Sign in with Apple JS in browsers),
// requested with a nonce from SocialNonce. It checks the user as confirmUser
// does before anything else. This is how an account adds a provider whose
// email address the provider isn't authoritative for (ErrSocialLinkRequired),
// or with another address. For Apple, authorizationCode (when sent) is
// exchanged for a refresh token kept to revoke later, and name is the
// first-time name. Linking an identity the user already has changes nothing
// and reports added false. It returns ErrSocialUnavailable, ErrInvalidSocialToken (including a used
// or expired nonce), ErrInvalidCredentials, ErrInvalidMFA, ErrIdentityInUse
// or a *RateLimitError.
func (s *Service) LinkIdentity(ctx context.Context, provider, idToken, nonce, authorizationCode, name, password string) (identity authdomain.Identity, added bool, err error) {
	p, err := s.reauthPrincipal(ctx)
	if err != nil {
		return authdomain.Identity{}, false, err
	}
	pr := s.providers[provider]
	if pr == nil || provider == social.GitHub { // no ID tokens: StartIdentityLink
		return authdomain.Identity{}, false, authdomain.ErrSocialUnavailable
	}
	// Confirm the user first, so a wrong password neither uses up the nonce
	// nor fetches a refresh token from Apple.
	u, err := s.store.SelectUserByID(ctx, p.UserID, false)
	if errors.Is(err, authdomain.ErrUserNotFound) {
		return authdomain.Identity{}, false, authlib.ErrUnauthenticated
	}
	if err != nil {
		return authdomain.Identity{}, false, dbError("link sign-in method", err)
	}
	state, err := s.confirmUser(ctx, s.store, p, u, password)
	if err != nil {
		return authdomain.Identity{}, false, dbError("link sign-in method", err)
	}
	if state != nil {
		return authdomain.Identity{}, false, s.reauthFailed(ctx, p.UserID, state)
	}

	if idToken == "" || len(idToken) > 16_384 || nonce == "" || len(nonce) > 256 {
		return authdomain.Identity{}, false, authdomain.ErrInvalidSocialToken
	}
	used, err := s.store.UseSocialNonce(ctx, provider, authlib.HashToken(nonce), s.now())
	if err != nil {
		return authdomain.Identity{}, false, dbError("link sign-in method", err)
	}
	if !used {
		return authdomain.Identity{}, false, authdomain.ErrInvalidSocialToken
	}
	expected := nonce
	if provider == social.Apple {
		sum := sha256.Sum256([]byte(nonce))
		expected = hex.EncodeToString(sum[:])
	}
	id, err := pr.VerifyIDToken(ctx, idToken, expected)
	if err != nil {
		return authdomain.Identity{}, false, s.socialFailed(ctx, provider, err)
	}
	if id.Name == "" {
		id.Name = name
	}
	var refresh string
	if provider == social.Apple && authorizationCode != "" {
		if refresh, err = pr.ExchangeNativeCode(ctx, authorizationCode, id.Audience); err != nil {
			return authdomain.Identity{}, false, s.socialFailed(ctx, provider, err)
		}
	}
	identity, err = s.newIdentity(ctx, id, refresh)
	if err != nil {
		return authdomain.Identity{}, false, err
	}
	identity, added, err = s.addIdentity(ctx, p.UserID, identity, "id_token", nil)
	if errors.Is(err, authlib.ErrUnauthenticated) || errors.Is(err, authdomain.ErrIdentityInUse) {
		return authdomain.Identity{}, false, err
	}
	if err != nil {
		return authdomain.Identity{}, false, dbError("link sign-in method", err)
	}
	return identity, added, nil
}

// addIdentity links identity to the account userID, after check (when set)
// passes in the same transaction, unless the account already has it, and
// emails and audits an addition. flow is how the identity was proven:
// id_token or web. It returns the stored identity without its refresh token,
// ErrUnauthenticated for a deleted account, ErrIdentityInUse, or check's
// error.
func (s *Service) addIdentity(ctx context.Context, userID string, identity authdomain.Identity, flow string, check func(tx Store) error) (authdomain.Identity, bool, error) {
	var (
		u     authdomain.User
		added bool
	)
	err := s.store.InTx(ctx, func(tx Store) error {
		var err error
		if u, err = tx.SelectUserByID(ctx, userID, true); err != nil {
			return err
		}
		if check != nil {
			if err := check(tx); err != nil {
				return err
			}
		}
		existing, found, err := tx.SelectIdentity(ctx, identity.Provider, identity.Subject, true)
		switch {
		case err != nil:
			return err
		case found && existing.UserID != userID:
			return authdomain.ErrIdentityInUse
		case found:
			identity = existing
			return nil
		}
		identity.UserID, identity.CreatedAt = userID, s.now()
		added = true
		return tx.InsertIdentity(ctx, identity)
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return authdomain.Identity{}, false, authlib.ErrUnauthenticated
	case errors.Is(err, authdomain.ErrIdentityInUse), errors.Is(err, authdomain.ErrIdentityTaken):
		return authdomain.Identity{}, false, authdomain.ErrIdentityInUse
	case err != nil:
		return authdomain.Identity{}, false, err
	}
	identity.RefreshKeyID, identity.RefreshTokenCiphertext, identity.RefreshClientID = "", nil, ""
	if !added {
		return identity, false, nil
	}
	s.sent(ctx, "sign_in_method_added", s.emails.SendSignInMethodAdded(ctx, u.Email, providerName(identity.Provider)))
	e := userEvent("auth.identity.linked", userID, authlib.ClientInfoFromContext(ctx))
	e.Metadata = map[string]any{"provider": identity.Provider, "identity_id": identity.ID, "new_account": false, "signed_in": true, "flow": flow}
	s.audit(ctx, e)
	return identity, true, nil
}

// startSocialSession starts a session for u, or a second-factor challenge
// when the account has two-factor authentication.
func (s *Service) startSocialSession(ctx context.Context, u authdomain.User, provider string) (LoginResult, error) {
	client := authlib.ClientInfoFromContext(ctx)
	methods, err := s.secondFactorMethods(ctx, u.ID)
	switch {
	case errors.Is(err, authdomain.ErrMFAUnavailable):
		s.logger.ErrorContext(ctx, "sign-in needs a second factor this server can't check: set AUTH_ENCRYPTION_KEYS or WEBAUTHN_RP_ID", "user_id", u.ID)
		return LoginResult{}, err
	case err != nil:
		return LoginResult{}, dbError("sign in", err)
	case len(methods) > 0:
		return s.startChallenge(ctx, u, "", false, client, methods, provider)
	}
	var res LoginResult
	err = s.store.InTx(ctx, func(tx Store) error {
		var err error
		res, err = s.startSignIn(ctx, tx, u, false, client, provider, "")
		return err
	})
	if err != nil {
		return LoginResult{}, s.signInFailed(ctx, "sign in", u.ID, provider, err, client)
	}
	e := userEvent("auth.login.succeeded", u.ID, client)
	e.ActorKind, e.ActorID = actor.KindUser, u.ID
	e.Metadata = map[string]any{"session_id": res.Session.ID, "method": provider}
	s.audit(ctx, e)
	s.afterLogin(ctx, res, provider, "")
	return res, nil
}

// socialFailed records a provider's refusal and returns
// ErrInvalidSocialToken; the provider's message is logged, never tokens.
func (s *Service) socialFailed(ctx context.Context, provider string, err error) error {
	if !errors.Is(err, social.ErrInvalidToken) && !errors.Is(err, social.ErrExchange) {
		return err
	}
	s.logger.WarnContext(ctx, "sign-in with provider refused", "provider", provider, "err", err)
	e := userEvent("auth.login.failed", "", authlib.ClientInfoFromContext(ctx))
	e.Outcome, e.Metadata = "failure", map[string]any{"reason": "invalid_social_token", "provider": provider}
	s.audit(ctx, e)
	return authdomain.ErrInvalidSocialToken
}

// checkReturnTo returns returnTo without a fragment when it is an absolute
// URL on an allowed origin, or DefaultReturnTo when empty.
func (s *Service) checkReturnTo(returnTo string) (string, error) {
	if returnTo == "" {
		return s.defaultReturnTo, nil
	}
	u, err := url.Parse(returnTo)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || len(returnTo) > 2048 {
		return "", authdomain.ErrInvalidReturnTo
	}
	if !slices.Contains(s.returnOrigins, strings.ToLower(u.Scheme+"://"+u.Host)) {
		return "", authdomain.ErrInvalidReturnTo
	}
	u.Fragment = "" // the result goes in the fragment
	return u.String(), nil
}

func (s *Service) callbackURL(provider string) string {
	return s.publicURL + "/v1/auth/" + provider + "/callback"
}

// providerName is how emails name a provider.
func providerName(provider string) string {
	switch provider {
	case social.Google:
		return "Google"
	case social.Apple:
		return "Apple"
	case social.GitHub:
		return "GitHub"
	}
	return provider
}

// identityAAD binds an encrypted refresh token to its identity.
func identityAAD(provider, subject string) []byte {
	return []byte(provider + ":" + subject + ":refresh")
}

func cmpOr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

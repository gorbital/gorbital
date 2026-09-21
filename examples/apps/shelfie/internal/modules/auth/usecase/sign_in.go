package usecase

import (
	"context"
	"errors"

	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

// SignIn signs userID in with a module's own method, such as a code sent to
// a phone, once the module has verified the person controls that method
// (ADR-0083, Phase 6). It applies what Login applies after a correct
// password: the login rate limits of the account's address, an unverified
// address refused (ErrEmailNotVerified), a second-factor challenge for an
// account with two-factor authentication (finished with LoginMFA), a
// banned account refused, the BeforeLogin and AfterLogin hooks, and
// auth.login.succeeded recording method.
//
// method must pass domain.CheckCustomMethod. An unknown or deleted account
// returns ErrInvalidCredentials, a limit a *RateLimitError, and a hook's
// refusal its *domain.Refusal.
func (s *Service) SignIn(ctx context.Context, userID, method string) (LoginResult, error) {
	if err := authdomain.CheckCustomMethod(method); err != nil {
		return LoginResult{}, err
	}
	client := authlib.ClientInfoFromContext(ctx)
	u, err := s.store.SelectUserByID(ctx, userID, false)
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		s.loginFailed(ctx, "", "invalid_credentials", client)
		return LoginResult{}, authdomain.ErrInvalidCredentials
	case err != nil:
		return LoginResult{}, dbError("sign in", err)
	}
	if ok, retry := s.allowLogin(ctx, u.NormalizedEmail, client); !ok {
		s.loginFailed(ctx, u.ID, "rate_limited", client)
		return LoginResult{}, &authdomain.RateLimitError{RetryAfter: retry}
	}
	if !u.EmailVerified() {
		s.loginFailed(ctx, u.ID, "email_not_verified", client)
		return LoginResult{}, authdomain.ErrEmailNotVerified
	}

	methods, err := s.secondFactorMethods(ctx, u.ID)
	switch {
	case errors.Is(err, authdomain.ErrMFAUnavailable):
		s.logger.ErrorContext(ctx, "sign-in needs a second factor this server can't check: set AUTH_ENCRYPTION_KEYS or WEBAUTHN_RP_ID", "user_id", u.ID)
		return LoginResult{}, err
	case err != nil:
		return LoginResult{}, dbError("sign in", err)
	case len(methods) > 0:
		return s.startChallenge(ctx, u, "", false, client, methods, method)
	}

	var res LoginResult
	err = s.store.InTx(ctx, func(tx Store) error {
		var err error
		res, err = s.startSignIn(ctx, tx, u, false, client, method, "")
		return err
	})
	if err != nil {
		return LoginResult{}, s.signInFailed(ctx, "sign in", u.ID, method, err, client)
	}
	s.loginSucceeded(ctx, res, method, "")
	s.afterLogin(ctx, res, method, "")
	return res, nil
}

package authhttp

import (
	"context"
	"errors"
	"fmt"

	authlib "gorbital.dev/modules/auth"

	"example.com/invoicing/internal/modules/auth/delivery"
	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// ErrUserNotFound is returned by [Authenticator.User] for an unknown or
// deleted account. Returned from a handler, it answers 404 user_not_found.
var ErrUserNotFound = authdomain.ErrUserNotFound

// A SignInRequest signs an account in with a module's own method, once the
// module has verified the person controls it (see [Authenticator.SignIn]).
type SignInRequest struct {
	// UserID is the account the verified method belongs to.
	UserID string
	// Method names the module's method in audit events and hooks, such as
	// phone_code: lowercase snake_case of 2 to 32 characters, not password,
	// passkey, operator, google, apple, github, mfa, api_key or
	// impersonation.
	Method string
	// Transport is how the session reaches the client, as in POST
	// /v1/auth/login: cookie (browsers, the default) sets the HttpOnly
	// __Host-session cookie; bearer (native apps) returns the token in the
	// body.
	Transport string
}

// SignedIn is the response of a sign-in, the same as POST /v1/auth/login's:
// Status 200 with the session (Body.User, Body.Session, and Body.Token for
// transport bearer, or the cookie in SetCookie), or 202 with Body.MFA
// (challenge_token, methods, expires_at) for an account with two-factor
// authentication, which the client finishes with POST /v1/auth/login/mfa.
// Return it from a Huma handler as is: its body schema is login's
// LoginResponse.
type SignedIn = delivery.LoginOutput

// SignIn signs an account in with a module's own method and returns what
// POST /v1/auth/login returns after a correct password, so a module's
// sign-in route returns it as its output:
//
//   - the account's login rate limits (auth_login, auth_login_address)
//     are charged: 429 too_many_attempts;
//   - an account whose address isn't verified gets 403 email_not_verified;
//   - an account with two-factor authentication gets 202 and a challenge;
//   - a banned account gets 403 account_banned;
//   - the [BeforeLogin] hooks run, then the session is created, audited as
//     auth.login.succeeded with the method in its metadata, and the
//     [AfterLogin] hooks run;
//   - an unknown or deleted account gets 401 invalid_credentials.
//
// SignIn doesn't check anything about the method itself: the module must
// have verified, before calling it, that the person controls a credential
// bound to req.UserID — a single-use code sent to a phone number the
// account confirmed, a signature from a key it registered — with its own
// attempt limits (guard.RateLimit) and without revealing whether an
// account exists. Never call it with an ID taken from the request.
//
// The errors are problems (or sign-in's mapped errors) a handler returns as
// they are. Calling SignIn before gorbital.New has set sign-in up, or with
// an invalid Method or Transport, returns a plain error (500).
func (a *Authenticator) SignIn(ctx context.Context, req SignInRequest) (*SignedIn, error) {
	svc := a.service()
	if svc == nil {
		return nil, errors.New("authhttp: SignIn called before Setup: pass the Authenticator to gorbital.WithAuth")
	}
	switch req.Transport {
	case "":
		req.Transport = authdomain.TransportCookie
	case authdomain.TransportCookie, authdomain.TransportBearer:
	default:
		return nil, fmt.Errorf("authhttp: SignIn: transport %q is cookie or bearer", req.Transport)
	}
	res, err := svc.SignIn(ctx, req.UserID, req.Method)
	if errors.Is(err, authdomain.ErrInvalidMethod) {
		return nil, fmt.Errorf("authhttp: SignIn: method %q: %w", req.Method, err)
	}
	if err != nil {
		return nil, delivery.AuthError(err)
	}
	return delivery.SignedIn(res, req.Transport, authlib.DefaultCookieName), nil
}

// User returns an account with its platform roles, or [ErrUserNotFound].
// A module uses it to show or check the account its own records point to.
func (a *Authenticator) User(ctx context.Context, id string) (User, error) {
	svc := a.service()
	if svc == nil {
		return User{}, errors.New("authhttp: User called before Setup: pass the Authenticator to gorbital.WithAuth")
	}
	u, err := svc.User(ctx, id)
	if err != nil {
		return User{}, err
	}
	return user(u), nil
}

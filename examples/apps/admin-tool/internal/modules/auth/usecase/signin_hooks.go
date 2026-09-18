package usecase

import (
	"context"
	"errors"

	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

// SignInHooks let the app take part in sign-in and account creation
// (ADR-0083, Phase 6). authhttp builds them from its options: it traces
// them, bounds AfterLogin and converts refusals to *domain.Refusal.
//
// Unlike AccountHooks, BeforeLogin and OnRegister run inside sign-in's
// transaction, so an OnRegister error rolls the account back and what
// BeforeLogin writes commits only with the session.
type SignInHooks struct {
	// BeforeLogin runs once every factor of a sign-in is verified and the
	// account isn't banned, in the transaction that creates the session,
	// for every method but impersonation. A *domain.Refusal refuses the
	// sign-in; any other error fails it as a server error.
	BeforeLogin func(ctx context.Context, tx Store, a LoginAttempt) error
	// AfterLogin runs after the session is committed and audited. It
	// returns nothing: authhttp logs its errors.
	AfterLogin func(ctx context.Context, e LoginEvent)
	// OnRegister runs in the transaction that creates an account, for
	// every way one is created. fields are the extra registration fields of
	// an email registration (nil otherwise). An error rolls the account
	// back.
	OnRegister func(ctx context.Context, tx Store, a NewAccount, fields any) error
}

// LoginAttempt is a sign-in whose factors are verified, before its session
// exists. It never carries a password, token or code.
type LoginAttempt struct {
	User authdomain.User
	// Method is how the sign-in started: password, passkey, google, apple,
	// github or a custom method's name.
	Method string
	// SecondFactor is the second factor that finished it (totp, passkey or
	// recovery_code), or empty.
	SecondFactor string
	Client       authlib.ClientInfo
}

// LoginEvent is a sign-in whose session is committed.
type LoginEvent struct {
	LoginAttempt
	SessionID string
}

// NewAccount is an account being created.
type NewAccount struct {
	User authdomain.User
	// Method is how: password (email registration), google, apple, github
	// or operator.
	Method string
	// Name is the name the provider gave, when it gave one.
	Name   string
	Client authlib.ClientInfo
}

// beforeLogin runs the BeforeLogin hook for u, whose factors are verified,
// inside tx.
func (s *Service) beforeLogin(ctx context.Context, tx Store, u authdomain.User, method, secondFactor string, client authlib.ClientInfo) error {
	if s.signInHooks.BeforeLogin == nil {
		return nil
	}
	return s.signInHooks.BeforeLogin(ctx, tx, LoginAttempt{User: u, Method: method, SecondFactor: secondFactor, Client: client})
}

// afterLogin runs the AfterLogin hook for a committed session.
func (s *Service) afterLogin(ctx context.Context, res LoginResult, method, secondFactor string) {
	if s.signInHooks.AfterLogin == nil {
		return
	}
	s.signInHooks.AfterLogin(ctx, LoginEvent{
		LoginAttempt: LoginAttempt{User: res.User, Method: method, SecondFactor: secondFactor, Client: authlib.ClientInfoFromContext(ctx)},
		SessionID:    res.Session.ID,
	})
}

// onRegister runs the OnRegister hook for an account inserted in tx.
func (s *Service) onRegister(ctx context.Context, tx Store, a NewAccount, fields any) error {
	if s.signInHooks.OnRegister == nil {
		return nil
	}
	return s.signInHooks.OnRegister(ctx, tx, a, fields)
}

// refusal returns err's refusal, if it is one.
func refusal(err error) (*authdomain.Refusal, bool) {
	var r *authdomain.Refusal
	ok := errors.As(err, &r)
	return r, ok
}

func isRefusal(err error) bool {
	_, ok := refusal(err)
	return ok
}

// loginRefused records a sign-in a hook refused.
func (s *Service) loginRefused(ctx context.Context, userID, method string, r *authdomain.Refusal, client authlib.ClientInfo) {
	e := userEvent("auth.login.failed", userID, client)
	e.Outcome = "failure"
	e.Metadata = map[string]any{"reason": "refused", "code": r.Code, "method": method}
	s.audit(ctx, e)
}

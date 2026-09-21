package authhttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"gorbital.dev/httpx"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
	"example.com/shelfie/internal/modules/auth/repository"
	"example.com/shelfie/internal/modules/auth/usecase"
)

// A User is an account, as hooks and modules see it. It never carries the
// password hash.
type User struct {
	ID    string
	Email string
	// EmailVerified reports whether the owner proved the address: with a
	// code, or through Google, Apple or GitHub, which are authoritative for
	// their own addresses.
	EmailVerified bool
	// HasPassword is false for accounts created with Google, Apple or
	// GitHub until they set one.
	HasPassword bool
	// Banned reports an operator's ban (POST /ops/auth/users/{id}/ban).
	Banned    bool
	CreatedAt time.Time
	// Roles are the account's platform roles. Empty in [NewAccount] and
	// [LoginAttempt]; loaded in [LoginEvent] and by [Authenticator.User].
	Roles []string
}

// A LoginAttempt is a sign-in whose every factor is verified, just before its
// session is created, as [BeforeLogin] hooks receive it: a correct password
// for a verified address, a passkey, a Google, Apple or GitHub identity, or a
// module's method through [Authenticator.SignIn], and, for an account with
// two-factor authentication, the second factor too. A banned account is
// refused before the hooks run. The hooks never see unknown addresses, wrong
// passwords or failed second factors, so they can't tell anyone whether an
// address has an account, and a refusal is only ever shown to someone who
// passed every check. Impersonation in development doesn't run them.
type LoginAttempt struct {
	User User
	// Method is how the sign-in started: password, passkey, google, apple,
	// github or a module's method name.
	Method string
	// SecondFactor is the second factor that finished it: totp,
	// recovery_code or passkey; empty without one.
	SecondFactor string
	// IP and UserAgent are the client's, after APP_TRUSTED_PROXIES.
	IP        string
	UserAgent string
}

// A LoginEvent is a sign-in whose session is committed and audited, as
// [AfterLogin] hooks receive it. It never carries the session's token.
type LoginEvent struct {
	LoginAttempt
	SessionID string
}

// A NewAccount is an account being created, in the transaction that creates
// it, as [OnRegister] and [RegisterFields] hooks receive it.
//
// What a hook's error does depends on how the account is created:
//
//   - password (POST /v1/auth/register): the account is rolled back, the
//     error logged, and the response is still 202. Registration answers the
//     same whether or not the address has an account, and hooks run only
//     for new ones, so any other answer would reveal that. Refuse bad input
//     with RegisterFields' validation or a [PasswordPolicy] instead.
//   - google, apple, github (a first sign-in): the account is rolled back; a
//     [Refuse] error answers 403 with its code (in the redirect's fragment
//     for web sign-ins) and any other error 500. The provider already
//     proved the identity, so nothing is revealed.
//   - operator (POST /ops/auth/users, orb dev's seed data): the account is
//     rolled back; 403 with a refusal's code, otherwise 500.
type NewAccount struct {
	User User
	// Method is password, google, apple, github or operator.
	Method string
	// Name is the name the provider gave on a first Google, Apple or GitHub
	// sign-in, when it gave one (Apple only the first time), to prefill a
	// profile. Empty otherwise.
	Name string
	// IP and UserAgent are the client's; empty for operators.
	IP        string
	UserAgent string
}

// A Refusal is a hook refusing a sign-in or an account: the client receives
// 403 with Code and Detail as a problem. Create one with [Refuse].
type Refusal struct {
	Code   string
	Detail string
}

// Error returns the code and detail.
func (r *Refusal) Error() string { return "authhttp: refused: " + r.Code + ": " + r.Detail }

var refusalCode = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

// Refuse returns the error a [BeforeLogin], [OnRegister] or [RegisterFields]
// hook returns to refuse with 403, code and detail. The code is lowercase
// snake_case, 3 to 64 characters, and can't be one of sign-in's or gorbital's
// own codes (such as invalid_credentials, account_banned or mfa_required),
// so clients can always tell the app's refusals apart. Refuse panics on
// such a code: declare refusals as package variables, so a wrong code stops
// the program when it starts, before gorbital.New:
//
//	var errSuspended = authhttp.Refuse("reader_suspended", "this account is suspended; write to support")
func Refuse(code, detail string) error {
	if err := checkRefusalCode(code); err != nil {
		panic(err)
	}
	return &Refusal{Code: code, Detail: detail}
}

// checkRefusalCode reports a code a refusal can't use.
func checkRefusalCode(code string) error {
	switch {
	case !refusalCode.MatchString(code):
		return fmt.Errorf("authhttp: refusal code %q isn't lowercase snake_case of 3 to 64 characters", code)
	case reservedCodes()[code]:
		return fmt.Errorf("authhttp: refusal code %q is one of sign-in's or gorbital's own codes; choose a code of the app's", code)
	}
	return nil
}

// reservedCodes are the problem codes a refusal can't use: builtInCodes,
// sign-in's error mappings and the generic code of every status.
func reservedCodes() map[string]bool {
	codes := map[string]bool{}
	for _, c := range builtInCodes {
		codes[c] = true
	}
	for _, status := range []int{400, 401, 403, 404, 405, 409, 413, 422, 429, 500, 503} {
		codes[httpx.DefaultCode(status)] = true
	}
	for _, m := range errorMappings() {
		codes[m.Code] = true
	}
	return codes
}

// tracer traces the app's hooks.
var tracer = otel.Tracer("gorbital.dev/gorbital/authhttp")

// afterLoginTimeout is the longest a response waits for AfterLogin hooks; a
// variable for tests.
var afterLoginTimeout = 5 * time.Second

// hooks returns the use cases' hooks for o: each traced, with the app's
// errors converted, and AfterLogin bounded.
func (o options) hooks(logger *slog.Logger) usecase.SignInHooks {
	var h usecase.SignInHooks
	if len(o.beforeLogin) > 0 {
		h.BeforeLogin = func(ctx context.Context, store usecase.Store, a usecase.LoginAttempt) error {
			ctx, span := tracer.Start(ctx, "authhttp.BeforeLogin", trace.WithAttributes(attribute.String("gorbital.auth.method", a.Method)))
			defer span.End()
			tx, ok := repository.Tx(store)
			if !ok {
				return hookFailed(span, "BeforeLogin", errors.New("not in a transaction"))
			}
			attempt := loginAttempt(a)
			for _, hook := range o.beforeLogin {
				if err := hook(ctx, tx, attempt); err != nil {
					return hookFailed(span, "BeforeLogin", err)
				}
			}
			return nil
		}
	}
	if len(o.afterLogin) > 0 {
		h.AfterLogin = func(ctx context.Context, e usecase.LoginEvent) {
			runAfterLogin(ctx, logger, o.afterLogin, LoginEvent{LoginAttempt: loginAttempt(e.LoginAttempt), SessionID: e.SessionID})
		}
	}
	if len(o.onRegister) > 0 || o.saveFields != nil {
		h.OnRegister = func(ctx context.Context, store usecase.Store, a usecase.NewAccount, fields any) error {
			ctx, span := tracer.Start(ctx, "authhttp.OnRegister", trace.WithAttributes(attribute.String("gorbital.auth.method", a.Method)))
			defer span.End()
			tx, ok := repository.Tx(store)
			if !ok {
				return hookFailed(span, "OnRegister", errors.New("not in a transaction"))
			}
			account := NewAccount{User: user(a.User), Method: a.Method, Name: a.Name, IP: a.Client.IP, UserAgent: a.Client.UserAgent}
			for _, hook := range o.onRegister {
				if err := hook(ctx, tx, account); err != nil {
					return hookFailed(span, "OnRegister", err)
				}
			}
			if fields != nil && o.saveFields != nil {
				if err := o.saveFields(ctx, tx, account, fields); err != nil {
					return hookFailed(span, "RegisterFields", err)
				}
			}
			return nil
		}
	}
	return h
}

// hookFailed records a hook's error on span and converts it for the use
// cases: a valid refusal to *domain.Refusal, anything else, including a
// Refusal built without Refuse whose code is reserved, to a server error.
func hookFailed(span trace.Span, hook string, err error) error {
	if r, ok := errors.AsType[*Refusal](err); ok {
		if codeErr := checkRefusalCode(r.Code); codeErr != nil {
			err = codeErr
		} else {
			span.SetAttributes(attribute.String("gorbital.auth.refused", r.Code))
			return &authdomain.Refusal{Code: r.Code, Detail: r.Detail}
		}
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, hook+" hook failed")
	return &authdomain.HookError{Hook: hook, Err: err}
}

// runAfterLogin runs the AfterLogin hooks with a deadline, and returns when
// they finish or the deadline passes, whichever is first. Errors and panics
// are logged.
func runAfterLogin(ctx context.Context, logger *slog.Logger, hooks []func(context.Context, LoginEvent) error, e LoginEvent) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), afterLoginTimeout)
	defer cancel()
	ctx, span := tracer.Start(ctx, "authhttp.AfterLogin", trace.WithAttributes(attribute.String("gorbital.auth.method", e.Method)))
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer span.End()
		defer func() {
			if r := recover(); r != nil {
				span.SetStatus(codes.Error, "AfterLogin hook panicked")
				logger.ErrorContext(ctx, "AfterLogin hook panicked", "panic", fmt.Sprint(r))
			}
		}()
		for _, hook := range hooks {
			if err := hook(ctx, e); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, "AfterLogin hook failed")
				logger.ErrorContext(ctx, "AfterLogin hook failed", "method", e.Method, "err", err)
			}
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
		logger.WarnContext(ctx, "AfterLogin hooks ran past their deadline; the sign-in's response didn't wait", "method", e.Method, "timeout", afterLoginTimeout)
	}
}

// user converts an account for the app; the password hash stays inside.
func user(u authdomain.User) User {
	return User{
		ID: u.ID, Email: u.Email, EmailVerified: u.EmailVerified(), HasPassword: u.HasPassword(), Banned: u.Banned(),
		CreatedAt: u.CreatedAt, Roles: append([]string(nil), u.Roles...),
	}
}

func loginAttempt(a usecase.LoginAttempt) LoginAttempt {
	return LoginAttempt{User: user(a.User), Method: a.Method, SecondFactor: a.SecondFactor, IP: a.Client.IP, UserAgent: a.Client.UserAgent}
}

package authhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/acme-api/internal/modules/auth/delivery"
	"github.com/jackc/pgx/v5"

	"gorbital.dev/mail"
	authlib "gorbital.dev/modules/auth"
)

// An Option changes sign-in from v0.1's behaviour, which [New] keeps when it
// gets none (ADR-0083, Phase 6). Deployment values, such as provider
// credentials and the passkey relying party, stay in environment variables.
// A mistake in an option, such as a password length below the minimum, is
// reported by [Authenticator.CheckConfig], so the app exits with status 2
// before it connects.
type Option interface{ apply(*options) }

type optionFunc func(*options)

func (f optionFunc) apply(o *options) { f(o) }

type options struct {
	minPasswordLength int
	passwordPolicies  []func(ctx context.Context, password string) error
	requireMFA        []string
	apiKeyMaxTTL      time.Duration
	closed            bool
	brand             *mail.Brand
	routeMiddleware   []func(http.Handler) http.Handler
	// methods are the sign-in methods the app serves (Methods); nil is
	// every one of them.
	methods map[Method]bool

	beforeLogin  []func(ctx context.Context, tx pgx.Tx, a LoginAttempt) error
	afterLogin   []func(ctx context.Context, e LoginEvent) error
	onRegister   []func(ctx context.Context, tx pgx.Tx, a NewAccount) error
	registration delivery.Registration
	saveFields   func(ctx context.Context, tx pgx.Tx, a NewAccount, fields any) error

	errs []error
}

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt.apply(&o)
		}
	}
	if o.closed && o.registration != nil {
		o.errs = append(o.errs, errors.New("authhttp: RegisterFields and WithoutRegistration: without registration there are no registration fields"))
	}
	return o
}

// A Method is one way of signing in, with the operations, runtime
// settings, jobs, rate limiters and permissions that belong to it.
// [Methods] chooses the ones an app serves. The values are public API: the
// operations API and the CLI's profiles name them.
type Method string

// The sign-in methods [Methods] chooses from. Their migrations are applied
// whichever are served, so the tables of a method an app leaves out exist
// and stay empty, and adding the method later is one line in main.go
// (ADR-0089).
const (
	// MethodPassword is registration, email verification, signing in with
	// a password, sessions, password changes and deleting the account.
	// Every app serves it: it is what this package implements.
	MethodPassword Method = "password"
	// MethodOperators is the operators' account APIs under
	// /ops/auth/users, with the ops.auth.write permission.
	MethodOperators Method = "operators"
	// MethodTOTP is two-factor authentication with an authenticator app,
	// its recovery codes and finishing a sign-in with them.
	MethodTOTP Method = "totp"
	// MethodPasskeys is signing in with a passkey, and adding, naming and
	// removing them.
	MethodPasskeys Method = "passkeys"
	// MethodSocial is Google, Apple and GitHub sign-in and the accounts
	// linked to them.
	MethodSocial Method = "social"
	// MethodAPIKeys is the API keys of a signed-in account and the
	// platform's service accounts, with their ops.service_accounts
	// permissions.
	MethodAPIKeys Method = "api_keys"
)

// allMethods are the methods an app serves without the [Methods] option.
var allMethods = []Method{MethodPassword, MethodOperators, MethodTOTP, MethodPasskeys, MethodSocial, MethodAPIKeys}

// Methods chooses the sign-in methods the app serves, such as
// authhttp.Methods(authhttp.MethodPassword, authhttp.MethodOperators) for
// an app that wants an email address, a password and the operators'
// account APIs. Without the option every method is served, which is v0.2's
// sign-in byte for byte.
//
// A method the app doesn't serve has no operations, so its paths are 404
// and they leave the OpenAPI document, and it declares no runtime
// settings, jobs, rate limiters or permissions, so /ops doesn't list them
// and no role can be granted them. Its migrations are applied all the
// same: the tables exist and stay empty, so adding the method later is
// this one line and a restart (ADR-0089).
//
// [MethodPassword] is required: this package is the password
// implementation, and an app that doesn't want passwords wants another
// authenticator. A set without it, or with a method that doesn't exist, is
// reported by [Authenticator.CheckConfig], so the app exits with status 2
// before it connects.
func Methods(m ...Method) Option {
	return optionFunc(func(o *options) {
		set := make(map[Method]bool, len(m))
		for _, method := range m {
			if !slices.Contains(allMethods, method) {
				o.errs = append(o.errs, fmt.Errorf("authhttp: Methods(%q): the sign-in methods are %s", method, methodNames()))
				return
			}
			set[method] = true
		}
		if !set[MethodPassword] {
			o.errs = append(o.errs, fmt.Errorf("authhttp: Methods: %s is required: authhttp is the password implementation, and an app without passwords wants another gorbital.Authenticator", MethodPassword))
			return
		}
		o.methods = set
	})
}

// methodNames lists the methods for an error message.
func methodNames() string {
	names := make([]string, len(allMethods))
	for i, m := range allMethods {
		names[i] = string(m)
	}
	return strings.Join(names, ", ")
}

// has reports whether the app serves m: every method without the Methods
// option.
func (o options) has(m Method) bool { return o.methods == nil || o.methods[m] }

// methodSet is the served methods as the delivery package takes them.
func (o options) methodSet() delivery.MethodSet {
	if o.methods == nil {
		return nil
	}
	set := make(delivery.MethodSet, len(o.methods))
	for m, on := range o.methods {
		set[delivery.Method(m)] = on
	}
	return set
}

// MinPasswordLength raises the shortest password accepted when an account
// registers, resets or changes its password, or an operator creates one,
// from 12 characters (auth.MinPasswordLength) up to at most 128
// (auth.MaxPasswordLength). Lengths count characters, not bytes. A shorter
// password gets 422 weak_password, "the password must be at least n
// characters". Existing passwords keep working.
//
// The OpenAPI document states n too, wherever a password field documents
// the minimum: registration, the password reset, the password change and
// the operators' account creation.
func MinPasswordLength(n int) Option {
	return optionFunc(func(o *options) {
		if n < authlib.MinPasswordLength || n > authlib.MaxPasswordLength {
			o.errs = append(o.errs, fmt.Errorf("authhttp: MinPasswordLength(%d): a minimum is %d to %d characters; a lower one would weaken v0.1's policy", n, authlib.MinPasswordLength, authlib.MaxPasswordLength))
			return
		}
		o.minPasswordLength = n
	})
}

// PasswordPolicy adds a check every new password must pass after the
// built-in rules (length, not blank) and [MinPasswordLength], such as a
// breached-password lookup or a ban on the app's name. A non-nil error
// refuses the password with 422 weak_password and the detail "the password
// " followed by the error's text, so word it to follow: "is too common".
// Several policies run in order. A policy runs before anything is stored
// and for every request, so its result can't reveal whether an address
// has an account; it must not log the password.
func PasswordPolicy(check func(ctx context.Context, password string) error) Option {
	return optionFunc(func(o *options) {
		if check == nil {
			o.errs = append(o.errs, errors.New("authhttp: PasswordPolicy(nil)"))
			return
		}
		o.passwordPolicies = append(o.passwordPolicies, check)
	})
}

// RequireMFA grants the permissions of roles only to sessions signed in with
// a second factor, as sign-in always does for platform_admin and
// ops_viewer: a session without one gets 403 mfa_required from routes those
// roles open, and an API key never holds them. A role must be declared by
// a module (gorbital.Module.Permissions); the user role every account holds
// can't require a second factor. Setup returns an error otherwise.
func RequireMFA(roles ...string) Option {
	return optionFunc(func(o *options) { o.requireMFA = append(o.requireMFA, roles...) })
}

// APIKeyMaxTTL caps the lifetime of new API keys: the runtime setting
// auth.api_key_max_ttl, which operators change, accepts at most d (instead
// of a year) and defaults to d when d is shorter than its 90-day default.
// d is 24 hours to 365 days. Existing keys keep their expiry.
func APIKeyMaxTTL(d time.Duration) Option {
	return optionFunc(func(o *options) {
		if d < minAPIKeyMaxTTL || d > maxAPIKeyMaxTTL {
			o.errs = append(o.errs, fmt.Errorf("authhttp: APIKeyMaxTTL(%s): the cap is 24h to 365 days, the range of auth.api_key_max_ttl", d))
			return
		}
		o.apiKeyMaxTTL = d
	})
}

// The range of auth.api_key_max_ttl.
const (
	minAPIKeyMaxTTL = 24 * time.Hour
	maxAPIKeyMaxTTL = 365 * 24 * time.Hour
)

// WithoutRegistration closes sign-up, for apps whose accounts come from
// operators or the app's own flows: POST /v1/auth/register isn't served
// (404, and it leaves the OpenAPI document), and a first Google, Apple or
// GitHub sign-in of an address without an account gets 403
// registration_closed (in the redirect's fragment for web sign-ins). Every
// other flow still works: accounts operators create (POST /ops/auth/users,
// orb dev's seed data), their verification and password reset, sign-in and
// linking providers to existing accounts, and custom methods through
// [Authenticator.SignIn].
func WithoutRegistration() Option {
	return optionFunc(func(o *options) { o.closed = true })
}

// Brand sets what every email sign-in sends has in common (mail.Brand): a
// logo, the support address, a footer line. An empty Name is the app's
// name (gorbital.WithName) and an empty URL is APP_PUBLIC_URL, as without
// the option. The dev console's previews use it too.
func Brand(b mail.Brand) Option {
	return optionFunc(func(o *options) { o.brand = &b })
}

// RouteMiddleware runs middleware on every operation under /v1/auth/, such
// as a CAPTCHA check on registration and sign-in or a country filter, after
// the app's middleware stack and before sign-in's own checks. It doesn't
// run on /ops/auth/users or /ops/service-accounts. Refuse with a problem
// (httpx.WriteProblem) and a code of your own. Middleware for the whole app
// goes in gorbital.WithMiddleware.
func RouteMiddleware(middleware ...func(http.Handler) http.Handler) Option {
	return optionFunc(func(o *options) { o.routeMiddleware = append(o.routeMiddleware, middleware...) })
}

// BeforeLogin adds a hook that decides whether a sign-in may start a
// session (see [LoginAttempt] for when it runs). Return [Refuse]'s error to
// refuse it with 403 and your code; any other error fails the sign-in with
// 500 internal_error, and nil lets it through. tx is the transaction that
// creates the session: read the app's tables in it, and what the hook
// writes commits only with the session. Several hooks run in the order
// given, until one returns an error.
func BeforeLogin(hook func(ctx context.Context, tx pgx.Tx, a LoginAttempt) error) Option {
	return optionFunc(func(o *options) {
		if hook == nil {
			o.errs = append(o.errs, errors.New("authhttp: BeforeLogin(nil)"))
			return
		}
		o.beforeLogin = append(o.beforeLogin, hook)
	})
}

// AfterLogin adds a hook that runs after a session is created (see
// [LoginEvent]). It can't change the sign-in: its error is logged, and the
// response waits for it at most 5 seconds, after which its context is
// cancelled. A panic is recovered and logged. Several hooks run in order.
func AfterLogin(hook func(ctx context.Context, e LoginEvent) error) Option {
	return optionFunc(func(o *options) {
		if hook == nil {
			o.errs = append(o.errs, errors.New("authhttp: AfterLogin(nil)"))
			return
		}
		o.afterLogin = append(o.afterLogin, hook)
	})
}

// OnRegister adds a hook that runs in the transaction that creates an
// account, for every way one is created (see [NewAccount]). Write the app's
// rows for the account in tx: a profile, a default workspace, a job with
// jobs.Client.InsertTx. An error rolls the account back; see [NewAccount]
// for what the client receives. Several hooks run in order, until one
// returns an error.
func OnRegister(hook func(ctx context.Context, tx pgx.Tx, a NewAccount) error) Option {
	return optionFunc(func(o *options) {
		if hook == nil {
			o.errs = append(o.errs, errors.New("authhttp: OnRegister(nil)"))
			return
		}
		o.onRegister = append(o.onRegister, hook)
	})
}

// RegisterFields adds the fields of T, a struct, to POST
// /v1/auth/register beside email and password, and passes them to save in
// the transaction that creates the account, after the [OnRegister] hooks.
// Huma validates them from T's tags (required unless omitempty,
// maxLength, enum, pattern…) and a Resolve method on *T, for every request
// and before anything is stored, and the OpenAPI document shows them.
// Properties the body doesn't name are still accepted, as in v0.1.
//
// Only email registration sends them: an account created by a first
// Google, Apple or GitHub sign-in, or by an operator, runs the OnRegister
// hooks without save. Let those users complete their profile later.
//
// save's error rolls the account back, like OnRegister's; the response is
// still 202 (see [NewAccount]), so validate in T, not in save. T's JSON
// names can't be email or password, and RegisterFields can be given once.
func RegisterFields[T any](save func(ctx context.Context, tx pgx.Tx, a NewAccount, fields T) error) Option {
	return optionFunc(func(o *options) {
		t := reflect.TypeFor[T]()
		switch {
		case save == nil:
			o.errs = append(o.errs, errors.New("authhttp: RegisterFields(nil)"))
			return
		case o.registration != nil:
			o.errs = append(o.errs, errors.New("authhttp: RegisterFields is given twice; one struct holds every registration field"))
			return
		case t.Kind() != reflect.Struct:
			o.errs = append(o.errs, fmt.Errorf("authhttp: RegisterFields[%s]: the fields are a struct", t))
			return
		}
		for _, name := range jsonNames(t) {
			if strings.EqualFold(name, "email") || strings.EqualFold(name, "password") {
				o.errs = append(o.errs, fmt.Errorf("authhttp: RegisterFields[%s]: the field %q is sign-in's", t, name))
				return
			}
		}
		o.registration = delivery.RegistrationWithFields[T]()
		o.saveFields = func(ctx context.Context, tx pgx.Tx, a NewAccount, fields any) error {
			f, ok := fields.(T)
			if !ok {
				return fmt.Errorf("authhttp: registration fields are %T, want %s", fields, t)
			}
			return save(ctx, tx, a, f)
		}
	})
}

// jsonNames returns the JSON names of t's fields, promoted fields included.
func jsonNames(t reflect.Type) []string {
	var names []string
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch {
		case name == "-" || !f.IsExported() && !f.Anonymous:
			continue
		case f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct:
			names = append(names, jsonNames(f.Type)...)
			continue
		case name == "":
			name = f.Name
		}
		names = append(names, name)
	}
	return names
}

// passwordChecker returns the check of MinPasswordLength and PasswordPolicy,
// or nil without them.
func (o options) passwordChecker() authlib.PasswordChecker {
	if o.minPasswordLength == 0 && len(o.passwordPolicies) == 0 {
		return nil
	}
	return func(ctx context.Context, password string) error {
		if n := utf8.RuneCountInString(password); n < o.minPasswordLength {
			return fmt.Errorf("must be at least %d characters", o.minPasswordLength)
		}
		for _, check := range o.passwordPolicies {
			if err := check(ctx, password); err != nil {
				return err
			}
		}
		return nil
	}
}

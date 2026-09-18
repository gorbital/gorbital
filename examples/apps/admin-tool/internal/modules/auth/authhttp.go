// Package authhttp is sign-in for apps on gorbital.Main: registration with
// email verification, passwords, sessions in a cookie or a bearer token,
// two-factor authentication with authenticator apps and passkeys, Google,
// Apple and GitHub sign-in, API keys and service accounts, platform roles,
// and the operators' account APIs under /ops/auth/users (ADR-0024,
// ADR-0038, ADR-0043 to ADR-0046, ADR-0058, ADR-0059, ADR-0070).
//
// It is the auth module v0.1 apps generate into internal/modules/auth,
// moved into the library unchanged: the same endpoints, error codes, audit
// events, permissions, runtime settings, jobs, rate limiters, cookies and
// migrations, configured by the same environment variables ([gorbital.Config]).
// Add it in main.go:
//
//	gorbital.Main(
//		gorbital.WithAuth(authhttp.New()),
//		gorbital.WithModules(modules.All()...),
//	)
//
// [gorbital.New] hands the authenticator its configuration and dependencies
// through [Authenticator.Setup], and [gorbital.Main] adds its commands:
// roles, grant-role, revoke-role, reset-mfa, rotate-auth-keys and
// auth-providers.
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).
package authhttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"gorbital.dev/gorbital"
	"gorbital.dev/mail"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/passkey"

	"example.com/admin-tool/internal/modules/auth/delivery/signintest"
	"example.com/admin-tool/internal/modules/auth/repository"
	"example.com/admin-tool/internal/modules/auth/usecase"
)

// An Authenticator is sign-in for one app. It implements
// gorbital.Authenticator, with the optional methods gorbital.New and
// gorbital.Main look for. Create it with [New] and pass it to
// gorbital.WithAuth; the app calls its methods.
type Authenticator struct {
	// settings are the auth.* runtime settings, declared by Module's
	// Settings.
	settings *authSettings

	mu      sync.Mutex
	set     bool
	name    string
	catalog *authlib.Catalog
	svc     *usecase.Service // nil until Setup with a database, as when exporting the OpenAPI document
	limits  *rateLimits      // nil until Setup with a database
	// orgs are the app's organisations (UseOrganisations), or nil.
	orgs Organisations

	// opts are New's options.
	opts options

	// endpoints point Google, Apple and GitHub at a fake provider in tests.
	endpoints providerEndpoints

	// tester runs the Dev Portal's sign-in tests; nil unless the dev
	// console is on (ADR-0087).
	tester *signintest.Tester
}

// New returns sign-in for an app, configured from the environment variables
// gorbital.LoadConfig reads (AUTH_ENCRYPTION_KEYS, WEBAUTHN_*, GOOGLE_*,
// APPLE_*, GITHUB_*, APP_PUBLIC_URL, AUTH_DEFAULT_RETURN_TO): each sign-in
// method is on when its variables are set. Use one Authenticator per app.
//
// Without options it is v0.1's sign-in. Options change the password policy
// ([MinPasswordLength], [PasswordPolicy]), second factors ([RequireMFA]), API
// keys ([APIKeyMaxTTL]), sign-up ([WithoutRegistration], [RegisterFields]),
// emails ([Brand]) and the routes ([RouteMiddleware]), and add hooks
// ([BeforeLogin], [AfterLogin], [OnRegister]). [Authenticator.CheckConfig]
// reports an invalid option.
func New(opts ...Option) *Authenticator {
	return &Authenticator{settings: &authSettings{}, opts: newOptions(opts)}
}

// Middleware authenticates each request with its session cookie
// (__Host-session) or bearer token, a session token or an API key, and sets
// the principal (auth.WithPrincipal) for the handlers and guards after it.
// A request without valid credentials passes on without an actor; one with
// a malformed, unknown or wrong API key counts against the
// auth.api_key_failures_per_minute limit of its network.
//
// gorbital.New calls it after [Authenticator.Setup]; it panics before.
func (a *Authenticator) Middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	svc := a.service()
	if svc == nil {
		panic("authhttp: Middleware called before Setup: pass the Authenticator to gorbital.WithAuth")
	}
	return authlib.Middleware(svc, authlib.WithCookieName(authlib.DefaultCookieName), authlib.WithLogger(logger), authlib.WithAPIKeys(svc))
}

// CheckConfig checks what sign-in needs its own packages for, which
// gorbital.LoadConfig leaves to it: AUTH_ENCRYPTION_KEYS is required in
// production; APPLE_PRIVATE_KEY (or APPLE_PRIVATE_KEY_FILE) must be an
// Apple sign-in key; WEBAUTHN_APPLE_APP_IDS and WEBAUTHN_ANDROID_APPS must
// parse; and WEBAUTHN_ORIGINS must be on WEBAUTHN_RP_ID. It returns every
// problem joined, with v0.1's messages. gorbital.New and gorbital.Main call
// it before anything connects, and exit with status 2 on its error.
//
// It also reports the options [New] received that can't apply, such as
// [MinPasswordLength] below 12, after the configuration's problems.
func (a *Authenticator) CheckConfig(cfg gorbital.Config) error {
	return errors.Join(append(checkConfig(cfg), a.opts.errs...)...)
}

// Setup builds sign-in from the app's configuration and dependencies:
// the use cases on s.Deps.DB, the rate limiters shared through
// s.Deps.RateLimits, email through s.Deps.Mailer with the app's brand, the
// sign-in providers that are configured, and the roles it relies on in
// s.Permissions: the user role every account holds, and a second factor
// required for platform_admin and ops_viewer, whose permissions reach /ops.
// It freezes the catalog, serves the /.well-known files passkeys in iOS and
// Android apps need when those apps are configured, and adds sign-in's
// emails to the dev console's previews. Impersonation is available only
// when s.DevConsole is set.
//
// gorbital.New calls it once; gorbital.Main calls it with zero Deps before
// one of [Authenticator.Commands] runs, which then opens its own database
// connection. It returns an error when called a second time with a
// database, or when a dependency sign-in needs is missing.
func (a *Authenticator) Setup(ctx context.Context, s gorbital.AuthSetup) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.set {
		return errors.New("authhttp: Setup called twice: an Authenticator serves one app; call authhttp.New for each")
	}
	if s.Permissions == nil {
		return errors.New("authhttp: AuthSetup.Permissions is nil")
	}
	if err := errors.Join(a.opts.errs...); err != nil {
		return err
	}
	a.name, a.catalog = s.Name, s.Permissions
	if err := declareRoles(s.Permissions, a.opts.requireMFA); err != nil {
		return err
	}
	if s.Deps.DB == nil {
		return nil // a command, or the OpenAPI document: nothing serves requests
	}
	a.set = true
	d := s.Deps
	if !a.settings.declared() {
		return errors.New("authhttp: sign-in's settings aren't declared: add the Authenticator with gorbital.WithAuth, which adds its Module")
	}
	if d.Audit == nil || d.Mailer == nil || d.RateLimits == nil || d.Logger == nil {
		return errors.New("authhttp: AuthSetup.Deps needs DB, Audit, Mailer, RateLimits and Logger")
	}
	limits, err := newRateLimits(d.RateLimits, a.settings)
	if err != nil {
		return err
	}
	google, apple, gitHub := providers(s.Config, a.endpoints)
	svc, err := usecase.NewService(usecase.Config{
		Store:                   repository.NewStore(d.DB),
		Impersonation:           s.DevConsole, // operators act as a user only in development (ADR-0070)
		LoginLimiter:            limits.login,
		LoginAddressLimiter:     limits.loginAddress,
		MFALimiter:              limits.mfa,
		ReauthLimiter:           limits.reauth,
		CodeLimiter:             limits.code,
		NoticeLimiter:           limits.notice,
		APIKeyLimiter:           limits.apiKey,
		APIKeyMaxTTL:            a.settings.apiKeyMaxTTL,
		Google:                  google,
		Apple:                   apple,
		GitHub:                  gitHub,
		PublicURL:               s.Config.Auth.PublicURL,
		ReturnOrigins:           returnOrigins(s.Config),
		DefaultReturnTo:         s.Config.Auth.DefaultReturnTo,
		Catalog:                 s.Permissions,
		Recorder:                d.Audit,
		Emails:                  authlib.NewBrandedEmails(d.Mailer, a.brand(s.Name, s.Config)),
		PasswordChecker:         a.opts.passwordChecker(),
		SignInHooks:             a.opts.hooks(d.Logger.With("source", "auth")),
		RegistrationClosed:      a.opts.closed,
		Keyring:                 keyring(s.Config),
		Issuer:                  s.Name,
		Passkeys:                passkeys(s.Name, s.Config),
		Logger:                  d.Logger.With("source", "auth"),
		SessionIdleTTL:          a.settings.sessionIdleTTL,
		SessionAbsoluteTTL:      a.settings.sessionAbsoluteTTL,
		VerificationCodeTTL:     a.settings.verificationCodeTTL,
		ResetCodeTTL:            a.settings.resetCodeTTL,
		DeletedAccountRetention: a.settings.deletedAccountRetention,
		UnverifiedAccountTTL:    a.settings.unverifiedAccountTTL,
		// Organisations take part once connected (orgs.go).
		Hooks: orgHooks{a: a},
		Orgs:  orgAccess{a: a},
	})
	if err != nil {
		return err
	}
	a.svc, a.limits = svc, limits

	a.mountWellKnown(s)
	a.mountSignInTests(s, google, apple, gitHub)
	s.MailPreviews(authlib.BrandedEmailPreviews(a.brand(s.Name, s.Config))...)
	reportSignInMethods(ctx, s.Config, d.Logger)
	return nil
}

// service returns the use cases, or nil before Setup.
func (a *Authenticator) service() *usecase.Service {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.svc
}

// brand is what every email sign-in sends has in common (ADR-0078): the
// app's name, linking to its public URL, or the Brand option with those as
// its defaults.
func (a *Authenticator) brand(name string, cfg gorbital.Config) mail.Brand {
	b := mail.Brand{}
	if a.opts.brand != nil {
		b = *a.opts.brand
	}
	if b.Name == "" {
		b.Name = name
	}
	if b.URL == "" {
		b.URL = cfg.Auth.PublicURL
	}
	return b
}

// keyring returns the parsed AUTH_ENCRYPTION_KEYS, or nil without keys.
// gorbital.LoadConfig has validated them.
func keyring(cfg gorbital.Config) *authlib.Keyring {
	if cfg.Auth.EncryptionKeys.IsZero() {
		return nil
	}
	k, _ := authlib.ParseKeyring(cfg.Auth.EncryptionKeys.Reveal())
	return k
}

// passkeys returns the passkey relying party, or nil when passkeys are off.
// CheckConfig has validated the configuration.
func passkeys(name string, cfg gorbital.Config) *passkey.Service {
	w, _ := loadWebAuthn(cfg)
	s, _ := w.service(name)
	return s
}

// returnOrigins are where a web sign-in may send the browser back: the API
// itself and the frontends allowed by APP_CORS_ORIGINS.
func returnOrigins(cfg gorbital.Config) []string {
	origins := []string{strings.ToLower(cfg.Auth.PublicURL)}
	for _, o := range cfg.CORSOrigins {
		origins = append(origins, strings.ToLower(strings.TrimRight(o, "/")))
	}
	return origins
}

// jsonDocument serves doc as a cacheable JSON file.
func jsonDocument(doc []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(doc)
	})
}

// mountWellKnown serves the files iOS and Android apps need to use passkeys
// for this site, when their apps are configured (ADR-0044). They must be
// reachable on the RP ID's domain: when a web frontend serves that domain,
// it proxies these paths to the API.
func (a *Authenticator) mountWellKnown(s gorbital.AuthSetup) {
	w, _ := loadWebAuthn(s.Config)
	if doc := passkey.AppleAppSiteAssociation(w.AppleAppIDs); doc != nil {
		s.Handle("GET /.well-known/apple-app-site-association", jsonDocument(doc))
	}
	if doc := passkey.AssetLinks(w.AndroidApps); doc != nil {
		s.Handle("GET /.well-known/assetlinks.json", jsonDocument(doc))
	}
}

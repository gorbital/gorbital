package authhttp_test

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	authhttp "example.com/shelfie/internal/modules/auth"
	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/httpx"
	"gorbital.dev/mail"
)

// migrationFiles stands in for an app's db/migrations package.
var migrationFiles embed.FS

// modulesAll stands in for the generated internal/modules.All.
func modulesAll() []gorbital.Module { return nil }

func ExampleNew() {
	// cmd/api/main.go of an app with sign-in: registration, sessions,
	// two-factor authentication, passkeys, Google, Apple and GitHub sign-in
	// and API keys, turned on by their environment variables.
	main := func() {
		gorbital.Main(
			gorbital.WithAuth(authhttp.New()),
			gorbital.WithModules(modulesAll()...),
			gorbital.WithMigrations(migrationFiles),
		)
	}
	_ = main
}

func ExampleAuthenticator() {
	// Without gorbital.Main: build the app from a loaded configuration. New
	// checks the sign-in configuration, then hands the authenticator its
	// dependencies (Setup) before serving.
	ctx := context.Background()
	cfg, err := gorbital.LoadConfig(config.OS)
	if err != nil {
		log.Fatal(err)
	}
	app, err := gorbital.New(ctx, cfg, gorbital.WithAuth(authhttp.New()), gorbital.WithModules(modulesAll()...))
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(app.Run(ctx))
}

func ExampleAuthenticator_Middleware() {
	// gorbital.New puts the middleware at the Auth step of the stack; a
	// custom order keeps it before the steps that read the caller.
	gorbital.WithStack(func(s gorbital.Stack) []func(http.Handler) http.Handler {
		steps := s.Default()
		// ... reorder or add steps; s.Auth is authhttp's Middleware.
		return steps
	})
	_ = slog.Default()
}

func ExampleAuthenticator_CheckConfig() {
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(k string) string {
		return map[string]string{
			"APP_ENV": "production", "STORAGE_DRIVER": "s3", "STORAGE_REGION": "eu-west-1", "STORAGE_BUCKET": "b",
			"STORAGE_ACCESS_KEY": "a", "STORAGE_SECRET_KEY": "s",
			"WEBAUTHN_RP_ID": "example.com", "WEBAUTHN_ORIGINS": "https://example.org",
		}[k]
	}})
	if err != nil {
		log.Fatal(err)
	}
	// gorbital.LoadConfig accepted it; sign-in's own checks don't.
	for line := range strings.Lines(authhttp.New().CheckConfig(cfg).Error()) {
		fmt.Print(line)
	}
	// Output:
	// AUTH_ENCRYPTION_KEYS is required in production: it encrypts two-factor authentication secrets (generate one: echo "k1:$(openssl rand -base64 32)")
	// WEBAUTHN_RP_ID and WEBAUTHN_ORIGINS: passkey: invalid configuration: origin "https://example.org" isn't on the relying party ID "example.com" or a subdomain of it
}

func ExampleAuthenticator_Commands() {
	for _, c := range authhttp.New().Commands() {
		fmt.Println(c.Usage)
	}
	// Output:
	// roles                          list the platform roles and their permissions
	// grant-role <email> <role>      give an account a platform role
	// revoke-role <email> <role>     take a platform role away
	// reset-mfa <email>              turn off an account's two-factor authentication
	// rotate-auth-keys               re-encrypt 2FA secrets with the first AUTH_ENCRYPTION_KEYS key
	// auth-providers                 show which sign-in methods are configured
	// seed [--email <email>]         create a development administrator with 2FA (orb dev runs it)
}

func ExampleAuthenticator_Module() {
	m := authhttp.New().Module()
	fmt.Println(m.Name)
	for _, p := range m.Permissions {
		fmt.Println(p.Name, p.Roles)
	}
	fmt.Println(m.Migrations[0].Version, m.Migrations[len(m.Migrations)-1].Version)
	// Output:
	// auth
	// ops.auth.read [platform_admin ops_viewer]
	// ops.auth.write [platform_admin]
	// ops.service_accounts.read [platform_admin ops_viewer]
	// ops.service_accounts.write [platform_admin]
	// 20260915000001 20260918000070
}

func ExampleAuthenticator_Setup() {
	// gorbital.New and gorbital.Main call Setup; an app never does. A
	// wrapper that adds to sign-in passes the call on.
	type auditedAuth struct{ *authhttp.Authenticator }
	setup := func(ctx context.Context, a auditedAuth, s gorbital.AuthSetup) error {
		s.Deps.Logger.InfoContext(ctx, "sign-in starting", "app", s.Name, "dev_console", s.DevConsole)
		return a.Setup(ctx, s)
	}
	_ = setup
}

func ExampleAuthenticator_SignInMethods() {
	// What GET /ops/auth/providers lists and auth-providers prints: every
	// method, and what turns the ones that are off on.
	cfg := gorbital.Config{}
	cfg.Auth.GitHubClientID = "Iv1.8a61f9b3a7aba766"
	cfg.Auth.PublicURL = "https://api.example.com"
	for _, m := range authhttp.New().SignInMethods(cfg) {
		if m.Key == "email_password" || m.Key == "github" || m.Key == "passkeys" {
			fmt.Println(m.Key, m.Enabled, m.Detail, m.Missing)
		}
	}
	// Output:
	// email_password true  []
	// passkeys false  [WEBAUTHN_RP_ID WEBAUTHN_ORIGINS]
	// github true callback https://api.example.com/v1/auth/github/callback []
}

func ExampleOption() {
	// cmd/api/main.go: v0.1's sign-in with a stricter password policy, a
	// module's role behind a second factor, and shorter-lived API keys.
	auth := authhttp.New(
		authhttp.MinPasswordLength(14),
		authhttp.RequireMFA("billing_admin"),
		authhttp.APIKeyMaxTTL(30*24*time.Hour),
	)
	gorbital.Main(gorbital.WithAuth(auth), gorbital.WithModules(modulesAll()...))
}

func ExampleMinPasswordLength() {
	err := authhttp.New(authhttp.MinPasswordLength(8)).CheckConfig(gorbital.Config{})
	fmt.Println(err)
	// Output:
	// authhttp: MinPasswordLength(8): a minimum is 12 to 128 characters; a lower one would weaken v0.1's policy
}

func ExamplePasswordPolicy() {
	// The error's text follows "the password": 422 weak_password,
	// "the password must not contain the app's name".
	noAppName := func(_ context.Context, password string) error {
		if strings.Contains(strings.ToLower(password), "shelfie") {
			return errors.New("must not contain the app's name")
		}
		return nil
	}
	_ = authhttp.New(authhttp.PasswordPolicy(noAppName))
}

func ExampleRequireMFA() {
	// billing_admin is declared by a module's permissions; its sessions need
	// a second factor, like platform_admin's.
	_ = authhttp.New(authhttp.RequireMFA("billing_admin"))
}

func ExampleAPIKeyMaxTTL() {
	// New API keys expire within 30 days; operators can shorten
	// auth.api_key_max_ttl, not lengthen it.
	_ = authhttp.New(authhttp.APIKeyMaxTTL(30 * 24 * time.Hour))
}

func ExampleWithoutRegistration() {
	// An internal tool: operators create accounts (POST /ops/auth/users);
	// POST /v1/auth/register answers 404 and a first Google sign-in 403
	// registration_closed.
	_ = authhttp.New(authhttp.WithoutRegistration())
}

func ExampleBrand() {
	_ = authhttp.New(authhttp.Brand(mail.Brand{
		LogoURL:      "https://shelfie.example/logo.png",
		SupportEmail: "help@shelfie.example",
		Footer:       "Shelfie Ltd, 1 Main Street, London",
	}))
}

func ExampleRouteMiddleware() {
	// A CAPTCHA check on sign-in's routes, with a problem code of the app's.
	captcha := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.Header.Get("X-Captcha-Token") == "" {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusBadRequest, "captcha_required", "solve the CAPTCHA first"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	_ = authhttp.New(authhttp.RouteMiddleware(captcha))
}

// errSuspended is declared once: Refuse panics on a reserved code when the
// program starts.
var errSuspended = authhttp.Refuse("reader_suspended", "this account is suspended; write to help@shelfie.example")

func ExampleBeforeLogin() {
	refuseSuspended := func(ctx context.Context, tx pgx.Tx, a authhttp.LoginAttempt) error {
		var suspended bool
		err := tx.QueryRow(ctx, `SELECT suspended FROM profiles WHERE user_id = $1`, a.User.ID).Scan(&suspended)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case err != nil:
			return err // 500: sign-in fails closed
		case suspended:
			return errSuspended // 403 reader_suspended
		}
		return nil
	}
	_ = authhttp.New(authhttp.BeforeLogin(refuseSuspended))
}

func ExampleAfterLogin() {
	logger := slog.Default()
	_ = authhttp.New(authhttp.AfterLogin(func(ctx context.Context, e authhttp.LoginEvent) error {
		logger.InfoContext(ctx, "signed in", "user_id", e.User.ID, "method", e.Method, "second_factor", e.SecondFactor)
		return nil
	}))
}

func ExampleOnRegister() {
	// Every new account gets a default shelf, whichever way it was created.
	defaultShelf := func(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount) error {
		_, err := tx.Exec(ctx, `INSERT INTO shelves (owner_id, name) VALUES ($1, 'Reading')`, a.User.ID)
		return err // rolls the account back
	}
	_ = authhttp.New(authhttp.OnRegister(defaultShelf))
}

// Profile is Shelfie's registration fields.
type Profile struct {
	DisplayName string `json:"display_name" minLength:"1" maxLength:"50"`
	Country     string `json:"country,omitempty" pattern:"^[A-Z]{2}$" doc:"ISO 3166-1 alpha-2 code"`
}

func ExampleRegisterFields() {
	// POST /v1/auth/register takes {"email", "password", "display_name",
	// "country"}; Huma refuses a missing display_name with 422 before any
	// account exists.
	save := func(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount, p Profile) error {
		_, err := tx.Exec(ctx, `INSERT INTO profiles (user_id, display_name, country) VALUES ($1, $2, $3)`, a.User.ID, p.DisplayName, p.Country)
		return err
	}
	_ = authhttp.New(authhttp.RegisterFields(save))
}

func ExampleRefuse() {
	err := authhttp.Refuse("reader_suspended", "this account is suspended")
	var refusal *authhttp.Refusal
	fmt.Println(errors.As(err, &refusal), refusal.Code)

	defer func() { fmt.Println(recover()) }()
	_ = authhttp.Refuse("account_banned", "sign-in's own code")
	// Output:
	// true reader_suspended
	// authhttp: refusal code "account_banned" is one of sign-in's or gorbital's own codes; choose a code of the app's
}

func ExampleRefusal() {
	// A hook can build the refusal's detail when it refuses. The code must
	// still be the app's: a reserved one answers 500 instead.
	refuse := func(until time.Time) error {
		return &authhttp.Refusal{Code: "reader_suspended", Detail: "suspended until " + until.Format(time.DateOnly)}
	}
	fmt.Println(refuse(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
	// Output:
	// authhttp: refused: reader_suspended: suspended until 2026-10-01
}

func ExampleUser() {
	// A module shows the account its own rows point to.
	var auth *authhttp.Authenticator // the one main.go passes to gorbital.WithAuth
	show := func(ctx context.Context, userID string) (string, error) {
		u, err := auth.User(ctx, userID)
		if err != nil {
			return "", err // authhttp.ErrUserNotFound answers 404 user_not_found
		}
		return u.Email, nil
	}
	_ = show
}

func ExampleLoginAttempt() {
	// Administrators sign in with a password and a second factor, never
	// with a module's phone code.
	errAdminsUsePasswords := authhttp.Refuse("admin_sign_in_method", "administrators sign in with a password and a second factor")
	_ = authhttp.New(authhttp.BeforeLogin(func(ctx context.Context, tx pgx.Tx, a authhttp.LoginAttempt) error {
		var admin bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM auth_user_roles WHERE user_id = $1 AND role = 'platform_admin')`, a.User.ID).Scan(&admin); err != nil {
			return err
		}
		if admin && (a.Method != "password" || a.SecondFactor == "") {
			return errAdminsUsePasswords
		}
		return nil
	}))
}

func ExampleLoginEvent() {
	_ = authhttp.New(authhttp.AfterLogin(func(ctx context.Context, e authhttp.LoginEvent) error {
		slog.InfoContext(ctx, "new session", "session_id", e.SessionID, "ip", e.IP)
		return nil
	}))
}

func ExampleNewAccount() {
	// Prefill a profile with the provider's name; email registrations get
	// theirs from RegisterFields.
	_ = authhttp.New(authhttp.OnRegister(func(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount) error {
		_, err := tx.Exec(ctx, `INSERT INTO profiles (user_id, display_name) VALUES ($1, $2) ON CONFLICT DO NOTHING`, a.User.ID, a.Name)
		return err
	}))
}

// phoneSignInInput is a module's sign-in route's input.
type phoneSignInInput struct {
	Body struct {
		Phone     string `json:"phone" pattern:"^\\+[1-9][0-9]{7,14}$"`
		Code      string `json:"code" pattern:"^[0-9]{6}$"`
		Transport string `json:"transport,omitempty" enum:"cookie,bearer" default:"cookie"`
	}
}

// verifyPhoneCode stands in for the module's use case, which checks the
// code and returns the account of the confirmed phone number.
func verifyPhoneCode(context.Context, string, string) (string, error) { return "usr_1", nil }

func ExampleAuthenticator_SignIn() {
	auth := authhttp.New()
	signIn := func(ctx context.Context, in *phoneSignInInput) (*authhttp.SignedIn, error) {
		userID, err := verifyPhoneCode(ctx, in.Body.Phone, in.Body.Code) // the module's own check, with its limits
		if err != nil {
			return nil, err
		}
		return auth.SignIn(ctx, authhttp.SignInRequest{UserID: userID, Method: "phone_code", Transport: in.Body.Transport})
	}
	module := gorbital.Module{
		Name: "phonelogin",
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			gorbital.Post(r, "/v1/phone-sign-in", signIn, gorbital.Summary("Sign in with a code sent to your phone"), guard.Public())
		},
	}
	gorbital.Main(gorbital.WithAuth(auth), gorbital.WithModules(module))
}

func ExampleSignInRequest() {
	req := authhttp.SignInRequest{UserID: "usr_mfrggzdfmztwq2lk", Method: "phone_code", Transport: "bearer"}
	fmt.Println(req.Method, req.Transport)
	// Output:
	// phone_code bearer
}

func ExampleSignedIn() {
	// A client reads a 202 as login's: finish with POST /v1/auth/login/mfa.
	answer := func(out *authhttp.SignedIn) string {
		if out.Status == http.StatusAccepted {
			return "second factor: " + strings.Join(out.Body.MFA.Methods, ", ")
		}
		return "signed in as " + out.Body.User.Email
	}
	_ = answer
}

func ExampleAuthenticator_User() {
	auth := authhttp.New()
	_, err := auth.User(context.Background(), "usr_1")
	fmt.Println(err)
	// Output:
	// authhttp: User called before Setup: pass the Authenticator to gorbital.WithAuth
}

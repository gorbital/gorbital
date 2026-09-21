# gorbital/authhttp

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital/authhttp"
```

Package authhttp is sign-in for apps on gorbital.Main: registration with email verification, passwords, sessions in a cookie or a bearer token, two-factor authentication with authenticator apps and passkeys, Google, Apple and GitHub sign-in, API keys and service accounts, platform roles, and the operators' account APIs under /ops/auth/users (ADR-0024, ADR-0038, ADR-0043 to ADR-0046, ADR-0058, ADR-0059, ADR-0070).

It is the auth module v0.1 apps generate into internal/modules/auth, moved into the library unchanged: the same endpoints, error codes, audit events, permissions, runtime settings, jobs, rate limiters, cookies and migrations, configured by the same environment variables ([gorbital.Config](gorbital.md#Config)). Add it in main.go:

```go
gorbital.Main(
	gorbital.WithAuth(authhttp.New()),
	gorbital.WithModules(modules.All()...),
)
```

[gorbital.New](gorbital.md#New) hands the authenticator its configuration and dependencies through [Authenticator.Setup](#Authenticator.Setup), and [gorbital.Main](gorbital.md#Main) adds its commands: roles, grant-role, revoke-role, reset-mfa, rotate-auth-keys and auth-providers.

Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).

## Contents

- Variables: [`ErrUserNotFound`](#ErrUserNotFound)
- Functions: [`Refuse`](#Refuse)
- Types:
  - [`Authenticator`](#Authenticator): [`New`](#New), [`Authenticator.CheckConfig`](#Authenticator.CheckConfig), [`Authenticator.Commands`](#Authenticator.Commands), [`Authenticator.Middleware`](#Authenticator.Middleware), [`Authenticator.Module`](#Authenticator.Module), [`Authenticator.OrgServiceAccountRoutes`](#Authenticator.OrgServiceAccountRoutes), [`Authenticator.Setup`](#Authenticator.Setup), [`Authenticator.SignIn`](#Authenticator.SignIn), [`Authenticator.SignInMethods`](#Authenticator.SignInMethods), [`Authenticator.UseOrganisations`](#Authenticator.UseOrganisations), [`Authenticator.User`](#Authenticator.User)
  - [`LoginAttempt`](#LoginAttempt)
  - [`LoginEvent`](#LoginEvent)
  - [`Method`](#Method): [`MethodPassword`](#MethodPassword), [`MethodOperators`](#MethodOperators), [`MethodTOTP`](#MethodTOTP), [`MethodPasskeys`](#MethodPasskeys), [`MethodSocial`](#MethodSocial), [`MethodAPIKeys`](#MethodAPIKeys)
  - [`NewAccount`](#NewAccount)
  - [`Option`](#Option): [`APIKeyMaxTTL`](#APIKeyMaxTTL), [`AfterLogin`](#AfterLogin), [`BeforeLogin`](#BeforeLogin), [`Brand`](#Brand), [`Methods`](#Methods), [`MinPasswordLength`](#MinPasswordLength), [`OnRegister`](#OnRegister), [`PasswordPolicy`](#PasswordPolicy), [`RegisterFields`](#RegisterFields), [`RequireMFA`](#RequireMFA), [`RouteMiddleware`](#RouteMiddleware), [`WithoutRegistration`](#WithoutRegistration)
  - [`Organisations`](#Organisations)
  - [`Refusal`](#Refusal): [`Refusal.Error`](#Refusal.Error)
  - [`SignInRequest`](#SignInRequest)
  - [`SignedIn`](#SignedIn)
  - [`User`](#User)

## Variables

<a id="ErrUserNotFound"></a>

```go
var ErrUserNotFound = authdomain.ErrUserNotFound
```

ErrUserNotFound is returned by [Authenticator.User](#Authenticator.User) for an unknown or deleted account. Returned from a handler, it answers 404 user\_not\_found.

*Since `v0.2.0 (unreleased)`*

## Functions

<a id="Refuse"></a>

### func Refuse

```go
func Refuse(code, detail string) error
```

Refuse returns the error a [BeforeLogin](#BeforeLogin), [OnRegister](#OnRegister) or [RegisterFields](#RegisterFields) hook returns to refuse with 403, code and detail. The code is lowercase snake\_case, 3 to 64 characters, and can't be one of sign-in's or gorbital's own codes (such as invalid\_credentials, account\_banned or mfa\_required), so clients can always tell the app's refusals apart. Refuse panics on such a code: declare refusals as package variables, so a wrong code stops the program when it starts, before gorbital.New:

```go
var errSuspended = authhttp.Refuse("reader_suspended", "this account is suspended; write to support")
```

*Since `v0.2.0 (unreleased)`*

**Example**

```go
err := authhttp.Refuse("reader_suspended", "this account is suspended")
var refusal *authhttp.Refusal
fmt.Println(errors.As(err, &refusal), refusal.Code)

defer func() { fmt.Println(recover()) }()
_ = authhttp.Refuse("account_banned", "sign-in's own code")
```

Output:

```text
true reader_suspended
authhttp: refusal code "account_banned" is one of sign-in's or gorbital's own codes; choose a code of the app's
```

## Types

<a id="Authenticator"></a>

### type Authenticator

```go
type Authenticator struct {
	// contains filtered or unexported fields
}
```

An Authenticator is sign-in for one app. It implements gorbital.Authenticator, with the optional methods gorbital.New and gorbital.Main look for. Create it with [New](#New) and pass it to gorbital.WithAuth; the app calls its methods.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

<a id="New"></a>

#### func New

```go
func New(opts ...Option) *Authenticator
```

New returns sign-in for an app, configured from the environment variables gorbital.LoadConfig reads (AUTH\_ENCRYPTION\_KEYS, WEBAUTHN\_\*, GOOGLE\_\*, APPLE\_\*, GITHUB\_\*, APP\_PUBLIC\_URL, AUTH\_DEFAULT\_RETURN\_TO): each sign-in method is on when its variables are set. Use one Authenticator per app.

Without options it is v0.1's sign-in. Options change the password policy ([MinPasswordLength](#MinPasswordLength), [PasswordPolicy](#PasswordPolicy)), second factors ([RequireMFA](#RequireMFA)), API keys ([APIKeyMaxTTL](#APIKeyMaxTTL)), sign-up ([WithoutRegistration](#WithoutRegistration), [RegisterFields](#RegisterFields)), emails ([Brand](#Brand)) and the routes ([RouteMiddleware](#RouteMiddleware)), and add hooks ([BeforeLogin](#BeforeLogin), [AfterLogin](#AfterLogin), [OnRegister](#OnRegister)). [Authenticator.CheckConfig](#Authenticator.CheckConfig) reports an invalid option.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

<a id="Authenticator.CheckConfig"></a>

#### func (*Authenticator) CheckConfig

```go
func (a *Authenticator) CheckConfig(cfg gorbital.Config) error
```

CheckConfig checks what sign-in needs its own packages for, which gorbital.LoadConfig leaves to it: AUTH\_ENCRYPTION\_KEYS is required in production; APPLE\_PRIVATE\_KEY (or APPLE\_PRIVATE\_KEY\_FILE) must be an Apple sign-in key; WEBAUTHN\_APPLE\_APP\_IDS and WEBAUTHN\_ANDROID\_APPS must parse; and WEBAUTHN\_ORIGINS must be on WEBAUTHN\_RP\_ID. It returns every problem joined, with v0.1's messages. gorbital.New and gorbital.Main call it before anything connects, and exit with status 2 on its error.

It also reports the options [New](#New) received that can't apply, such as [MinPasswordLength](#MinPasswordLength) below 12, after the configuration's problems.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

Output:

```text
AUTH_ENCRYPTION_KEYS is required in production: it encrypts two-factor authentication secrets (generate one: echo "k1:$(openssl rand -base64 32)")
WEBAUTHN_RP_ID and WEBAUTHN_ORIGINS: passkey: invalid configuration: origin "https://example.org" isn't on the relying party ID "example.com" or a subdomain of it
```

<a id="Authenticator.Commands"></a>

#### func (*Authenticator) Commands

```go
func (a *Authenticator) Commands() []gorbital.Command
```

Commands returns sign-in's commands, which gorbital.Main serves beside its own, with the arguments, output and messages of a v0.1 app's cmd/api:

```
roles                        list the platform roles and their permissions
grant-role <email> <role>    give an account a platform role
revoke-role <email> <role>   take a platform role away
reset-mfa <email>            turn off an account's two-factor authentication
rotate-auth-keys             re-encrypt 2FA secrets with the first AUTH_ENCRYPTION_KEYS key
auth-providers               show which sign-in methods are configured
seed [--email <email>]       create a development administrator with two-factor authentication
```

Changes are recorded in the audit log as the "cli" system actor. Wrong arguments exit with status 2 (gorbital.ErrUsage), as every command of gorbital.Main does; other failures, such as an unknown account, with 1.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
for _, c := range authhttp.New().Commands() {
	fmt.Println(c.Usage)
}
```

Output:

```text
roles                          list the platform roles and their permissions
grant-role <email> <role>      give an account a platform role
revoke-role <email> <role>     take a platform role away
reset-mfa <email>              turn off an account's two-factor authentication
rotate-auth-keys               re-encrypt 2FA secrets with the first AUTH_ENCRYPTION_KEYS key
auth-providers                 show which sign-in methods are configured
seed [--email <email>]         create a development administrator with 2FA (orb dev runs it)
```

<a id="Authenticator.Middleware"></a>

#### func (*Authenticator) Middleware

```go
func (a *Authenticator) Middleware(logger *slog.Logger) func(http.Handler) http.Handler
```

Middleware authenticates each request with its session cookie (\_\_Host-session) or bearer token, a session token or an API key, and sets the principal (auth.WithPrincipal) for the handlers and guards after it. A request without valid credentials passes on without an actor; one with a malformed, unknown or wrong API key counts against the auth.api\_key\_failures\_per\_minute limit of its network.

gorbital.New calls it after [Authenticator.Setup](#Authenticator.Setup); it panics before.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// gorbital.New puts the middleware at the Auth step of the stack; a
// custom order keeps it before the steps that read the caller.
gorbital.WithStack(func(s gorbital.Stack) []func(http.Handler) http.Handler {
	steps := s.Default()
	// ... reorder or add steps; s.Auth is authhttp's Middleware.
	return steps
})
_ = slog.Default()
```

<a id="Authenticator.Module"></a>

#### func (*Authenticator) Module

```go
func (a *Authenticator) Module() gorbital.Module
```

Module returns sign-in as a gorbital module, which gorbital.New adds before the app's modules: its routes under /v1/auth/, /ops/auth/users and /ops/service-accounts, its error mappings, permissions, runtime settings (auth.\*), the auth\_cleanup and auth\_revoke\_tokens jobs, its rate limiters and the retention of deleted and unverified accounts (which the operations API lists in /ops/auth/rate-limits and /ops/retention), and its migrations under the versions v0.1 apps hold them under, so a v0.1 database migrates as a no-op.

Everything but the migrations is what the app's sign-in methods declare ([Methods](#Methods)): a method it doesn't serve has no routes, settings, jobs, limiters or permissions, so /ops lists none of them. The migrations are applied whichever methods are served (ADR-0089).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
m := authhttp.New().Module()
fmt.Println(m.Name)
for _, p := range m.Permissions {
	fmt.Println(p.Name, p.Roles)
}
fmt.Println(m.Migrations[0].Version, m.Migrations[len(m.Migrations)-1].Version)
```

Output:

```text
auth
ops.auth.read [platform_admin ops_viewer]
ops.auth.write [platform_admin]
ops.service_accounts.read [platform_admin ops_viewer]
ops.service_accounts.write [platform_admin]
20260915000001 20260918000070
```

<a id="Authenticator.OrgServiceAccountRoutes"></a>

#### func (*Authenticator) OrgServiceAccountRoutes

```go
func (a *Authenticator) OrgServiceAccountRoutes(r *gorbital.Router)
```

OrgServiceAccountRoutes registers the operations on organisations' service accounts and their API keys under /v1/orgs/{orgId}/service-accounts on r, with v0.1's operation IDs, schemas and error codes (ADR-0058). The organisations module registers them; they work once [Authenticator.UseOrganisations](#Authenticator.UseOrganisations) is called, and before [Authenticator.Setup](#Authenticator.Setup) they register for the OpenAPI document only. An app that doesn't serve [MethodAPIKeys](#MethodAPIKeys) has no service accounts, so it registers none of them.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// The organisations module registers organisations' service accounts
// with its own routes.
auth := authhttp.New()
orgs := gorbital.Module{
	Name:   "orgs",
	Routes: func(r *gorbital.Router, _ gorbital.Deps) { auth.OrgServiceAccountRoutes(r) },
}
api, mux := exampleAPI()
if err := gorbital.Mount(api, nil, gorbital.Deps{}, orgs); err != nil {
	panic(err)
}
for _, path := range []string{"/v1/orgs/{orgId}/service-accounts", "/v1/orgs/{orgId}/service-accounts/{id}/keys"} {
	item := api.OpenAPI().Paths[path]
	fmt.Println(item.Get.OperationID, item.Post.OperationID)
}
_ = http.Handler(mux)
```

Output:

```text
orgs-list-service-accounts orgs-create-service-account
orgs-list-service-account-keys orgs-create-service-account-key
```

<a id="Authenticator.Setup"></a>

#### func (*Authenticator) Setup

```go
func (a *Authenticator) Setup(ctx context.Context, s gorbital.AuthSetup) error
```

Setup builds sign-in from the app's configuration and dependencies: the use cases on s.Deps.DB, the rate limiters shared through s.Deps.RateLimits, email through s.Deps.Mailer with the app's brand, the sign-in providers that are configured, and the roles it relies on in s.Permissions: the user role every account holds, and a second factor required for platform\_admin and ops\_viewer, whose permissions reach /ops. It freezes the catalog, serves the /.well-known files passkeys in iOS and Android apps need when those apps are configured, and adds sign-in's emails to the dev console's previews. Impersonation is available only when s.DevConsole is set.

gorbital.New calls it once; gorbital.Main calls it with zero Deps before one of [Authenticator.Commands](#Authenticator.Commands) runs, which then opens its own database connection. It returns an error when called a second time with a database, or when a dependency sign-in needs is missing.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// gorbital.New and gorbital.Main call Setup; an app never does. A
// wrapper that adds to sign-in passes the call on.
type auditedAuth struct{ *authhttp.Authenticator }
setup := func(ctx context.Context, a auditedAuth, s gorbital.AuthSetup) error {
	s.Deps.Logger.InfoContext(ctx, "sign-in starting", "app", s.Name, "dev_console", s.DevConsole)
	return a.Setup(ctx, s)
}
_ = setup
```

<a id="Authenticator.SignIn"></a>

#### func (*Authenticator) SignIn

```go
func (a *Authenticator) SignIn(ctx context.Context, req SignInRequest) (*SignedIn, error)
```

SignIn signs an account in with a module's own method and returns what POST /v1/auth/login returns after a correct password, so a module's sign-in route returns it as its output:

  - the account's login rate limits (auth\_login, auth\_login\_address) are charged: 429 too\_many\_attempts;
  - an account whose address isn't verified gets 403 email\_not\_verified;
  - an account with two-factor authentication gets 202 and a challenge;
  - a banned account gets 403 account\_banned;
  - the [BeforeLogin](#BeforeLogin) hooks run, then the session is created, audited as auth.login.succeeded with the method in its metadata, and the [AfterLogin](#AfterLogin) hooks run;
  - an unknown or deleted account gets 401 invalid\_credentials.

SignIn doesn't check anything about the method itself: the module must have verified, before calling it, that the person controls a credential bound to req.UserID — a single-use code sent to a phone number the account confirmed, a signature from a key it registered — with its own attempt limits (guard.RateLimit) and without revealing whether an account exists. Never call it with an ID taken from the request.

The errors are problems (or sign-in's mapped errors) a handler returns as they are. Calling SignIn before gorbital.New has set sign-in up, or with an invalid Method or Transport, returns a plain error (500).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

<a id="Authenticator.SignInMethods"></a>

#### func (*Authenticator) SignInMethods

```go
func (a *Authenticator) SignInMethods(cfg gorbital.Config) []gorbital.SignInMethod
```

SignInMethods reports each sign-in method a v0.1 app has, whether cfg configures it and, for the ones that are off, the environment variables that turn them on and the section of AUTH\_PROVIDERS.md that explains them (ADR-0045). It never includes secret values. The operations API lists them in GET /ops/auth/providers (gorbital.Platform.SignInMethods), and the auth-providers command prints them.

A method the app doesn't serve ([Methods](#Methods)) is reported off with no variables to set and a Detail saying so: naming variables that would change nothing would tell an operator to set them (ADR-0089).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

Output:

```text
email_password true  []
passkeys false  [WEBAUTHN_RP_ID WEBAUTHN_ORIGINS]
github true callback https://api.example.com/v1/auth/github/callback []
```

<a id="Authenticator.UseOrganisations"></a>

#### func (*Authenticator) UseOrganisations

```go
func (a *Authenticator) UseOrganisations(o Organisations) error
```

UseOrganisations connects sign-in to the app's organisations: new accounts and deleted ones reach o, and the operations of [Authenticator.OrgServiceAccountRoutes](#Authenticator.OrgServiceAccountRoutes) manage the service accounts of organisations o authorizes. Until it is called, accounts are created and deleted without organisations, and organisations have no service accounts (404 service\_account\_not\_found). A later call replaces o.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
auth := authhttp.New()
if err := auth.UseOrganisations(nil); err != nil {
	fmt.Println(err)
}
fmt.Println(auth.UseOrganisations(orgDirectory{}))
```

Output:

```text
authhttp: UseOrganisations: the organisations are nil
<nil>
```

<a id="Authenticator.User"></a>

#### func (*Authenticator) User

```go
func (a *Authenticator) User(ctx context.Context, id string) (User, error)
```

User returns an account with its platform roles, or [ErrUserNotFound](#ErrUserNotFound). A module uses it to show or check the account its own records point to.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
auth := authhttp.New()
_, err := auth.User(context.Background(), "usr_1")
fmt.Println(err)
```

Output:

```text
authhttp: User called before Setup: pass the Authenticator to gorbital.WithAuth
```

<a id="LoginAttempt"></a>
<a id="LoginAttempt.User"></a>
<a id="LoginAttempt.Method"></a>
<a id="LoginAttempt.SecondFactor"></a>
<a id="LoginAttempt.IP"></a>
<a id="LoginAttempt.UserAgent"></a>

### type LoginAttempt

```go
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
```

A LoginAttempt is a sign-in whose every factor is verified, just before its session is created, as [BeforeLogin](#BeforeLogin) hooks receive it: a correct password for a verified address, a passkey, a Google, Apple or GitHub identity, or a module's method through [Authenticator.SignIn](#Authenticator.SignIn), and, for an account with two-factor authentication, the second factor too. A banned account is refused before the hooks run. The hooks never see unknown addresses, wrong passwords or failed second factors, so they can't tell anyone whether an address has an account, and a refusal is only ever shown to someone who passed every check. Impersonation in development doesn't run them.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

<a id="LoginEvent"></a>
<a id="LoginEvent.LoginAttempt"></a>
<a id="LoginEvent.SessionID"></a>

### type LoginEvent

```go
type LoginEvent struct {
	LoginAttempt
	SessionID string
}
```

A LoginEvent is a sign-in whose session is committed and audited, as [AfterLogin](#AfterLogin) hooks receive it. It never carries the session's token.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_ = authhttp.New(authhttp.AfterLogin(func(ctx context.Context, e authhttp.LoginEvent) error {
	slog.InfoContext(ctx, "new session", "session_id", e.SessionID, "ip", e.IP)
	return nil
}))
```

<a id="Method"></a>

### type Method

```go
type Method string
```

A Method is one way of signing in, with the operations, runtime settings, jobs, rate limiters and permissions that belong to it. [Methods](#Methods) chooses the ones an app serves. The values are public API: the operations API and the CLI's profiles name them.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// The methods are values, so an app can read its own from
// configuration. MethodPassword is always among them.
methods := []authhttp.Method{authhttp.MethodPassword, authhttp.MethodTOTP, authhttp.MethodPasskeys}
_ = authhttp.New(authhttp.Methods(methods...))
```

<a id="MethodPassword"></a>
<a id="MethodOperators"></a>
<a id="MethodTOTP"></a>
<a id="MethodPasskeys"></a>
<a id="MethodSocial"></a>
<a id="MethodAPIKeys"></a>

```go
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
```

The sign-in methods [Methods](#Methods) chooses from. Their migrations are applied whichever are served, so the tables of a method an app leaves out exist and stay empty, and adding the method later is one line in main.go (ADR-0089).

*Since `v0.2.0 (unreleased)`*

<a id="NewAccount"></a>
<a id="NewAccount.User"></a>
<a id="NewAccount.Method"></a>
<a id="NewAccount.Name"></a>
<a id="NewAccount.IP"></a>
<a id="NewAccount.UserAgent"></a>

### type NewAccount

```go
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
```

A NewAccount is an account being created, in the transaction that creates it, as [OnRegister](#OnRegister) and [RegisterFields](#RegisterFields) hooks receive it.

What a hook's error does depends on how the account is created:

  - password (POST /v1/auth/register): the account is rolled back, the error logged, and the response is still 202. Registration answers the same whether or not the address has an account, and hooks run only for new ones, so any other answer would reveal that. Refuse bad input with RegisterFields' validation or a [PasswordPolicy](#PasswordPolicy) instead.
  - google, apple, github (a first sign-in): the account is rolled back; a [Refuse](#Refuse) error answers 403 with its code (in the redirect's fragment for web sign-ins) and any other error 500. The provider already proved the identity, so nothing is revealed.
  - operator (POST /ops/auth/users, orb dev's seed data): the account is rolled back; 403 with a refusal's code, otherwise 500.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// Prefill a profile with the provider's name; email registrations get
// theirs from RegisterFields.
_ = authhttp.New(authhttp.OnRegister(func(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount) error {
	_, err := tx.Exec(ctx, `INSERT INTO profiles (user_id, display_name) VALUES ($1, $2) ON CONFLICT DO NOTHING`, a.User.ID, a.Name)
	return err
}))
```

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option changes sign-in from v0.1's behaviour, which [New](#New) keeps when it gets none (ADR-0083, Phase 6). Deployment values, such as provider credentials and the passkey relying party, stay in environment variables. A mistake in an option, such as a password length below the minimum, is reported by [Authenticator.CheckConfig](#Authenticator.CheckConfig), so the app exits with status 2 before it connects.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// cmd/api/main.go: v0.1's sign-in with a stricter password policy, a
// module's role behind a second factor, and shorter-lived API keys.
auth := authhttp.New(
	authhttp.MinPasswordLength(14),
	authhttp.RequireMFA("billing_admin"),
	authhttp.APIKeyMaxTTL(30*24*time.Hour),
)
gorbital.Main(gorbital.WithAuth(auth), gorbital.WithModules(modulesAll()...))
```

<a id="APIKeyMaxTTL"></a>

#### func APIKeyMaxTTL

```go
func APIKeyMaxTTL(d time.Duration) Option
```

APIKeyMaxTTL caps the lifetime of new API keys: the runtime setting auth.api\_key\_max\_ttl, which operators change, accepts at most d (instead of a year) and defaults to d when d is shorter than its 90-day default. d is 24 hours to 365 days. Existing keys keep their expiry.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// New API keys expire within 30 days; operators can shorten
// auth.api_key_max_ttl, not lengthen it.
_ = authhttp.New(authhttp.APIKeyMaxTTL(30 * 24 * time.Hour))
```

<a id="AfterLogin"></a>

#### func AfterLogin

```go
func AfterLogin(hook func(ctx context.Context, e LoginEvent) error) Option
```

AfterLogin adds a hook that runs after a session is created (see [LoginEvent](#LoginEvent)). It can't change the sign-in: its error is logged, and the response waits for it at most 5 seconds, after which its context is cancelled. A panic is recovered and logged. Several hooks run in order.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
logger := slog.Default()
_ = authhttp.New(authhttp.AfterLogin(func(ctx context.Context, e authhttp.LoginEvent) error {
	logger.InfoContext(ctx, "signed in", "user_id", e.User.ID, "method", e.Method, "second_factor", e.SecondFactor)
	return nil
}))
```

<a id="BeforeLogin"></a>

#### func BeforeLogin

```go
func BeforeLogin(hook func(ctx context.Context, tx pgx.Tx, a LoginAttempt) error) Option
```

BeforeLogin adds a hook that decides whether a sign-in may start a session (see [LoginAttempt](#LoginAttempt) for when it runs). Return [Refuse](#Refuse)'s error to refuse it with 403 and your code; any other error fails the sign-in with 500 internal\_error, and nil lets it through. tx is the transaction that creates the session: read the app's tables in it, and what the hook writes commits only with the session. Several hooks run in the order given, until one returns an error.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

<a id="Brand"></a>

#### func Brand

```go
func Brand(b mail.Brand) Option
```

Brand sets what every email sign-in sends has in common (mail.Brand): a logo, the support address, a footer line. An empty Name is the app's name (gorbital.WithName) and an empty URL is APP\_PUBLIC\_URL, as without the option. The dev console's previews use it too.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_ = authhttp.New(authhttp.Brand(mail.Brand{
	LogoURL:      "https://shelfie.example/logo.png",
	SupportEmail: "help@shelfie.example",
	Footer:       "Shelfie Ltd, 1 Main Street, London",
}))
```

<a id="Methods"></a>

#### func Methods

```go
func Methods(m ...Method) Option
```

Methods chooses the sign-in methods the app serves, such as authhttp.Methods(authhttp.MethodPassword, authhttp.MethodOperators) for an app that wants an email address, a password and the operators' account APIs. Without the option every method is served, which is v0.2's sign-in byte for byte.

A method the app doesn't serve has no operations, so its paths are 404 and they leave the OpenAPI document, and it declares no runtime settings, jobs, rate limiters or permissions, so /ops doesn't list them and no role can be granted them. Its migrations are applied all the same: the tables exist and stay empty, so adding the method later is this one line and a restart (ADR-0089).

[MethodPassword](#MethodPassword) is required: this package is the password implementation, and an app that doesn't want passwords wants another authenticator. A set without it, or with a method that doesn't exist, is reported by [Authenticator.CheckConfig](#Authenticator.CheckConfig), so the app exits with status 2 before it connects.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// An app with an email address, a password and the operators' account
// APIs: 29 operations instead of 74, and no passkey, provider or API
// key settings, jobs, limiters or permissions in /ops.
_ = authhttp.New(authhttp.Methods(authhttp.MethodPassword, authhttp.MethodOperators))
```

<a id="MinPasswordLength"></a>

#### func MinPasswordLength

```go
func MinPasswordLength(n int) Option
```

MinPasswordLength raises the shortest password accepted when an account registers, resets or changes its password, or an operator creates one, from 12 characters (auth.MinPasswordLength) up to at most 128 (auth.MaxPasswordLength). Lengths count characters, not bytes. A shorter password gets 422 weak\_password, "the password must be at least n characters". Existing passwords keep working.

The OpenAPI document states n too, wherever a password field documents the minimum: registration, the password reset, the password change and the operators' account creation.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
err := authhttp.New(authhttp.MinPasswordLength(8)).CheckConfig(gorbital.Config{})
fmt.Println(err)
```

Output:

```text
authhttp: MinPasswordLength(8): a minimum is 12 to 128 characters; a lower one would weaken v0.1's policy
```

<a id="OnRegister"></a>

#### func OnRegister

```go
func OnRegister(hook func(ctx context.Context, tx pgx.Tx, a NewAccount) error) Option
```

OnRegister adds a hook that runs in the transaction that creates an account, for every way one is created (see [NewAccount](#NewAccount)). Write the app's rows for the account in tx: a profile, a default workspace, a job with jobs.Client.InsertTx. An error rolls the account back; see [NewAccount](#NewAccount) for what the client receives. Several hooks run in order, until one returns an error.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// Every new account gets a default shelf, whichever way it was created.
defaultShelf := func(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount) error {
	_, err := tx.Exec(ctx, `INSERT INTO shelves (owner_id, name) VALUES ($1, 'Reading')`, a.User.ID)
	return err // rolls the account back
}
_ = authhttp.New(authhttp.OnRegister(defaultShelf))
```

<a id="PasswordPolicy"></a>

#### func PasswordPolicy

```go
func PasswordPolicy(check func(ctx context.Context, password string) error) Option
```

PasswordPolicy adds a check every new password must pass after the built-in rules (length, not blank) and [MinPasswordLength](#MinPasswordLength), such as a breached-password lookup or a ban on the app's name. A non-nil error refuses the password with 422 weak\_password and the detail "the password " followed by the error's text, so word it to follow: "is too common". Several policies run in order. A policy runs before anything is stored and for every request, so its result can't reveal whether an address has an account; it must not log the password.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// The error's text follows "the password": 422 weak_password,
// "the password must not contain the app's name".
noAppName := func(_ context.Context, password string) error {
	if strings.Contains(strings.ToLower(password), "shelfie") {
		return errors.New("must not contain the app's name")
	}
	return nil
}
_ = authhttp.New(authhttp.PasswordPolicy(noAppName))
```

<a id="RegisterFields"></a>

#### func RegisterFields

```go
func RegisterFields[T any](save func(ctx context.Context, tx pgx.Tx, a NewAccount, fields T) error) Option
```

RegisterFields adds the fields of T, a struct, to POST /v1/auth/register beside email and password, and passes them to save in the transaction that creates the account, after the [OnRegister](#OnRegister) hooks. Huma validates them from T's tags (required unless omitempty, maxLength, enum, pattern…) and a Resolve method on \*T, for every request and before anything is stored, and the OpenAPI document shows them. Properties the body doesn't name are still accepted, as in v0.1.

Only email registration sends them: an account created by a first Google, Apple or GitHub sign-in, or by an operator, runs the OnRegister hooks without save. Let those users complete their profile later.

save's error rolls the account back, like OnRegister's; the response is still 202 (see [NewAccount](#NewAccount)), so validate in T, not in save. T's JSON names can't be email or password, and RegisterFields can be given once.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// POST /v1/auth/register takes {"email", "password", "display_name",
// "country"}; Huma refuses a missing display_name with 422 before any
// account exists.
save := func(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount, p Profile) error {
	_, err := tx.Exec(ctx, `INSERT INTO profiles (user_id, display_name, country) VALUES ($1, $2, $3)`, a.User.ID, p.DisplayName, p.Country)
	return err
}
_ = authhttp.New(authhttp.RegisterFields(save))
```

<a id="RequireMFA"></a>

#### func RequireMFA

```go
func RequireMFA(roles ...string) Option
```

RequireMFA grants the permissions of roles only to sessions signed in with a second factor, as sign-in always does for platform\_admin and ops\_viewer: a session without one gets 403 mfa\_required from routes those roles open, and an API key never holds them. A role must be declared by a module (gorbital.Module.Permissions); the user role every account holds can't require a second factor. Setup returns an error otherwise.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// billing_admin is declared by a module's permissions; its sessions need
// a second factor, like platform_admin's.
_ = authhttp.New(authhttp.RequireMFA("billing_admin"))
```

<a id="RouteMiddleware"></a>

#### func RouteMiddleware

```go
func RouteMiddleware(middleware ...func(http.Handler) http.Handler) Option
```

RouteMiddleware runs middleware on every operation under /v1/auth/, such as a CAPTCHA check on registration and sign-in or a country filter, after the app's middleware stack and before sign-in's own checks. It doesn't run on /ops/auth/users or /ops/service-accounts. Refuse with a problem (httpx.WriteProblem) and a code of your own. Middleware for the whole app goes in gorbital.WithMiddleware.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

<a id="WithoutRegistration"></a>

#### func WithoutRegistration

```go
func WithoutRegistration() Option
```

WithoutRegistration closes sign-up, for apps whose accounts come from operators or the app's own flows: POST /v1/auth/register isn't served (404, and it leaves the OpenAPI document), and a first Google, Apple or GitHub sign-in of an address without an account gets 403 registration\_closed (in the redirect's fragment for web sign-ins). Every other flow still works: accounts operators create (POST /ops/auth/users, orb dev's seed data), their verification and password reset, sign-in and linking providers to existing accounts, and custom methods through [Authenticator.SignIn](#Authenticator.SignIn).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// An internal tool: operators create accounts (POST /ops/auth/users);
// POST /v1/auth/register answers 404 and a first Google sign-in 403
// registration_closed.
_ = authhttp.New(authhttp.WithoutRegistration())
```

<a id="Organisations"></a>
<a id="Organisations.AccountCreated"></a>
<a id="Organisations.CheckAccountDeletion"></a>
<a id="Organisations.AccountDeleted"></a>
<a id="Organisations.AuthorizeServiceAccounts"></a>
<a id="Organisations.CanAssignServiceAccountRole"></a>
<a id="Organisations.Catalog"></a>

### type Organisations

```go
type Organisations interface {
	// AccountCreated runs after an account is created: registration, a
	// first Google, Apple or GitHub sign-in, or an operator's CreateUser.
	// Its error is logged, not returned: the account exists either way.
	AccountCreated(ctx context.Context, userID string) error
	// CheckAccountDeletion runs before the signed-in user's account is
	// deleted. An error stops the deletion and is returned as it is.
	CheckAccountDeletion(ctx context.Context, userID string) error
	// AccountDeleted runs after an account is deleted. Its error is logged.
	AccountDeleted(ctx context.Context, userID string) error
	// AuthorizeServiceAccounts checks that the signed-in user is a member of
	// orgID whose role may manage its service accounts, and returns a
	// context acting in the organisation and the user's role there.
	AuthorizeServiceAccounts(ctx context.Context, orgID string) (context.Context, string, error)
	// CanAssignServiceAccountRole reports whether a member with callerRole
	// may give a service account role, or manage one that has it. The owner
	// role is never allowed.
	CanAssignServiceAccountRole(callerRole, role string) bool
	// Catalog returns the organisation catalog.
	Catalog() *authlib.Catalog
}
```

Organisations is what sign-in needs from an app's organisations (ADR-0048, ADR-0058): taking part in creating and deleting accounts, and deciding who manages an organisation's service accounts, which sign-in stores and authenticates. gorbital.dev/gorbital/orgshttp implements it and connects it with [Authenticator.UseOrganisations](#Authenticator.UseOrganisations); apps don't call either.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// orgshttp connects its organisations to sign-in from its module's
// Platform function; an app only passes the authenticator on.
auth := authhttp.New()
var orgs authhttp.Organisations = orgDirectory{}
fmt.Println(auth.UseOrganisations(orgs))
```

Output:

```text
<nil>
```

<a id="Refusal"></a>
<a id="Refusal.Code"></a>
<a id="Refusal.Detail"></a>

### type Refusal

```go
type Refusal struct {
	Code   string
	Detail string
}
```

A Refusal is a hook refusing a sign-in or an account: the client receives 403 with Code and Detail as a problem. Create one with [Refuse](#Refuse).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// A hook can build the refusal's detail when it refuses. The code must
// still be the app's: a reserved one answers 500 instead.
refuse := func(until time.Time) error {
	return &authhttp.Refusal{Code: "reader_suspended", Detail: "suspended until " + until.Format(time.DateOnly)}
}
fmt.Println(refuse(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
```

Output:

```text
authhttp: refused: reader_suspended: suspended until 2026-10-01
```

<a id="Refusal.Error"></a>

#### func (*Refusal) Error

```go
func (r *Refusal) Error() string
```

Error returns the code and detail.

*Since `v0.2.0 (unreleased)`*

<a id="SignInRequest"></a>
<a id="SignInRequest.UserID"></a>
<a id="SignInRequest.Method"></a>
<a id="SignInRequest.Transport"></a>

### type SignInRequest

```go
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
```

A SignInRequest signs an account in with a module's own method, once the module has verified the person controls it (see [Authenticator.SignIn](#Authenticator.SignIn)).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
req := authhttp.SignInRequest{UserID: "usr_mfrggzdfmztwq2lk", Method: "phone_code", Transport: "bearer"}
fmt.Println(req.Method, req.Transport)
```

Output:

```text
phone_code bearer
```

<a id="SignedIn"></a>

### type SignedIn

```go
type SignedIn = delivery.LoginOutput
```

SignedIn is the response of a sign-in, the same as POST /v1/auth/login's: Status 200 with the session (Body.User, Body.Session, and Body.Token for transport bearer, or the cookie in SetCookie), or 202 with Body.MFA (challenge\_token, methods, expires\_at) for an account with two-factor authentication, which the client finishes with POST /v1/auth/login/mfa. Return it from a Huma handler as is: its body schema is login's LoginResponse.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// A client reads a 202 as login's: finish with POST /v1/auth/login/mfa.
answer := func(out *authhttp.SignedIn) string {
	if out.Status == http.StatusAccepted {
		return "second factor: " + strings.Join(out.Body.MFA.Methods, ", ")
	}
	return "signed in as " + out.Body.User.Email
}
_ = answer
```

<a id="User"></a>
<a id="User.ID"></a>
<a id="User.Email"></a>
<a id="User.EmailVerified"></a>
<a id="User.HasPassword"></a>
<a id="User.Banned"></a>
<a id="User.CreatedAt"></a>
<a id="User.Roles"></a>

### type User

```go
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
```

A User is an account, as hooks and modules see it. It never carries the password hash.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

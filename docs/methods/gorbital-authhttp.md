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

- Types:
  - [`Authenticator`](#Authenticator): [`New`](#New), [`Authenticator.CheckConfig`](#Authenticator.CheckConfig), [`Authenticator.Commands`](#Authenticator.Commands), [`Authenticator.Middleware`](#Authenticator.Middleware), [`Authenticator.Module`](#Authenticator.Module), [`Authenticator.Setup`](#Authenticator.Setup), [`Authenticator.SignInMethods`](#Authenticator.SignInMethods)

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
func New() *Authenticator
```

New returns sign-in for an app, configured from the environment variables gorbital.LoadConfig reads (AUTH\_ENCRYPTION\_KEYS, WEBAUTHN\_\*, GOOGLE\_\*, APPLE\_\*, GITHUB\_\*, APP\_PUBLIC\_URL, AUTH\_DEFAULT\_RETURN\_TO): each sign-in method is on when its variables are set. Use one Authenticator per app.

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

<a id="Authenticator.SignInMethods"></a>

#### func (*Authenticator) SignInMethods

```go
func (a *Authenticator) SignInMethods(cfg gorbital.Config) []gorbital.SignInMethod
```

SignInMethods reports each sign-in method a v0.1 app has, whether cfg configures it and, for the ones that are off, the environment variables that turn them on and the section of AUTH\_PROVIDERS.md that explains them (ADR-0045). It never includes secret values. The operations API lists them in GET /ops/auth/providers (gorbital.Platform.SignInMethods), and the auth-providers command prints them.

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

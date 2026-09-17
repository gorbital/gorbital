# gorbital

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital"
```

Package gorbital composes gorbital's modules into an application (ADR-0081). An app's main.go is one call to [Main](#Main), which loads the configuration from the environment ([LoadConfig](#LoadConfig)), builds the app ([New](#New)) and serves it ([App.Run](#App.Run)), or migrates its database ([Migrate](#Migrate)):

```go
func main() {
	gorbital.Main(
		gorbital.WithModules(modules.All()...),
		gorbital.WithMigrations(migrations.FS),
	)
}
```

A [Module](#Module) declares one feature of an app: its routes, error mappings, permissions, runtime settings and feature flags. [Declare](#Declare) adds the declarations to the app's registries before their stores are built, and [Mount](#Mount) registers the error mappings and routes on the app's API.

Routes are declared with the generic functions [Get](#Get), [Post](#Post), [Put](#Put), [Patch](#Patch) and [Delete](#Delete) on a [Router](#Router), so handlers keep their typed input and output, and with them request validation and the OpenAPI document. Every route requires an authenticated actor unless it has guard.Public() (ADR-0082):

```go
func Module() gorbital.Module {
	return gorbital.Module{
		Name:   "books",
		Errors: []httpx.Mapping{{Err: ErrISBNTaken, Status: http.StatusConflict, Code: "isbn_taken"}},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			h := &handler{db: d.DB, audit: d.Audit}
			books := r.Group("/v1/books", gorbital.Tags("Books"))
			gorbital.Post(books, "", h.createBook, gorbital.Status(http.StatusCreated))
			gorbital.Get(books, "/{id}", h.getBook)
		},
	}
}
```

gorbital's own modules are packages of this module, added the same way: gorbital.dev/gorbital/opshttp (the operations API under /ops/), gorbital.dev/gorbital/flagshttp (GET /v1/flags) and gorbital.dev/gorbital/mailevents (the email provider's webhook). They read what the app built for all modules through [Module.Platform](#Module.Platform) (ADR-0083).

Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).

## Contents

- Constants: [`MailDevMail`](#MailDevMail), [`MailMailpit`](#MailMailpit), [`MailProvider`](#MailProvider), [`StorageLocal`](#StorageLocal), [`StorageS3`](#StorageS3), [`StorageSpaces`](#StorageSpaces), [`StorageR2`](#StorageR2), [`StorageMinIO`](#StorageMinIO)
- Variables: [`ErrUsage`](#ErrUsage)
- Functions: [`Declare`](#Declare), [`Delete`](#Delete), [`Get`](#Get), [`Grants`](#Grants), [`Main`](#Main), [`Migrate`](#Migrate), [`Mount`](#Mount), [`Patch`](#Patch), [`Post`](#Post), [`Put`](#Put)
- Types:
  - [`App`](#App): [`New`](#New), [`App.Close`](#App.Close), [`App.Deps`](#App.Deps), [`App.Handler`](#App.Handler), [`App.Run`](#App.Run)
  - [`AuthConfig`](#AuthConfig)
  - [`AuthSetup`](#AuthSetup)
  - [`Authenticator`](#Authenticator)
  - [`Command`](#Command)
  - [`Config`](#Config): [`LoadConfig`](#LoadConfig), [`Config.Production`](#Config.Production)
  - [`Declarations`](#Declarations)
  - [`Deps`](#Deps)
  - [`DevConsoleConfig`](#DevConsoleConfig)
  - [`MailConfig`](#MailConfig)
  - [`Migration`](#Migration)
  - [`Module`](#Module)
  - [`Option`](#Option): [`WithAuth`](#WithAuth), [`WithLogger`](#WithLogger), [`WithMailer`](#WithMailer), [`WithMailerFunc`](#WithMailerFunc), [`WithMiddleware`](#WithMiddleware), [`WithMiddlewareFunc`](#WithMiddlewareFunc), [`WithMigrations`](#WithMigrations), [`WithModules`](#WithModules), [`WithName`](#WithName), [`WithStack`](#WithStack), [`WithStorage`](#WithStorage), [`WithStorageFunc`](#WithStorageFunc)
  - [`Permission`](#Permission)
  - [`PermissionDeclarer`](#PermissionDeclarer)
  - [`Platform`](#Platform): [`Platform.Authenticate`](#Platform.Authenticate), [`Platform.OnShutdown`](#Platform.OnShutdown), [`Platform.RateLimiters`](#Platform.RateLimiters), [`Platform.Retention`](#Platform.Retention)
  - [`RateLimiter`](#RateLimiter)
  - [`Retention`](#Retention)
  - [`RouteOption`](#RouteOption): [`Customize`](#Customize), [`Deprecated`](#Deprecated), [`Description`](#Description), [`Errors`](#Errors), [`OperationID`](#OperationID), [`Status`](#Status), [`Summary`](#Summary), [`Tags`](#Tags), [`Timeout`](#Timeout), [`Use`](#Use)
  - [`Router`](#Router): [`Router.Group`](#Router.Group)
  - [`Stack`](#Stack): [`Stack.Default`](#Stack.Default)
  - [`StorageConfig`](#StorageConfig)

## Constants

<a id="MailDevMail"></a>
<a id="MailMailpit"></a>
<a id="MailProvider"></a>

```go
const (
	// MailDevMail sends every email to orb dev's mail catcher, read in the
	// Dev Portal (DEV_MAIL_SMTP_ADDR). The default in development.
	MailDevMail = "devmail"
	// MailMailpit sends every email to a Mailpit inbox (MAILPIT_SMTP_ADDR).
	MailMailpit = "mailpit"
	// MailProvider sends real email through the provider the app passes
	// with [WithMailer]. Always used in production.
	MailProvider = "provider"
)
```

Email delivery modes, the values of MAIL\_DELIVERY.

*Since `v0.2.0 (unreleased)`*

<a id="StorageLocal"></a>
<a id="StorageS3"></a>
<a id="StorageSpaces"></a>
<a id="StorageR2"></a>
<a id="StorageMinIO"></a>

```go
const (
	// StorageLocal keeps files under STORAGE_LOCAL_DIR; development only.
	StorageLocal = "local"
	// StorageS3, StorageSpaces, StorageR2 and StorageMinIO are
	// S3-compatible services, opened by the store the app passes with
	// [WithStorage].
	StorageS3     = "s3"
	StorageSpaces = "spaces"
	StorageR2     = "r2"
	StorageMinIO  = "minio"
)
```

Storage drivers, the values of STORAGE\_DRIVER.

*Since `v0.2.0 (unreleased)`*

## Variables

<a id="ErrUsage"></a>

```go
var ErrUsage = errors.New("usage")
```

ErrUsage marks an error in how a command was called, such as a missing argument. [Main](#Main) exits with status 2 for it; wrap it in a [Command](#Command)'s errors: fmt.Errorf("%w: grant-role \<email> \<role>", gorbital.ErrUsage).

*Since `v0.2.0 (unreleased)`*

## Functions

<a id="Declare"></a>

### func Declare

```go
func Declare(d Declarations, modules ...Module) error
```

Declare adds the modules' permissions, runtime settings and feature flags to d's registries, in module order. Call it once, before building the settings and flags stores and before freezing the permission catalog; then grant each role its permissions with [Grants](#Grants).

It returns an error naming the module for an invalid or duplicate module name, a permission declared by two modules, a missing registry, or an invalid declaration (which the registries report by panicking).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
var pageSize *settings.Setting[int]
books := gorbital.Module{
	Name:        "books",
	Permissions: []gorbital.Permission{{Name: "books.book.read", Description: "Read books", Roles: []string{"user"}}},
	Settings:    func(r *settings.Registry) { pageSize = settings.Int(r, "books.page_size", 20) },
}

catalog := auth.NewCatalog()
reg := settings.NewRegistry()
if err := gorbital.Declare(gorbital.Declarations{Permissions: catalog, Settings: reg}, books); err != nil {
	panic(err)
}
catalog.Role("user", "Every signed-in user", gorbital.Grants("user", books)...)

fmt.Println(reg.Keys(), pageSize != nil)
fmt.Println(catalog.Permissions("user"))
```

Output:

```text
[books.page_size] true
[books.book.read]
```

<a id="Delete"></a>

### func Delete

```go
func Delete[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption)
```

Delete registers a DELETE operation. See [Get](#Get).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Delete(r, "/v1/books/{id}", removeBook, gorbital.Status(http.StatusNoContent))
}, http.MethodDelete, "/v1/books/{id}")
```

Output:

```text
DELETE /v1/books/{id} id=books-delete-v1-books-by-id summary="Delete v1 books by ID" tags=[] secured=true deprecated=false
```

<a id="Get"></a>

### func Get

```go
func Get[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption)
```

Get registers a GET operation for path under r's prefix. The handler takes the request context and its typed input, and returns its typed output or an error; Huma validates the input and documents both (ADR-0082).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Summary("Get a book"))
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get a book" tags=[] secured=true deprecated=false
```

<a id="Grants"></a>

### func Grants

```go
func Grants(role string, modules ...Module) []string
```

Grants returns the permissions the modules give to role, sorted and without duplicates, for declaring the role in the permission catalog.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
books := gorbital.Module{Name: "books", Permissions: []gorbital.Permission{
	{Name: "books.book.read", Roles: []string{"user", "viewer"}},
	{Name: "books.book.write", Roles: []string{"user"}},
}}
shelves := gorbital.Module{Name: "shelves", Permissions: []gorbital.Permission{
	{Name: "shelves.shelf.read", Roles: []string{"viewer"}},
}}
fmt.Println(gorbital.Grants("user", books, shelves))
fmt.Println(gorbital.Grants("viewer", books, shelves))
```

Output:

```text
[books.book.read books.book.write]
[books.book.read shelves.shelf.read]
```

<a id="Main"></a>

### func Main

```go
func Main(opts ...Option)
```

Main runs the app as a command-line program, for main.go:

```go
func main() {
	gorbital.Main(
		gorbital.WithModules(modules.All()...),
		gorbital.WithMigrations(migrations.FS),
	)
}
```

The first argument chooses the command:

```
serve (default)             load the configuration from the environment, build the app with New and Run it
migrate [--status [--json]] apply pending migrations (Migrate), or report them and change nothing
migrate-down                roll back the most recent migration; development only
openapi [--dir <dir>]       print the OpenAPI document, built without a database, or write it
                            with a Postman collection and llms.txt into dir
version [--json]            print the build's version, commit and Go version
```

Main never returns: it exits with status 0 on success, 1 on a runtime error, and 2 for a usage or configuration error, such as an unknown command or an invalid environment variable. Errors go to standard error, prefixed with the app's name.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// cmd/api/main.go of an app: go run ./cmd/api serves it, go run ./cmd/api
// migrate migrates its database.
main := func() {
	gorbital.Main(
		gorbital.WithModules(modulesAll()...),
		gorbital.WithMigrations(migrationFiles),
	)
}
_ = main
```

<a id="Migrate"></a>

### func Migrate

```go
func Migrate(ctx context.Context, cfg Config, w io.Writer, opts ...Option) error
```

Migrate applies every pending migration: the merged goose history of the library, the modules and the app's own ([WithMigrations](#WithMigrations)) with the goose version table v0.1 apps use, then River's job tables. It reports each applied version to w. A database migrated by a v0.1 app has every library migration already, under the same versions, so nothing is applied twice.

Apps run it from the migrate command ([Main](#Main)) before starting a new version; New never migrates (ADR-0017). It returns an error for a missing DATABASE\_URL, conflicting migrations (naming both files), or a migration that fails, after reporting the ones applied before it.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
migrate := func(ctx context.Context, cfg gorbital.Config) error {
	// Prints "applied migration <version>" for each migration applied.
	return gorbital.Migrate(ctx, cfg, os.Stdout, gorbital.WithMigrations(migrationFiles))
}
_ = migrate
```

<a id="Mount"></a>

### func Mount

```go
func Mount(api huma.API, mapper *httpx.Mapper, deps Deps, modules ...Module) error
```

Mount adds each module's error mappings to mapper and registers its routes on api, in module order. The app calls it once, after [Declare](#Declare) and after building the stores that deps hold; to export the OpenAPI document without a database, pass zero Deps.

api must declare the bearer security scheme (openapi.WithBearerAuth) when any route requires authentication, and errors are written as problem+json only when the app has called openapi.InstallErrors with mapper.

Mount returns the first error, naming the module: an invalid or duplicate module name, errors to map without a mapper, a mapping the mapper refuses, an invalid path, two routes with the same method and path or the same operation ID, or a route Huma can't register (such as an unsupported input type). Routes registered before the error stay registered, so an app treats any error as fatal.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
mux, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "books",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r, "/v1/books/{id}", findBook)                   // signed-in callers only
		gorbital.Get(r, "/v1/catalog/{id}", findBook, guard.Public()) // anyone
	},
})
if err != nil {
	panic(err)
}
fmt.Println(call(mux, http.MethodGet, "/v1/books/bok_1", false))
fmt.Println(call(mux, http.MethodGet, "/v1/books/bok_1", true))
fmt.Println(call(mux, http.MethodGet, "/v1/catalog/bok_1", false))
```

Output:

```text
401 unauthenticated
200
200
```

**Example (duplicateRoute)**

```go
_, api, mapper := newAPI()
route := func(name string) gorbital.Module {
	return gorbital.Module{Name: name, Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.OperationID(name+"-get-book"))
	}}
}
err := gorbital.Mount(api, mapper, gorbital.Deps{}, route("books"), route("library"))
fmt.Println(err)
```

Output:

```text
gorbital: GET /v1/books/{id} is registered by modules "books" and "library"
```

<a id="Patch"></a>

### func Patch

```go
func Patch[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption)
```

Patch registers a PATCH operation. See [Get](#Get).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Patch(r, "/v1/books/{id}", findBook, gorbital.Summary("Update a book"))
}, http.MethodPatch, "/v1/books/{id}")
```

Output:

```text
PATCH /v1/books/{id} id=books-patch-v1-books-by-id summary="Update a book" tags=[] secured=true deprecated=false
```

<a id="Post"></a>

### func Post

```go
func Post[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption)
```

Post registers a POST operation. See [Get](#Get).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Post(r, "/v1/books", addBook, gorbital.Status(http.StatusCreated))
}, http.MethodPost, "/v1/books")
```

Output:

```text
POST /v1/books id=books-post-v1-books summary="Post v1 books" tags=[] secured=true deprecated=false
```

<a id="Put"></a>

### func Put

```go
func Put[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption)
```

Put registers a PUT operation. See [Get](#Get).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Put(r, "/v1/books/{id}/title", findBook, gorbital.Summary("Replace a book's title"))
}, http.MethodPut, "/v1/books/{id}/title")
```

Output:

```text
PUT /v1/books/{id}/title id=books-put-v1-books-by-id-title summary="Replace a book's title" tags=[] secured=true deprecated=false
```

## Types

<a id="App"></a>

### type App

```go
type App struct {
	// contains filtered or unexported fields
}
```

An App is a built application: its HTTP handler, background workers and the resources they hold. [New](#New) builds it; [App.Run](#App.Run) serves it until a shutdown signal; [App.Close](#App.Close) releases it without running.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
serve := func(ctx context.Context, cfg gorbital.Config) error {
	app, err := gorbital.New(ctx, cfg, gorbital.WithModules(modulesAll()...))
	if err != nil {
		return err
	}
	return app.Run(ctx) // until SIGINT or SIGTERM
}
_ = serve
```

<a id="New"></a>

#### func New

```go
func New(ctx context.Context, cfg Config, opts ...Option) (*App, error)
```

New builds the app from cfg in dependency order (ADR-0081): telemetry, the database pool, the audit log, the settings and flags registries with every module's declarations, their stores, email delivery, rate limits, idempotency keys, file storage, request metrics, the job definitions and client, the mailer modules send through, release tracking, the dev console, then the modules' routes and the middleware stack. Every step is a public constructor of a library module, which an app can also call itself.

New connects to PostgreSQL and loads the stored settings, flags and job overrides, but never migrates (ADR-0017): run [Migrate](#Migrate) first. Pending migrations are logged as a warning.

It returns an error, after closing whatever it had opened, for a missing DATABASE\_URL, an unreachable database, conflicting migrations, a module declaration that fails (a duplicate module, route, operation ID, permission, setting, flag or job, naming both modules), an S3-compatible STORAGE\_DRIVER without [WithStorage](#WithStorage), or MAIL\_DELIVERY=provider without [WithMailer](#WithMailer).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// What Main does for the serve command, for an app that builds its own
// program or embeds the handler in another server.
ctx := context.Background()
cfg, err := gorbital.LoadConfig(config.OS)
if err != nil {
	log.Fatal(err)
}
opts := []gorbital.Option{gorbital.WithModules(modulesAll()...), gorbital.WithMigrations(migrationFiles)}
if err := gorbital.Migrate(ctx, cfg, os.Stdout, opts...); err != nil {
	log.Fatal(err)
}
app, err := gorbital.New(ctx, cfg, opts...)
if err != nil {
	log.Fatal(err)
}
if err := app.Run(ctx); err != nil {
	log.Fatal(err)
}
```

<a id="App.Close"></a>

#### func (*App) Close

```go
func (a *App) Close(ctx context.Context) error
```

Close releases the app's resources without running it, in the reverse order New opened them. Call it when Run is never called, such as in tests.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
check := func(ctx context.Context, cfg gorbital.Config) (err error) {
	app, err := gorbital.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, app.Close(ctx)) }()
	// Use the app without running it.
	return nil
}
_ = check
```

<a id="App.Deps"></a>

#### func (*App) Deps

```go
func (a *App) Deps() Deps
```

Deps returns the dependencies the app passes to its modules, for commands, seed data and tests that use the same stores.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
seed := func(ctx context.Context, app *gorbital.App) error {
	_, err := app.Deps().DB.Exec(ctx, `INSERT INTO books (id, title) VALUES ('bok_1', 'Dune') ON CONFLICT DO NOTHING`)
	return err
}
_ = seed
```

<a id="App.Handler"></a>

#### func (*App) Handler

```go
func (a *App) Handler() http.Handler
```

Handler returns the app's HTTP handler: every route behind the middleware stack, as Run serves it. Use it in tests, or to serve the app from another server; background workers run only with Run.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
mount := func(app *gorbital.App) http.Handler {
	// The app under /api/ of another server.
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", app.Handler()))
	return mux
}
_ = mount
```

<a id="App.Run"></a>

#### func (*App) Run

```go
func (a *App) Run(ctx context.Context) error
```

Run serves HTTP and runs the background workers until ctx is done or a shutdown signal (SIGINT, SIGTERM) arrives, then shuts down in the order of ADR-0017: readiness answers 503, the drain delay passes (5 seconds in production, none in development), the server stops taking requests and the workers stop, then resources close in the reverse order New opened them, telemetry last. It returns nil after a clean shutdown, or every failure joined.

The workers are the settings, flags and job definition listeners, the job client, the release heartbeat and the request collector, plus the metrics listener when METRICS\_ADDR is set. Run releases the app's resources when it returns, so a Close after it does nothing.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
run := func(ctx context.Context, app *gorbital.App) error {
	// Run stops when ctx is done, as well as on a signal.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return app.Run(ctx)
}
_ = run
```

<a id="AuthConfig"></a>
<a id="AuthConfig.EncryptionKeys"></a>
<a id="AuthConfig.PublicURL"></a>
<a id="AuthConfig.DefaultReturnTo"></a>
<a id="AuthConfig.WebAuthnRPID"></a>
<a id="AuthConfig.WebAuthnOrigins"></a>
<a id="AuthConfig.WebAuthnAppleAppIDs"></a>
<a id="AuthConfig.WebAuthnAndroidApps"></a>
<a id="AuthConfig.GoogleClientID"></a>
<a id="AuthConfig.GoogleClientSecret"></a>
<a id="AuthConfig.GoogleIOSClientID"></a>
<a id="AuthConfig.GoogleAndroidClientID"></a>
<a id="AuthConfig.AppleTeamID"></a>
<a id="AuthConfig.AppleServicesID"></a>
<a id="AuthConfig.AppleKeyID"></a>
<a id="AuthConfig.ApplePrivateKey"></a>
<a id="AuthConfig.AppleBundleIDs"></a>
<a id="AuthConfig.GitHubClientID"></a>
<a id="AuthConfig.GitHubClientSecret"></a>

### type AuthConfig

```go
type AuthConfig struct {
	// EncryptionKeys encrypt authenticator app secrets
	// (AUTH_ENCRYPTION_KEYS: comma-separated id:base64 entries).
	EncryptionKeys config.Secret
	// PublicURL is the API's public base URL, where sign-in providers
	// return (APP_PUBLIC_URL; http://localhost:8080 in development).
	PublicURL string
	// DefaultReturnTo is where a web sign-in started without return_to
	// ends (AUTH_DEFAULT_RETURN_TO; the API docs in development).
	DefaultReturnTo string

	// WebAuthnRPID is the passkey relying party (WEBAUTHN_RP_ID; localhost
	// in development when neither it nor the origins are set).
	WebAuthnRPID string
	// WebAuthnOrigins are the browser origins using passkeys
	// (WEBAUTHN_ORIGINS).
	WebAuthnOrigins []string
	// WebAuthnAppleAppIDs and WebAuthnAndroidApps are the raw values of
	// WEBAUTHN_APPLE_APP_IDS and WEBAUTHN_ANDROID_APPS.
	WebAuthnAppleAppIDs string
	WebAuthnAndroidApps string

	GoogleClientID        string        // GOOGLE_CLIENT_ID: the web client
	GoogleClientSecret    config.Secret // GOOGLE_CLIENT_SECRET
	GoogleIOSClientID     string        // GOOGLE_IOS_CLIENT_ID
	GoogleAndroidClientID string        // GOOGLE_ANDROID_CLIENT_ID

	AppleTeamID     string        // APPLE_TEAM_ID
	AppleServicesID string        // APPLE_SERVICES_ID: web sign-in
	AppleKeyID      string        // APPLE_KEY_ID
	ApplePrivateKey config.Secret // APPLE_PRIVATE_KEY, or the file APPLE_PRIVATE_KEY_FILE names
	AppleBundleIDs  []string      // APPLE_BUNDLE_IDS: sign-in in iOS apps

	GitHubClientID     string        // GITHUB_CLIENT_ID
	GitHubClientSecret config.Secret // GITHUB_CLIENT_SECRET
}
```

AuthConfig holds the sign-in variables of a v0.1 app: the encryption keys for second-factor secrets, passkeys, and Google, Apple and GitHub sign-in (ADR-0043 to ADR-0046, ADR-0059). gorbital reads and checks them; the authenticator passed with [WithAuth](#WithAuth) uses them.

Checks that need a sign-in provider's own package are the authenticator's, so apps without sign-in don't compile those packages: parsing the Apple private key, WEBAUTHN\_APPLE\_APP\_IDS and WEBAUTHN\_ANDROID\_APPS, and matching WEBAUTHN\_ORIGINS to WEBAUTHN\_RP\_ID (ADR-0083).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
cfg, err := gorbital.LoadConfig(developmentEnv(map[string]string{
	"APP_ENV": "development", "GITHUB_CLIENT_ID": "Iv1.abc", "GITHUB_CLIENT_SECRET": "secret",
}))
if err != nil {
	panic(err)
}
fmt.Println(cfg.Auth.PublicURL, cfg.Auth.DefaultReturnTo, cfg.Auth.WebAuthnRPID)
```

Output:

```text
http://localhost:8080 http://localhost:8080/docs localhost
```

<a id="AuthSetup"></a>
<a id="AuthSetup.Name"></a>
<a id="AuthSetup.Config"></a>
<a id="AuthSetup.Deps"></a>
<a id="AuthSetup.Permissions"></a>
<a id="AuthSetup.DevConsole"></a>
<a id="AuthSetup.Handle"></a>
<a id="AuthSetup.MailPreviews"></a>

### type AuthSetup

```go
type AuthSetup struct {
	// Name is the app's name ([WithName]).
	Name string
	// Config is the loaded configuration.
	Config Config
	// Deps are the app's dependencies, with an untagged logger; zero from
	// Main.
	Deps Deps
	// Permissions is the app's permission catalog: every module's
	// permissions, and a role for every role name they use, with the
	// permissions the modules grant it. It isn't frozen yet, so the
	// authenticator can declare the roles it relies on and require a second
	// factor for roles; the authenticator freezes it.
	Permissions *auth.Catalog
	// DevConsole reports whether the app serves the development console
	// (APP_ENV=development with DEV_CONSOLE_TOKEN): features that must never
	// run in production, such as impersonation, check it.
	DevConsole bool
	// Handle serves a handler outside the OpenAPI document on the app's mux,
	// behind the middleware stack, such as the /.well-known files passkeys
	// need. The pattern is an http.ServeMux pattern with a method, such as
	// "GET /.well-known/assetlinks.json". The dev console lists it. It does
	// nothing from Main.
	Handle func(pattern string, handler http.Handler)
	// MailPreviews adds emails the dev console previews and sends with
	// sample data (ADR-0074). It does nothing from Main.
	MailPreviews func(previews ...auth.EmailPreview)
}
```

AuthSetup is what the app hands its authenticator before using it: the configuration, the dependencies and the permission catalog. An [Authenticator](#Authenticator) with a method

```
Setup(ctx context.Context, s gorbital.AuthSetup) error
```

receives it, explicitly and once per app:

  - from [New](#New), after the stores exist and before the modules' jobs, routes and the middleware stack are built, with the app's Deps; an error fails New;
  - from [Main](#Main), before a command the authenticator contributes runs, with zero Deps: the command opens what it needs from Config.

An authenticator that also has a method CheckConfig(cfg Config) error has it called first, by New before connecting to anything and by Main before such a command; its error is a configuration error (exit status 2), as for [LoadConfig](#LoadConfig). gorbital.dev/gorbital/authhttp checks there what needs sign-in's own packages, such as the Apple private key.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
package gorbital_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"gorbital.dev/gorbital"
)

// tokenAuth is an authenticator that reads its configuration and database
// from the app, as gorbital.dev/gorbital/authhttp does.
type tokenAuth struct {
	app  string
	deps gorbital.Deps
}

func (a *tokenAuth) Middleware(*slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Look the bearer token up in a.deps.DB and set the actor.
			next.ServeHTTP(w, r)
		})
	}
}

// CheckConfig runs before anything connects; an error exits with status 2.
func (a *tokenAuth) CheckConfig(cfg gorbital.Config) error {
	if cfg.Production() && cfg.Auth.EncryptionKeys.IsZero() {
		return errors.New("AUTH_ENCRYPTION_KEYS is required in production")
	}
	return nil
}

// Setup receives the app's dependencies once the stores exist.
func (a *tokenAuth) Setup(_ context.Context, s gorbital.AuthSetup) error {
	a.app, a.deps = s.Name, s.Deps
	if !s.Permissions.HasRole("user") {
		s.Permissions.Role("user", "Every signed-in user")
	}
	s.Permissions.Freeze()
	s.Handle("GET /.well-known/token-issuer", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(a.app))
	}))
	return nil
}

func ExampleAuthSetup() {
	main := func() {
		gorbital.Main(gorbital.WithAuth(&tokenAuth{}), gorbital.WithModules(modulesAll()...))
	}
	_ = main
}
```

<a id="Authenticator"></a>
<a id="Authenticator.Middleware"></a>

### type Authenticator

```go
type Authenticator interface {
	Middleware(logger *slog.Logger) func(http.Handler) http.Handler
}
```

An Authenticator resolves who makes each request. Its middleware runs at the Auth step of the middleware stack ([Stack](#Stack)) and sets the actor (actor.With, or auth.WithPrincipal) for authenticated requests; requests it can't authenticate pass through without one, and every route that isn't guard.Public() then answers 401.

A value that also has a method Module() Module contributes that module too: its routes, permissions, settings, jobs and migrations. One with a method Setup(ctx, AuthSetup) error receives the app's configuration, dependencies and permission catalog before it serves, and one with a method CheckConfig(Config) error checks the configuration first ([AuthSetup](#AuthSetup)). gorbital.dev/gorbital/authhttp has all of them.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
var a gorbital.Authenticator = headerAuth{}
gorbital.Main(gorbital.WithAuth(a), gorbital.WithModules(modulesAll()...))
```

<a id="Command"></a>
<a id="Command.Name"></a>
<a id="Command.Usage"></a>
<a id="Command.Run"></a>

### type Command

```go
type Command struct {
	// Name is what follows the program name, such as "grant-role". It
	// can't be a built-in command's name.
	Name string
	// Usage is one line: the arguments, then what the command does, such as
	// "grant-role <email> <role>   give an account a platform role".
	Usage string
	// Run runs the command with the loaded configuration and the arguments
	// after the name. Return an error wrapping [ErrUsage] for bad
	// arguments.
	Run func(ctx context.Context, cfg Config, args []string, stdout io.Writer) error
}
```

A Command is a subcommand of [Main](#Main) beside the built-in ones. A value passed to [WithAuth](#WithAuth) that has a method Commands() \[]Command contributes its commands, as the built-in sign-in does for its role commands.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// A command an authenticator contributes from its Commands method.
grantRole := gorbital.Command{
	Name:  "grant-role",
	Usage: "grant-role <email> <role>      give an account a platform role",
	Run: func(ctx context.Context, cfg gorbital.Config, args []string, stdout io.Writer) error {
		if len(args) != 2 {
			return fmt.Errorf("%w: grant-role <email> <role>", gorbital.ErrUsage)
		}
		fmt.Fprintf(stdout, "%s is now %s\n", args[0], args[1])
		return nil
	},
}
err := grantRole.Run(context.Background(), gorbital.Config{}, []string{"ada@example.com"}, os.Stdout)
fmt.Println(errors.Is(err, gorbital.ErrUsage))
```

Output:

```text
true
```

<a id="Config"></a>
<a id="Config.Env"></a>
<a id="Config.Addr"></a>
<a id="Config.LogLevel"></a>
<a id="Config.LogFormat"></a>
<a id="Config.LogArchiveDir"></a>
<a id="Config.DocsEnabled"></a>
<a id="Config.CORSOrigins"></a>
<a id="Config.TrustedProxies"></a>
<a id="Config.TrustedCallers"></a>
<a id="Config.OpsAllowedIPs"></a>
<a id="Config.MaxBodyBytes"></a>
<a id="Config.RequestTimeout"></a>
<a id="Config.OTLPEndpoint"></a>
<a id="Config.MetricsAddr"></a>
<a id="Config.DatabaseURL"></a>
<a id="Config.DBMaxConns"></a>
<a id="Config.JobWorkers"></a>
<a id="Config.MailDelivery"></a>
<a id="Config.MailpitAddr"></a>
<a id="Config.DevMailAddr"></a>
<a id="Config.Mail"></a>
<a id="Config.Storage"></a>
<a id="Config.Auth"></a>
<a id="Config.DevConsole"></a>
<a id="Config.EnvKeys"></a>

### type Config

```go
type Config struct {
	// Env is development or production (APP_ENV, required).
	Env string
	// Addr is where the API listens (APP_ADDR, default 127.0.0.1:8080).
	Addr     string
	LogLevel slog.Level // APP_LOG_LEVEL: debug, info, warn or error
	// LogFormat is json or text (APP_LOG_FORMAT); empty means JSON in
	// production and text elsewhere.
	LogFormat string
	// LogArchiveDir is where the hourly log archive spools the current hour
	// (LOG_ARCHIVE_DIR, default .orb/logs; ADR-0079).
	LogArchiveDir string
	// DocsEnabled serves /docs and the OpenAPI document (APP_DOCS_ENABLED;
	// default on in development, off in production).
	DocsEnabled bool
	// CORSOrigins are the browser origins allowed to call the API
	// (APP_CORS_ORIGINS, comma-separated; https in production).
	CORSOrigins []string
	// TrustedProxies are the load balancers whose X-Forwarded-For names the
	// client (APP_TRUSTED_PROXIES, ADR-0052).
	TrustedProxies []netip.Prefix
	// TrustedCallers are the gateways whose X-Request-ID and trace context
	// the app accepts (APP_TRUSTED_CALLERS).
	TrustedCallers []netip.Prefix
	// OpsAllowedIPs are the client addresses allowed to call the operations
	// API under /ops/ (OPS_ALLOWED_IPS, comma-separated ranges or
	// addresses; ADR-0085). Empty allows every address. The address is the
	// client's after APP_TRUSTED_PROXIES.
	OpsAllowedIPs []netip.Prefix
	// MaxBodyBytes limits request bodies (APP_MAX_BODY_BYTES, default 1 MiB).
	MaxBodyBytes int64
	// RequestTimeout is how long a handler may take before the Timeout
	// step answers 503 request_timeout (APP_REQUEST_TIMEOUT, default 30s;
	// 0 turns it off). It must be shorter than the server's write timeout,
	// httpx.DefaultWriteTimeout, so the 503 can still be sent.
	RequestTimeout time.Duration
	// OTLPEndpoint exports traces and metrics when set
	// (OTEL_EXPORTER_OTLP_ENDPOINT).
	OTLPEndpoint string
	// MetricsAddr is the separate listener serving Prometheus metrics
	// (METRICS_ADDR; empty turns it off; never APP_ADDR's port).
	MetricsAddr string

	// DatabaseURL is the PostgreSQL connection URL (DATABASE_URL), required
	// by [New] and migrations but not by exporting the OpenAPI document.
	DatabaseURL config.Secret
	// DBMaxConns sizes the connection pool (APP_DB_MAX_CONNS, 1–1000,
	// default 10).
	DBMaxConns int32
	// JobWorkers is how many jobs run at once (APP_JOB_WORKERS, 1–10000,
	// default 10).
	JobWorkers int

	// MailDelivery is MailDevMail, MailMailpit or MailProvider
	// (MAIL_DELIVERY; production allows only MailProvider).
	MailDelivery string
	// MailpitAddr is Mailpit's SMTP address (MAILPIT_SMTP_ADDR, default
	// 127.0.0.1:1025).
	MailpitAddr string
	// DevMailAddr is orb dev's mail catcher (DEV_MAIL_SMTP_ADDR, default
	// 127.0.0.1:1025; ADR-0074).
	DevMailAddr string
	// Mail holds the email provider's secrets.
	Mail MailConfig
	// Storage is the file storage driver and its settings (ADR-0075).
	Storage StorageConfig
	// Auth holds the sign-in variables, for the authenticator.
	Auth AuthConfig
	// DevConsole turns on the development console under /_dev/.
	DevConsole DevConsoleConfig

	// EnvKeys are the environment variables LoadConfig read, secrets as set
	// or unset only, listed by the dev console's GET /_dev/config.
	EnvKeys []devconsole.EnvKey
}
```

Config is every boot setting of an app: secrets and infrastructure, read from environment variables by [LoadConfig](#LoadConfig). The names, defaults and checks are those of a v0.1 app's internal/app/config.go, so a v0.1 deployment's environment works unchanged. Values operators change at runtime are runtime settings, not configuration (ADR-0031).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
cfg, err := gorbital.LoadConfig(developmentEnv(map[string]string{"APP_ENV": "development", "APP_JOB_WORKERS": "4"}))
if err != nil {
	panic(err)
}
fmt.Println(cfg.Addr, cfg.JobWorkers, cfg.MailDelivery, cfg.DocsEnabled)
```

Output:

```text
127.0.0.1:8080 4 devmail true
```

<a id="LoadConfig"></a>

#### func LoadConfig

```go
func LoadConfig(src config.Source) (Config, error)
```

LoadConfig reads the configuration from src, such as config.OS. It reports every invalid value at once, joined, each naming its variable, so a misconfigured deployment fails on its first start. It never connects to anything.

Production refuses: a missing or unknown APP\_ENV; http CORS origins, passkey origins, public URL or default return address; MAIL\_DELIVERY other than provider; STORAGE\_DRIVER=local; and DEV\_CONSOLE\_TOKEN.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
cfg, err := gorbital.LoadConfig(developmentEnv(map[string]string{
	"APP_ENV":          "production",
	"APP_ADDR":         "api",
	"APP_CORS_ORIGINS": "http://app.example.com",
	"STORAGE_DRIVER":   "local",
}))
fmt.Println(cfg.Env == "") // the zero Config on error
for line := range strings.Lines(err.Error()) {
	fmt.Print(line)
}
```

Output:

```text
true
invalid configuration:
APP_ADDR "api" is not host:port: address api: missing port in address
APP_CORS_ORIGINS: "http://app.example.com" must use https in production
STORAGE_DRIVER=local is for development; production needs s3, spaces, r2 or minio with STORAGE_ENDPOINT, STORAGE_BUCKET, STORAGE_ACCESS_KEY and STORAGE_SECRET_KEY
```

<a id="Config.Production"></a>

#### func (Config) Production

```go
func (c Config) Production() bool
```

Production reports whether the app runs in production mode.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
cfg, _ := gorbital.LoadConfig(developmentEnv(map[string]string{"APP_ENV": "development"}))
fmt.Println(cfg.Production())
```

Output:

```text
false
```

<a id="Declarations"></a>
<a id="Declarations.Permissions"></a>
<a id="Declarations.Settings"></a>
<a id="Declarations.Flags"></a>

### type Declarations

```go
type Declarations struct {
	// Permissions receives every module's permissions; nil skips them.
	Permissions PermissionDeclarer
	// Settings and Flags are required when a module declares settings or
	// flags.
	Settings *settings.Registry
	Flags    *flags.Registry
}
```

Declarations are the registries [Declare](#Declare) adds modules' declarations to.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
books := gorbital.Module{
	Name:  "books",
	Flags: func(r *flags.Registry) { flags.Bool(r, "books.covers", flags.Describe("Show cover images")) },
}
err := gorbital.Declare(gorbital.Declarations{}, books)
fmt.Println(err)
```

Output:

```text
gorbital: module "books" declares flags, but Declarations.Flags is nil
```

<a id="Deps"></a>
<a id="Deps.DB"></a>
<a id="Deps.Audit"></a>
<a id="Deps.Mailer"></a>
<a id="Deps.Jobs"></a>
<a id="Deps.Settings"></a>
<a id="Deps.Flags"></a>
<a id="Deps.Storage"></a>
<a id="Deps.RateLimits"></a>
<a id="Deps.Logger"></a>

### type Deps

```go
type Deps struct {
	DB       *pgxpool.Pool
	Audit    audit.Recorder
	Mailer   mail.Sender
	Jobs     *jobs.Client
	Settings *settings.Store
	Flags    *flags.Store
	Storage  storage.Store
	// RateLimits shares guard.RateLimit budgets across instances; without
	// it, each instance counts on its own.
	RateLimits *ratelimitpg.Store
	// Logger is tagged with the module's name by [Mount].
	Logger *slog.Logger
}
```

Deps are the app's shared dependencies, passed to each module's Routes.

Every field may be nil: all of them are while the OpenAPI document is exported without a database, and Storage is nil unless the app configures file storage. A module registers the same routes either way and uses its dependencies only when handling requests.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
books := gorbital.Module{
	Name: "books",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		// d.DB, d.Mailer and the others are nil while the OpenAPI
		// document is exported; use them only in handlers.
		d.Logger.Info("registering routes", "database", d.DB != nil)
		gorbital.Get(r, "/v1/books/{id}", findBook)
	},
}
_, api, mapper := newAPI()
fmt.Println(gorbital.Mount(api, mapper, gorbital.Deps{}, books))
```

Output:

```text
<nil>
```

<a id="DevConsoleConfig"></a>
<a id="DevConsoleConfig.Token"></a>
<a id="DevConsoleConfig.MailpitWebPort"></a>

### type DevConsoleConfig

```go
type DevConsoleConfig struct {
	// Token turns the console on in development (DEV_CONSOLE_TOKEN, set by
	// orb dev; refused in production).
	Token config.Secret
	// MailpitWebPort is the port of Mailpit's web interface on
	// MAILPIT_SMTP_ADDR's host (MAILPIT_WEB_PORT, default 8025).
	MailpitWebPort string
}
```

DevConsoleConfig is the development console's configuration (ADR-0065).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_, err := gorbital.LoadConfig(developmentEnv(map[string]string{"APP_ENV": "production", "DEV_CONSOLE_TOKEN": strings.Repeat("x", 40)}))
fmt.Println(strings.Contains(err.Error(), "DEV_CONSOLE_TOKEN is for local development only"))
```

Output:

```text
true
```

<a id="MailConfig"></a>
<a id="MailConfig.ResendAPIKey"></a>
<a id="MailConfig.ResendWebhookSecret"></a>

### type MailConfig

```go
type MailConfig struct {
	// ResendAPIKey is RESEND_API_KEY.
	ResendAPIKey config.Secret
	// ResendWebhookSecret verifies Resend's bounce and complaint webhooks
	// (RESEND_WEBHOOK_SECRET, ADR-0062).
	ResendWebhookSecret config.Secret
}
```

MailConfig holds the email provider's secrets. The provider itself is the app's choice ([WithMailer](#WithMailer)); these are the variables a v0.1 app's Resend provider reads, kept so a provider constructor can use them.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// An email provider built from its secrets, for MAIL_DELIVERY=provider.
provider := gorbital.WithMailerFunc(func(cfg gorbital.Config) (mail.Sender, error) {
	if cfg.Mail.ResendAPIKey.IsZero() {
		return nil, errors.New("RESEND_API_KEY is required to send email with Resend")
	}
	return newResendSender(cfg.Mail.ResendAPIKey), nil
})
_ = provider
```

<a id="Migration"></a>
<a id="Migration.Version"></a>
<a id="Migration.Name"></a>
<a id="Migration.FS"></a>
<a id="Migration.File"></a>

### type Migration

```go
type Migration struct {
	// Version orders the migration in the app's history, such as
	// 20260914000001. Released versions never change.
	Version int64
	// Name describes the migration in lowercase snake_case, such as
	// "settings"; the merged file is named <Version>_<Name>.sql.
	Name string
	// FS holds the file, usually the module's embedded migrations.
	FS fs.FS
	// File is the file's path in FS, such as "00001_settings.sql".
	File string
}
```

A Migration is one goose migration file a module contributes to the app's single migration history (ADR-0083). Library modules number their files locally, such as 00001\_settings.sql; Version places the file in the app's history, and is the version a v0.1 app holds the same file under, so an upgraded database sees it as applied.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// A built-in module serving its embedded migration under the version it
// has in apps' histories.
reviews := gorbital.Module{
	Name: "reviews",
	Migrations: []gorbital.Migration{
		{Version: 20270301000001, Name: "reviews", FS: migrationFiles, File: "00001_reviews.sql"},
	},
}
fmt.Println(reviews.Migrations[0].Version, reviews.Migrations[0].Name)
```

Output:

```text
20270301000001 reviews
```

<a id="Module"></a>
<a id="Module.Name"></a>
<a id="Module.Routes"></a>
<a id="Module.Errors"></a>
<a id="Module.Permissions"></a>
<a id="Module.Settings"></a>
<a id="Module.Flags"></a>
<a id="Module.Middleware"></a>
<a id="Module.Jobs"></a>
<a id="Module.Migrations"></a>
<a id="Module.RateLimiters"></a>
<a id="Module.Retention"></a>
<a id="Module.Platform"></a>

### type Module

```go
type Module struct {
	// Name identifies the module in operation IDs, errors and logs. It is
	// lowercase snake_case, such as "books", and unique in an app.
	Name string

	// Routes registers the module's operations. [Mount] calls it once.
	Routes func(r *Router, d Deps)

	// Errors map the module's errors to problem responses. Error codes are
	// public API: add new ones, never change existing ones (ADR-0015).
	Errors []httpx.Mapping

	// Permissions are the permissions the module checks and the roles that
	// hold them. Permission names are public API.
	Permissions []Permission

	// Settings declares the module's runtime settings. [Declare] calls it
	// once, before the settings store is built.
	Settings func(r *settings.Registry)

	// Flags declares the module's feature flags. [Declare] calls it once,
	// before the flags store is built.
	Flags func(r *flags.Registry)

	// Middleware runs on every route of the module, before group and route
	// middleware (see [Use]).
	Middleware []func(http.Handler) http.Handler

	// Jobs defines the module's background jobs with jobs.Define. [New]
	// calls it once, before the job client exists, because the client is
	// built from the definitions. So in the Deps it receives, Jobs is nil,
	// and Mailer queues through the job client [New] builds next: a worker
	// keeps d and uses it when a job runs, never inside Jobs itself. A
	// worker that enqueues other jobs gets the client from its context with
	// river.ClientFromContext.
	Jobs func(defs *jobs.Definitions, d Deps)

	// Migrations are the module's migrations, merged by version with the
	// library's and the app's (ADR-0083). App modules keep theirs in the
	// app's db/migrations ([WithMigrations]) so tables of different modules
	// can reference each other; built-in modules declare theirs here.
	Migrations []Migration

	// RateLimiters describe the named limiters the module creates on
	// Deps.RateLimits, for /ops/auth/rate-limits. A name declared twice
	// fails New naming both modules.
	RateLimiters []RateLimiter

	// Retention says how long the module's data is kept and what deletes
	// it, for /ops/retention; the built-in retention job deletes what has
	// a Delete function. [New] calls it once, before defining the jobs,
	// with the Deps Jobs receives.
	Retention func(d Deps) []Retention

	// Platform receives what New built for the whole app, after every
	// store and before any module's Routes. It is for gorbital's built-in
	// modules (see [Platform]). An error from it, such as an invalid
	// secret in the configuration, fails New as a configuration error. It
	// isn't called when the OpenAPI document is exported without a
	// database.
	Platform func(p *Platform) error
}
```

A Module is one feature of an app. Its package returns it from a function named Module, so declarations such as settings are created in that function's closure and used by Routes without a lookup by name:

```go
func Module() gorbital.Module {
	var pageSize *settings.Setting[int]
	return gorbital.Module{
		Name:     "books",
		Settings: func(r *settings.Registry) { pageSize = settings.Int(r, "books.page_size", 20) },
		Routes:   func(r *gorbital.Router, d gorbital.Deps) { /* use pageSize */ },
	}
}
```

*Since `v0.2.0 (unreleased)`*

**Example**

```go
books := gorbital.Module{
	Name: "books",
	Errors: []httpx.Mapping{
		{Err: ErrBookNotFound, Status: http.StatusNotFound, Code: "book_not_found", Detail: "no book has this ID"},
	},
	Permissions: []gorbital.Permission{
		{Name: "books.book.read", Description: "Read books", Roles: []string{"user"}},
	},
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r.Group("/v1/books"), "/{id}", findBook)
	},
}

mux, api, mapper := newAPI()
if err := gorbital.Mount(api, mapper, gorbital.Deps{}, books); err != nil {
	panic(err)
}
fmt.Println(call(mux, http.MethodGet, "/v1/books/bok_1", true))
fmt.Println(call(mux, http.MethodGet, "/v1/books/bok_9", true))
```

Output:

```text
200
404 book_not_found
```

**Example (jobs)**

```go
// Jobs runs before the job client exists: keep d and use it when a job
// runs.
digest := gorbital.Module{
	Name: "digests",
	Jobs: func(defs *jobs.Definitions, d gorbital.Deps) {
		// jobs.Define(defs, jobs.Definition[DigestArgs]{Name: "digests_send", Worker: &digestWorker{mailer: d.Mailer}, ...})
		_ = d.Mailer
	},
}
_ = digest
```

**Example (platform)**

```go
// A built-in module checks its configuration when the app starts: an
// error is a configuration error (exit status 2 from Main).
module := gorbital.Module{
	Name: "payments",
	Platform: func(p *gorbital.Platform) error {
		if p.Config.Production() && p.Config.Mail.ResendWebhookSecret.IsZero() {
			return errors.New("RESEND_WEBHOOK_SECRET is required in production")
		}
		return nil
	},
}
_ = module
```

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [New](#New), [Main](#Main) and [Migrate](#Migrate). Every option is one line in main.go that names what the app contains (ADR-0081).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
opts := []gorbital.Option{
	gorbital.WithName("shelfie"),
	gorbital.WithModules(modulesAll()...),
}
_ = opts
```

<a id="WithAuth"></a>

#### func WithAuth

```go
func WithAuth(a Authenticator) Option
```

WithAuth sets the app's authenticator. Without one, requests have no actor, so only guard.Public() routes succeed, and New logs how many routes can't be reached.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
gorbital.Main(gorbital.WithAuth(headerAuth{}), gorbital.WithModules(modulesAll()...))
```

<a id="WithLogger"></a>

#### func WithLogger

```go
func WithLogger(logger *slog.Logger) Option
```

WithLogger sets the logger of the app and its modules. Without it, New builds one from APP\_LOG\_LEVEL and APP\_LOG\_FORMAT through the telemetry module, which also feeds the dev console and the hourly log archive.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
gorbital.Main(gorbital.WithLogger(slog.New(slog.NewJSONHandler(os.Stderr, nil))))
```

<a id="WithMailer"></a>

#### func WithMailer

```go
func WithMailer(s mail.Sender) Option
```

WithMailer sets the provider email is delivered through when MAIL\_DELIVERY is provider, the only mode production allows. Modules don't send through it directly: Deps.Mailer queues each message as a job, and the mail worker delivers it through this sender, skipping suppressed addresses. In development, devmail and mailpit deliver over SMTP without it.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
logged := mail.SenderFunc(func(ctx context.Context, m mail.Message) error {
	slog.InfoContext(ctx, "email", "subject", m.Subject)
	return nil
})
gorbital.Main(gorbital.WithMailer(logged))
```

<a id="WithMailerFunc"></a>

#### func WithMailerFunc

```go
func WithMailerFunc(open func(cfg Config) (mail.Sender, error)) Option
```

WithMailerFunc sets the email provider like [WithMailer](#WithMailer), built from the loaded configuration, such as an API key in Config.Mail. An error from open fails New as a configuration error.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
gorbital.Main(gorbital.WithMailerFunc(func(cfg gorbital.Config) (mail.Sender, error) {
	return newResendSender(cfg.Mail.ResendAPIKey), nil
}))
```

<a id="WithMiddleware"></a>

#### func WithMiddleware

```go
func WithMiddleware(middleware ...func(http.Handler) http.Handler) Option
```

WithMiddleware adds middleware that runs on every request after the built-in stack: after authentication, rate limits and idempotency keys, so it can read the actor. Middleware added by WithMiddleware and [WithMiddlewareFunc](#WithMiddlewareFunc) runs in the order the options are given.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
gorbital.Main(gorbital.WithMiddleware(requireClientVersion("2.4.0")))
```

<a id="WithMiddlewareFunc"></a>

#### func WithMiddlewareFunc

```go
func WithMiddlewareFunc(build func(d Deps) func(http.Handler) http.Handler) Option
```

WithMiddlewareFunc adds middleware like [WithMiddleware](#WithMiddleware), built with the app's dependencies once they exist, such as a middleware that reads a runtime setting or writes to the database.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
gorbital.Main(gorbital.WithMiddlewareFunc(func(d gorbital.Deps) func(http.Handler) http.Handler {
	logger := d.Logger
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a, ok := actor.From(r.Context()); ok {
				logger.DebugContext(r.Context(), "request", "actor", a.ID)
			}
			next.ServeHTTP(w, r)
		})
	}
}))
```

<a id="WithMigrations"></a>

#### func WithMigrations

```go
func WithMigrations(fsys fs.FS) Option
```

WithMigrations sets the app's own goose migrations, usually the embedded files of its db/migrations package. [Migrate](#Migrate) merges them with the library's and the modules' migrations by version; New reports pending ones; the dev console lists them.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// db/migrations/migrations.go:
//
//	//go:embed *.sql
//	var FS embed.FS
gorbital.Main(gorbital.WithMigrations(migrationFiles))
```

<a id="WithModules"></a>

#### func WithModules

```go
func WithModules(modules ...Module) Option
```

WithModules adds modules to the app, after those added before. Order matters only for reading: each module's routes, settings and permissions are its own, and a duplicate fails New naming both modules.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
gorbital.Main(gorbital.WithModules(modulesAll()...))
```

<a id="WithName"></a>

#### func WithName

```go
func WithName(name string) Option
```

WithName sets the app's name, used as the service name in logs, traces and metrics, the OpenAPI document's title and the database connections' application name. Without it, the name is the last element of the main module's path, such as shelfie for example.com/shelfie.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
gorbital.Main(gorbital.WithName("shelfie"), gorbital.WithModules(modulesAll()...))
```

<a id="WithStack"></a>

#### func WithStack

```go
func WithStack(build func(s Stack) []func(http.Handler) http.Handler) Option
```

WithStack replaces the order of the built-in middleware stack: build receives every built-in step and returns the steps to run, outermost first. Leaving out Recover or Auth is allowed, and logged as a warning when New builds the handler. [WithMiddleware](#WithMiddleware) still runs after the returned steps.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
gorbital.Main(gorbital.WithStack(func(s gorbital.Stack) []func(http.Handler) http.Handler {
	// Refuse old clients before anything is logged or authenticated.
	return append([]func(http.Handler) http.Handler{s.Recover, requireClientVersion("2.4.0")}, s.Default()[1:]...)
}))
```

<a id="WithStorage"></a>

#### func WithStorage

```go
func WithStorage(s storage.Store) Option
```

WithStorage sets the app's file storage, passed to modules as Deps.Storage. Without it, New opens the local driver for STORAGE\_DRIVER=local (the default in development) and refuses the S3-compatible drivers, whose client gorbital doesn't import: pass one, built from Config.Storage with [WithStorageFunc](#WithStorageFunc).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
var bucket storage.Store // such as a store from gorbital.dev/modules/storage/s3
gorbital.Main(gorbital.WithStorage(bucket))
```

<a id="WithStorageFunc"></a>

#### func WithStorageFunc

```go
func WithStorageFunc(open func(cfg Config) (storage.Store, error)) Option
```

WithStorageFunc sets the app's file storage like [WithStorage](#WithStorage), built from the loaded configuration, as main.go needs for a store whose settings come from STORAGE\_\* variables. An error from open fails New as a configuration error.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
gorbital.Main(gorbital.WithStorageFunc(func(cfg gorbital.Config) (storage.Store, error) {
	// With gorbital.dev/modules/storage/s3:
	//	return s3.New(s3.Config{Driver: cfg.Storage.Driver, Endpoint: cfg.Storage.Endpoint, ...})
	return nil, fmt.Errorf("STORAGE_DRIVER=%s isn't set up in this app", cfg.Storage.Driver)
}))
```

<a id="Permission"></a>
<a id="Permission.Name"></a>
<a id="Permission.Description"></a>
<a id="Permission.Roles"></a>

### type Permission

```go
type Permission struct {
	Name        string
	Description string
	// Roles are the roles that hold the permission, such as "user". A role
	// the app doesn't declare grants nothing.
	Roles []string
}
```

A Permission is a permission a module checks, such as "books.book.write".

*Since `v0.2.0 (unreleased)`*

**Example**

```go
write := gorbital.Permission{
	Name:        "books.book.write",
	Description: "Add, change and remove books",
	Roles:       []string{"user"},
}
catalog := auth.NewCatalog()
if err := gorbital.Declare(gorbital.Declarations{Permissions: catalog}, gorbital.Module{Name: "books", Permissions: []gorbital.Permission{write}}); err != nil {
	panic(err)
}
fmt.Println(len(catalog.AllPermissions()), catalog.AllPermissions()[0].Name)
```

Output:

```text
1 books.book.write
```

<a id="PermissionDeclarer"></a>
<a id="PermissionDeclarer.Permission"></a>

### type PermissionDeclarer

```go
type PermissionDeclarer interface {
	Permission(name, description string)
}
```

A PermissionDeclarer declares permissions. \*auth.Catalog implements it.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
var declared allowList
books := gorbital.Module{Name: "books", Permissions: []gorbital.Permission{{Name: "books.book.read"}, {Name: "books.book.write"}}}
if err := gorbital.Declare(gorbital.Declarations{Permissions: &declared}, books); err != nil {
	panic(err)
}
fmt.Println(declared)
```

Output:

```text
[books.book.read books.book.write]
```

<a id="Platform"></a>
<a id="Platform.Config"></a>
<a id="Platform.Name"></a>
<a id="Platform.StartedAt"></a>
<a id="Platform.InstanceID"></a>
<a id="Platform.Health"></a>
<a id="Platform.Jobs"></a>
<a id="Platform.MailSender"></a>
<a id="Platform.Migrations"></a>

### type Platform

```go
type Platform struct {
	// Config is the app's configuration.
	Config Config
	// Name is the app's name ([WithName]).
	Name string
	// StartedAt is when New started building the app.
	StartedAt time.Time
	// InstanceID identifies this process in /ops/releases and in request
	// metrics.
	InstanceID string
	// Health runs the readiness checks of /readyz.
	Health *health.Checker
	// Jobs manages job definitions, runs and queues.
	Jobs *jobs.Manager
	// MailSender are the runtime settings that fill every email's sender.
	MailSender mail.Defaults
	// Migrations are every migration Migrate applies: the library's, the
	// modules' and the app's, merged.
	Migrations fs.FS
	// contains filtered or unexported fields
}
```

A Platform is what [New](#New) builds for the app as a whole, beyond [Deps](#Deps): the job manager, the instance's identity and health, the configuration, and what every module declared about its data and rate limits. The operations API (gorbital.dev/gorbital/opshttp) reports on them and the mail events module (gorbital.dev/gorbital/mailevents) reads its webhook secret from the configuration; they receive it through [Module.Platform](#Module.Platform).

Platform is for gorbital's built-in modules. App modules use Deps: a Platform's fields follow what the built-in modules need, and grow with them (ADR-0083).

*Since `v0.2.0 (unreleased)`*

**Example**

A built-in module keeps the Platform New gives it, and uses it in its routes; app modules use Deps.

```go
module := func() gorbital.Module {
	var platform *gorbital.Platform
	return gorbital.Module{
		Name:     "status",
		Platform: func(p *gorbital.Platform) error { platform = p; return nil },
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			gorbital.Get(r, "/ops/status", func(ctx context.Context, _ *struct{}) (*struct{ Body string }, error) {
				return &struct{ Body string }{Body: platform.Name + " " + platform.InstanceID}, nil
			})
		},
	}
}
_ = gorbital.WithModules(module())
```

<a id="Platform.Authenticate"></a>

#### func (*Platform) Authenticate

```go
func (p *Platform) Authenticate(ctx context.Context, r *http.Request) (actor.Actor, bool)
```

Authenticate runs the app's authentication step again for r, such as a long-running stream checking that its session hasn't ended: the authenticator ([WithAuth](#WithAuth)) and, in development, the dev console's operator on /ops/. It sees r's headers and client address, but none of the values in r's context, so an actor set before doesn't carry over. It returns the actor the step resolved, and false when the request isn't authenticated any more.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// A server-sent events stream checks every few seconds that the
// request's session hasn't ended.
stillSignedIn := func(ctx context.Context, p *gorbital.Platform, r *http.Request) (context.Context, bool) {
	a, ok := p.Authenticate(ctx, r)
	if !ok {
		return ctx, false
	}
	return actor.With(ctx, a), true
}
_ = stillSignedIn
```

<a id="Platform.OnShutdown"></a>

#### func (*Platform) OnShutdown

```go
func (p *Platform) OnShutdown(fn func())
```

OnShutdown adds fn to what runs when [App.Run](#App.Run) starts shutting down, before the server stops taking requests, such as closing streams that would hold it open.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// Long-lived streams end when the app starts shutting down, so the
// server doesn't wait for them.
stop := make(chan struct{})
module := gorbital.Module{
	Name: "feed",
	Platform: func(p *gorbital.Platform) error {
		p.OnShutdown(func() { close(stop) })
		return nil
	},
}
_ = module
```

<a id="Platform.RateLimiters"></a>

#### func (*Platform) RateLimiters

```go
func (p *Platform) RateLimiters() []RateLimiter
```

RateLimiters returns every named rate limiter of the app: the built-in one of the RateLimit step (auth\_ip), those modules declare in Module.RateLimiters, in module order, then those guard.RateLimit creates, by name.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// GET /ops/auth/rate-limits lists every limiter, so operators know what
// they can reset.
list := func(p *gorbital.Platform) {
	for _, l := range p.RateLimiters() {
		fmt.Printf("%s (keys: %s): %s\n", l.Name, l.Keys, l.Description)
	}
}
_ = list
```

<a id="Platform.Retention"></a>

#### func (*Platform) Retention

```go
func (p *Platform) Retention() []Retention
```

Retention returns how long each kind of data is kept: what gorbital builds, then each module's Module.Retention, in module order.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// GET /ops/retention reads each kind of data's setting.
report := func(ctx context.Context, p *gorbital.Platform) {
	for _, r := range p.Retention() {
		fmt.Printf("%s: kept %s (%s)\n", r.Data, r.Setting.Get(ctx), r.Setting.Key())
	}
}
_ = report
```

<a id="RateLimiter"></a>
<a id="RateLimiter.Name"></a>
<a id="RateLimiter.Keys"></a>
<a id="RateLimiter.Description"></a>

### type RateLimiter

```go
type RateLimiter struct {
	// Name is the limiter's name, as passed to ratelimitpg.Store.Limiter,
	// such as "auth_login". Names are unique in an app.
	Name string
	// Keys says what a key is, such as "client IP address" or "actor ID",
	// so operators know what to reset.
	Keys string
	// Description says what the limiter protects.
	Description string
}
```

A RateLimiter describes a named rate limiter a module creates on Deps.RateLimits, for GET /ops/auth/rate-limits, where operators reset a key's budget. Limiters of guard.RateLimit are listed without one.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// A module creating its own limiter on the shared store declares it, so
// /ops/auth/rate-limits lists it.
var limiter *ratelimitpg.Limiter
module := gorbital.Module{
	Name:         "imports",
	RateLimiters: []gorbital.RateLimiter{{Name: "imports_uploads", Keys: "user ID", Description: "CSV uploads per user"}},
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		if d.RateLimits != nil {
			var err error
			limiter, err = d.RateLimits.Limiter("imports_uploads", func(context.Context) ratelimit.Limit {
				return ratelimit.Per(10, time.Hour)
			})
			if err != nil {
				panic(err)
			}
		}
	},
}
_, _ = module, limiter
```

<a id="Retention"></a>
<a id="Retention.Data"></a>
<a id="Retention.Setting"></a>
<a id="Retention.Delete"></a>
<a id="Retention.Job"></a>
<a id="Retention.EnforcedBy"></a>
<a id="Retention.Oldest"></a>

### type Retention

```go
type Retention struct {
	// Data names the data, such as "audit_events", in /ops/retention, logs
	// and retention.purged audit events. Names are unique in an app.
	Data string
	// Setting is the runtime setting that holds how long the data is kept.
	Setting *settings.Setting[time.Duration]
	// Delete removes up to limit rows older than before and returns how
	// many it removed. The built-in retention job calls it every day, with
	// the time Setting's value ago.
	Delete func(ctx context.Context, before time.Time, limit int) (int64, error)
	// Job is the name of the job that deletes the data, when the module
	// deletes it with a job of its own, such as "auth_cleanup".
	Job string
	// EnforcedBy says what deletes the data when no job does, such as
	// "each instance, when it starts".
	EnforcedBy string
	// Oldest returns when the oldest stored row was written, and false when
	// there is none. Leave it nil when that can't be read cheaply.
	Oldest func(ctx context.Context) (time.Time, bool, error)
}
```

A Retention is how long one kind of a module's data is kept and what deletes it, listed by GET /ops/retention. Exactly one of Delete, Job and EnforcedBy says what deletes the data.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// A module keeps notes for a runtime setting's duration; the built-in
// retention job deletes older ones every day, and /ops/retention lists
// the policy.
var keep *settings.Setting[time.Duration]
module := gorbital.Module{
	Name: "notes",
	Settings: func(r *settings.Registry) {
		keep = settings.Duration(r, "notes.retention", 90*24*time.Hour,
			settings.Describe("How long deleted notes are kept."), settings.Group("retention"))
	},
	Retention: func(d gorbital.Deps) []gorbital.Retention {
		return []gorbital.Retention{{
			Data:    "deleted_notes",
			Setting: keep,
			Delete: func(ctx context.Context, before time.Time, limit int) (int64, error) {
				tag, err := d.DB.Exec(ctx, `DELETE FROM notes WHERE id IN (
					SELECT id FROM notes WHERE deleted_at < $1 LIMIT $2)`, before, limit)
				return tag.RowsAffected(), err
			},
		}}
	},
}
_ = gorbital.WithModules(module)
```

<a id="RouteOption"></a>

### type RouteOption

```go
type RouteOption = route.Option
```

A RouteOption configures a route, or every route of a group. Options of a group apply first, then the route's own.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// Options on a group apply to its routes first; a route's own options
// come after and win.
routesExample(func(r *gorbital.Router) {
	books := r.Group("/v1/books", gorbital.Tags("Books"), gorbital.Summary("A books operation"))
	gorbital.Get(books, "/{id}", findBook, gorbital.Summary("Get a book"))
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get a book" tags=[Books] secured=true deprecated=false
```

<a id="Customize"></a>

#### func Customize

```go
func Customize(fn func(api huma.API, op *huma.Operation)) RouteOption
```

Customize changes the Huma operation before it is registered, for what the other options don't set, such as a response's media types for a streaming route, Huma's body limit, or schemas added to the API's registry. fn receives the API and the operation as the other options, guards and deny by default have built it, and runs once, at registration. Customize options run in the order given.

fn can't change the method, path, operation ID, security requirements or operation middleware: registration fails when it does, so a route can't leave deny by default or its guards behind.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
stream := func(context.Context, *struct{}) (*huma.StreamResponse, error) {
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		hctx.SetHeader("Content-Type", "text/event-stream")
		_, _ = hctx.BodyWriter().Write([]byte("event: tick\ndata: {}\n\n"))
	}}, nil
}
_, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "clock",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r, "/v1/ticks", stream, gorbital.Customize(func(api huma.API, op *huma.Operation) {
			// Document the event stream and the schema of its events.
			tick := api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[Tick](), true, "")
			op.Responses = map[string]*huma.Response{"200": {
				Description: "Server-sent events: tick, with a Tick",
				Content:     map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeString, Description: tick.Ref}}},
			}}
		}))
	},
})
fmt.Println(err)
for mediaType := range api.OpenAPI().Paths["/v1/ticks"].Get.Responses["200"].Content {
	fmt.Println(mediaType)
}

// What protects a route can't be customized away.
err = gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "leaky",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r, "/v1/secrets", stream, gorbital.Customize(func(_ huma.API, op *huma.Operation) { op.Security = nil }))
	},
})
fmt.Println(err)
```

Output:

```text
<nil>
text/event-stream
gorbital: module "leaky": GET /v1/secrets: a Customize option can't change the method, path, operation ID, security or middleware of a route
```

<a id="Deprecated"></a>

#### func Deprecated

```go
func Deprecated() RouteOption
```

Deprecated marks the route deprecated in the OpenAPI document.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Deprecated())
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[] secured=true deprecated=true
```

<a id="Description"></a>

#### func Description

```go
func Description(markdown string) RouteOption
```

Description sets the OpenAPI description, in Markdown.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, d gorbital.Deps) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Description("Returns one of **your** books."))
}})
if err != nil {
	panic(err)
}
fmt.Println(api.OpenAPI().Paths["/v1/books/{id}"].Get.Description)
```

Output:

```text
Returns one of **your** books.
```

<a id="Errors"></a>

#### func Errors

```go
func Errors(statuses ...int) RouteOption
```

Errors documents error statuses the route returns, in addition to those its guards document.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, d gorbital.Deps) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Errors(http.StatusNotFound))
}})
if err != nil {
	panic(err)
}
responses := api.OpenAPI().Paths["/v1/books/{id}"].Get.Responses
fmt.Println(responses["401"] != nil, responses["404"] != nil)
```

Output:

```text
true true
```

<a id="OperationID"></a>

#### func OperationID

```go
func OperationID(id string) RouteOption
```

OperationID sets the operation ID. Without it, the ID is the module's name followed by the one Huma generates from the method and path, such as "books-post-v1-books". Operation IDs are public API: client generators name their functions after them.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.OperationID("books-get"))
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get summary="Get v1 books by ID" tags=[] secured=true deprecated=false
```

<a id="Status"></a>

#### func Status

```go
func Status(code int) RouteOption
```

Status sets the success status, such as http.StatusCreated.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
mux, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, d gorbital.Deps) {
	gorbital.Delete(r, "/v1/books/{id}", removeBook, gorbital.Status(http.StatusNoContent))
}})
if err != nil {
	panic(err)
}
fmt.Println(call(mux, http.MethodDelete, "/v1/books/bok_1", true))
```

Output:

```text
204
```

<a id="Summary"></a>

#### func Summary

```go
func Summary(s string) RouteOption
```

Summary sets the OpenAPI summary. Without it, Huma generates one from the method and path.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Summary("Get a book"))
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get a book" tags=[] secured=true deprecated=false
```

<a id="Tags"></a>

#### func Tags

```go
func Tags(tags ...string) RouteOption
```

Tags sets the OpenAPI tags. Without it, a route has none.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Tags("Books", "Library"))
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[Books Library] secured=true deprecated=false
```

<a id="Timeout"></a>

#### func Timeout

```go
func Timeout(d time.Duration) RouteOption
```

Timeout gives a route a shorter deadline than the app's request timeout (APP\_REQUEST\_TIMEOUT): after d, a handler that hasn't started its response gets 503 request\_timeout, and its context is cancelled. A context deadline can only be shortened, so a longer d has no effect: raise APP\_REQUEST\_TIMEOUT, or leave the Timeout step out with [WithStack](#WithStack), for routes that need longer. Streaming responses that have started aren't cut off (httpx.Timeout).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
mux, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "reports",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		slow := func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			<-ctx.Done() // a query that respects its context stops here
			return nil, ctx.Err()
		}
		gorbital.Get(r, "/v1/reports/yearly", slow, guard.Public(), gorbital.Timeout(20*time.Millisecond))
	},
})
if err != nil {
	panic(err)
}
fmt.Println(call(mux, http.MethodGet, "/v1/reports/yearly", false))
```

Output:

```text
503 request_timeout
```

<a id="Use"></a>

#### func Use

```go
func Use(middlewares ...func(http.Handler) http.Handler) RouteOption
```

Use adds middleware to a route, or to every route of a group. Middleware runs in the order given, after the module's and the group's middleware and before the route's guards and input parsing (ADR-0082). Any standard middleware works:

```go
books := r.Group("/v1/books", gorbital.Use(requireClientVersion("2.4.0")))
```

A route can't remove middleware its group added: put routes that need different middleware in their own group.

Route middleware needs an API built on Huma's humago adapter, as openapi.New builds it.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// requireClientVersion refuses mobile apps older than min.
requireClientVersion := func(min string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-App-Version") < min {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusUpgradeRequired, "app_outdated", "update the app to continue"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

mux, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "books",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		books := r.Group("/v1/books", gorbital.Use(requireClientVersion("2.4.0")))
		gorbital.Get(books, "/{id}", findBook)
	},
})
if err != nil {
	panic(err)
}
for _, version := range []string{"2.3.9", "2.4.0"} {
	req := httptest.NewRequest(http.MethodGet, "/v1/books/bok_1", nil)
	req.Header.Set("X-App-Version", version)
	req = req.WithContext(actor.With(req.Context(), actor.Actor{Kind: actor.KindUser, ID: "usr_1"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	fmt.Println(version, rec.Code)
}
```

Output:

```text
2.3.9 426
2.4.0 200
```

<a id="Router"></a>

### type Router

```go
type Router struct {
	// contains filtered or unexported fields
}
```

A Router registers a module's routes under a path prefix with shared options. [Mount](#Mount) passes each module's Routes a Router for that module.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "books",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r, "/v1/books/{id}", findBook)
	},
})
if err != nil {
	panic(err)
}
describe(api, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[] secured=true deprecated=false
```

<a id="Router.Group"></a>

#### func (*Router) Group

```go
func (r *Router) Group(prefix string, opts ...RouteOption) *Router
```

Group returns a Router for the routes under prefix, which is empty or starts with a slash, with opts added to the options of r.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "books",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		v1 := r.Group("/v1", gorbital.Tags("Books"))
		books := v1.Group("/books")
		gorbital.Get(books, "/{id}", findBook)
		gorbital.Get(v1.Group("/catalog", guard.Public()), "/{id}", findBook)
	},
})
if err != nil {
	panic(err)
}
describe(api, http.MethodGet, "/v1/books/{id}")
describe(api, http.MethodGet, "/v1/catalog/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[Books] secured=true deprecated=false
GET /v1/catalog/{id} id=books-get-v1-catalog-by-id summary="Get v1 catalog by ID" tags=[Books] secured=false deprecated=false
```

<a id="Stack"></a>
<a id="Stack.Recover"></a>
<a id="Stack.TrustedProxies"></a>
<a id="Stack.RequestID"></a>
<a id="Stack.Telemetry"></a>
<a id="Stack.Observability"></a>
<a id="Stack.AccessLog"></a>
<a id="Stack.Timeout"></a>
<a id="Stack.SecureHeaders"></a>
<a id="Stack.CORS"></a>
<a id="Stack.CrossOrigin"></a>
<a id="Stack.BodyLimit"></a>
<a id="Stack.Maintenance"></a>
<a id="Stack.Auth"></a>
<a id="Stack.RateLimit"></a>
<a id="Stack.Idempotency"></a>

### type Stack

```go
type Stack struct {
	// Recover turns a panic into a 500 problem response (httpx.Recover).
	Recover func(http.Handler) http.Handler
	// TrustedProxies sets the client's address from X-Forwarded-For sent
	// by APP_TRUSTED_PROXIES (httpx.TrustedProxies, ADR-0052).
	TrustedProxies func(http.Handler) http.Handler
	// RequestID gives every request an ID, accepting X-Request-ID only
	// from APP_TRUSTED_CALLERS (httpx.RequestIDFrom).
	RequestID func(http.Handler) http.Handler
	// Telemetry records a span and metrics per request.
	Telemetry func(http.Handler) http.Handler
	// Observability counts requests per route for /ops/observability and
	// automatic incidents (ADR-0064).
	Observability func(http.Handler) http.Handler
	// AccessLog logs one structured line per request (httpx.AccessLog).
	AccessLog func(http.Handler) http.Handler
	// Timeout answers 503 request_timeout when a handler hasn't started its
	// response within APP_REQUEST_TIMEOUT, and cancels the request's
	// context (httpx.Timeout, ADR-0085). A route can shorten it with
	// [Timeout].
	Timeout func(http.Handler) http.Handler
	// SecureHeaders sets security headers, and HSTS in production
	// (httpx.SecureHeaders).
	SecureHeaders func(http.Handler) http.Handler
	// CORS answers browsers on APP_CORS_ORIGINS (httpx.CORS).
	CORS func(http.Handler) http.Handler
	// CrossOrigin refuses cross-site writes that browsers send with
	// cookies (httpx.CrossOrigin), except the sign-in callbacks other sites
	// post to by design.
	CrossOrigin func(http.Handler) http.Handler
	// BodyLimit refuses bodies over APP_MAX_BODY_BYTES (httpx.BodyLimit).
	BodyLimit func(http.Handler) http.Handler
	// Maintenance answers 503 while the maintenance.enabled setting is on,
	// except health checks, docs, sign-in and /ops (httpx.Maintenance).
	Maintenance func(http.Handler) http.Handler
	// Auth runs the authenticator's middleware ([WithAuth]); without an
	// authenticator it passes requests on unchanged.
	Auth func(http.Handler) http.Handler
	// RateLimit limits requests to /v1/auth/ per client address, by the
	// auth.ip_requests_per_minute setting, shared by every instance.
	RateLimit func(http.Handler) http.Handler
	// Idempotency replays a signed-in POST or PATCH retried with the same
	// Idempotency-Key (ADR-0060).
	Idempotency func(http.Handler) http.Handler
}
```

Stack is the built-in middleware of an app, one field per step, which New builds from the configuration (ADR-0083). The fields are in the default order, outermost first; [Stack.Default](#Stack.Default) returns them in that order. Change the order, leave steps out or add your own between them with [WithStack](#WithStack):

```go
gorbital.WithStack(func(s gorbital.Stack) []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		s.Recover, s.TrustedProxies, s.RequestID, requireTenantHeader, // yours, early
		s.Telemetry, s.Observability, s.AccessLog, s.Timeout, s.SecureHeaders, s.CORS,
		s.CrossOrigin, s.BodyLimit, s.Maintenance, s.Auth, s.RateLimit, s.Idempotency,
	}
})
```

*Since `v0.2.0 (unreleased)`*

**Example**

```go
name := func(label string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Print(label, " ")
			next.ServeHTTP(w, r)
		})
	}
}
s := gorbital.Stack{Recover: name("recover"), RequestID: name("request-id"), Auth: name("auth")}
var h http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { fmt.Println("handler") })
for _, mw := range []func(http.Handler) http.Handler{s.Auth, s.RequestID, s.Recover} {
	h = mw(h)
}
h.ServeHTTP(nil, nil)
```

Output:

```text
recover request-id auth handler
```

<a id="Stack.Default"></a>

#### func (Stack) Default

```go
func (s Stack) Default() []func(http.Handler) http.Handler
```

Default returns the steps in the default order, outermost first: the order of a v0.1 app's routes.go, with Timeout added after AccessLog so timed-out requests are logged with their 503.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
var s gorbital.Stack
fmt.Println(len(s.Default()))
```

Output:

```text
15
```

<a id="StorageConfig"></a>
<a id="StorageConfig.Driver"></a>
<a id="StorageConfig.LocalDir"></a>
<a id="StorageConfig.Endpoint"></a>
<a id="StorageConfig.Region"></a>
<a id="StorageConfig.Bucket"></a>
<a id="StorageConfig.AccessKey"></a>
<a id="StorageConfig.SecretKey"></a>
<a id="StorageConfig.PublicURL"></a>
<a id="StorageConfig.PathStyle"></a>
<a id="StorageConfig.SigningKey"></a>

### type StorageConfig

```go
type StorageConfig struct {
	Driver    string // STORAGE_DRIVER: local (default), s3, spaces, r2 or minio
	LocalDir  string // STORAGE_LOCAL_DIR, default .orb/storage
	Endpoint  string // STORAGE_ENDPOINT; defaulted from the region for s3 and spaces
	Region    string // STORAGE_REGION
	Bucket    string // STORAGE_BUCKET
	AccessKey string // STORAGE_ACCESS_KEY
	SecretKey config.Secret
	PublicURL string // STORAGE_PUBLIC_URL
	// PathStyle addresses buckets by path (STORAGE_PATH_STYLE, default on
	// for minio).
	PathStyle bool
	// SigningKey signs local signed URLs (STORAGE_SIGNING_KEY); random per
	// start when empty, so those URLs stop working at a restart.
	SigningKey config.Secret
}
```

StorageConfig is file storage from STORAGE\_\* (ADR-0075). The local driver is built in; S3-compatible drivers are passed by the app ([WithStorage](#WithStorage)), built from these values.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
cfg, err := gorbital.LoadConfig(developmentEnv(map[string]string{
	"APP_ENV": "development", "STORAGE_DRIVER": "s3", "STORAGE_REGION": "eu-west-1",
	"STORAGE_BUCKET": "files", "STORAGE_ACCESS_KEY": "AKIA", "STORAGE_SECRET_KEY": "secret",
}))
if err != nil {
	panic(err)
}
fmt.Println(cfg.Storage.Driver, cfg.Storage.Endpoint, cfg.Storage.Bucket, cfg.Storage.SecretKey)
```

Output:

```text
s3 s3.eu-west-1.amazonaws.com files [redacted]
```

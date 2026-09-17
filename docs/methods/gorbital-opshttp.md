# gorbital/opshttp

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital/opshttp"
```

Package opshttp is the operations API as a gorbital module: the admin endpoints under /ops/ for runtime settings, feature flags, background jobs, the audit log, releases, email and its suppression list, file storage, sign-in methods and rate limits, system health, retention, live observability and incidents (ADR-0026, ADR-0051, ADR-0064).

An app adds it in main.go:

```go
gorbital.Main(
	gorbital.WithModules(opshttp.Module()),
	gorbital.WithModules(modules.All()...),
)
```

Its paths, operation IDs, request and response schemas, error codes, permissions, roles and audit actions are those of the ops module a v0.1 app generated, and are public API (ADR-0015). Every operation needs an actor holding its ops.\* permission; the module declares the roles platform\_admin (every permission) and ops\_viewer (reading). When OPS\_ALLOWED\_IPS is set, requests from other client addresses get 403 ip\_not\_allowed (ADR-0085).

The module needs an app built by gorbital.New (or gorbital.Main), which gives it the job manager, health checks and what every module declared through gorbital.Platform; mounted by hand with gorbital.Mount, it registers its routes for the OpenAPI document only.

Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).

## Contents

- Constants: [`ProviderResend`](#ProviderResend), [`ProviderSMTP`](#ProviderSMTP)
- Functions: [`Module`](#Module)
- Types:
  - [`Option`](#Option): [`MailProvider`](#MailProvider), [`SignInMethods`](#SignInMethods)
  - [`SignInMethod`](#SignInMethod)

## Constants

<a id="ProviderResend"></a>
<a id="ProviderSMTP"></a>

```go
const (
	ProviderResend = "resend"
	ProviderSMTP   = "smtp"
)
```

Mail providers GET /ops/mail reports.

*Since `v0.2.0 (unreleased)`*

## Functions

<a id="Module"></a>

### func Module

```go
func Module(opts ...Option) gorbital.Module
```

Module returns the operations API. Its name is "ops".

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// cmd/api/main.go of an app with the operations API.
main := func() {
	gorbital.Main(
		gorbital.WithModules(opshttp.Module()),
	)
}
_ = main

// The module's routes, as the openapi command exports them.
mux := http.NewServeMux()
api := openapi.New(mux, "acme-api", "1.0.0", openapi.WithBearerAuth("Session token"))
mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
if err != nil {
	panic(err)
}
openapi.InstallErrors(mapper)
if err := gorbital.Mount(api, mapper, gorbital.Deps{}, opshttp.Module()); err != nil {
	panic(err)
}
fmt.Println(api.OpenAPI().Paths["/ops/settings"].Get.OperationID)

// Every operation needs an authenticated actor holding its permission.
rec := httptest.NewRecorder()
mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ops/settings", nil))
fmt.Println(rec.Code)
```

Output:

```text
ops-list-settings
401
```

## Types

<a id="Option"></a>

### type Option

```go
type Option func(*options)
```

An Option configures [Module](#Module).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
var opts []opshttp.Option
opts = append(opts, opshttp.MailProvider(opshttp.ProviderSMTP))
_ = gorbital.WithModules(opshttp.Module(opts...))
```

<a id="MailProvider"></a>

#### func MailProvider

```go
func MailProvider(name string) Option
```

MailProvider sets the email provider GET /ops/mail reports: [ProviderResend](#ProviderResend) (the default), whose API key and webhook secret it reports as configured or missing, or [ProviderSMTP](#ProviderSMTP). It doesn't change how email is sent: that is gorbital.WithMailer's.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// GET /ops/mail reports SMTP, for an app sending through its own SMTP
// relay with gorbital.WithMailer.
_ = gorbital.WithModules(opshttp.Module(opshttp.MailProvider(opshttp.ProviderSMTP)))
```

<a id="SignInMethods"></a>

#### func SignInMethods

```go
func SignInMethods(list func() []SignInMethod) Option
```

SignInMethods sets what GET /ops/auth/providers lists, usually the authenticator's own report, called on every request. Without it the list is empty.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// GET /ops/auth/providers lists what the authenticator reports.
methods := func() []opshttp.SignInMethod {
	return []opshttp.SignInMethod{
		{Key: "password", Name: "Email and password", Enabled: true},
		{Key: "google", Name: "Sign in with Google", Missing: []string{"GOOGLE_CLIENT_ID"}},
	}
}
_ = gorbital.WithModules(opshttp.Module(opshttp.SignInMethods(methods)))
```

<a id="SignInMethod"></a>
<a id="SignInMethod.Key"></a>
<a id="SignInMethod.Name"></a>
<a id="SignInMethod.Enabled"></a>
<a id="SignInMethod.Detail"></a>
<a id="SignInMethod.Missing"></a>
<a id="SignInMethod.Guide"></a>

### type SignInMethod

```go
type SignInMethod struct {
	// Key identifies the method, such as "passkeys".
	Key string
	// Name is how people call it, such as "Passkeys in browsers".
	Name    string
	Enabled bool
	// Detail describes an enabled method, such as its relying party ID;
	// never a secret.
	Detail string
	// Missing are the environment variables that turn a disabled method on.
	Missing []string
	// Guide is the documentation section that explains the method.
	Guide string
}
```

A SignInMethod is a sign-in method and whether the app has it configured, as GET /ops/auth/providers lists it (ADR-0045). It never holds configuration values.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
m := opshttp.SignInMethod{Key: "passkeys", Name: "Passkeys in browsers", Enabled: true, Detail: "relying party api.example.com"}
fmt.Println(m.Key, m.Enabled)
```

Output:

```text
passkeys true
```

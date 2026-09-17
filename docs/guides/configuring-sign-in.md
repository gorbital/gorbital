# Configuring sign-in

How an app on [`gorbital.Main`](main-go.md) changes sign-in without owning its code: options passed to `authhttp.New`. Without options, sign-in is v0.1's, with the same endpoints, error codes and OpenAPI. The package is `gorbital.dev/gorbital/authhttp` ([Methods](../methods/gorbital-authhttp.md#Option)); the decisions are in [ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md), Phase 6 of the [v0.2 roadmap](../v0.2-roadmap.md).

A v0.1 app changed its generated `internal/modules/auth` for the same reasons. It keeps that code and needs nothing from this guide.

## In main.go

```go
func main() {
	auth := authhttp.New(
		authhttp.MinPasswordLength(14),
		authhttp.RequireMFA("billing_admin"),
		authhttp.APIKeyMaxTTL(30*24*time.Hour),
		authhttp.Brand(mail.Brand{
			LogoURL:      "https://shelfie.example/logo.png",
			SupportEmail: "help@shelfie.example",
			Footer:       "Shelfie Ltd, 1 Main Street, London",
		}),
		authhttp.OnRegister(defaultShelf),
	)
	gorbital.Main(
		gorbital.WithName("shelfie"),
		gorbital.WithAuth(auth),
		gorbital.WithModules(opshttp.Module(), flagshttp.Module()),
		gorbital.WithModules(modules.All()...),
		gorbital.WithMigrations(migrations.FS),
	)
}
```

Keep the `*authhttp.Authenticator` in a variable when a module needs it, such as a module that [adds a sign-in method](adding-a-sign-in-method.md).

## Options

| Option | What it changes | Limits |
|---|---|---|
| [`MinPasswordLength(n)`](../methods/gorbital-authhttp.md#MinPasswordLength) | The shortest password accepted when an account registers, resets or changes its password, or an operator creates one | 12 to 128 characters: it only raises v0.1's minimum. Existing passwords keep working |
| [`PasswordPolicy(check)`](../methods/gorbital-authhttp.md#PasswordPolicy) | A check every new password must pass after the built-in rules, such as a breached-password lookup | Several run in order |
| [`RequireMFA(roles...)`](../methods/gorbital-authhttp.md#RequireMFA) | A role's permissions are granted only to sessions signed in with a second factor, as for `platform_admin` and `ops_viewer` | The role must be declared by a module; not `user` |
| [`APIKeyMaxTTL(d)`](../methods/gorbital-authhttp.md#APIKeyMaxTTL) | The longest lifetime of new API keys | 24 hours to 365 days |
| [`WithoutRegistration()`](../methods/gorbital-authhttp.md#WithoutRegistration) | Closes sign-up | Can't be combined with `RegisterFields` |
| [`Brand(b)`](../methods/gorbital-authhttp.md#Brand) | The logo, support address and footer of sign-in's emails | |
| [`RouteMiddleware(mws...)`](../methods/gorbital-authhttp.md#RouteMiddleware) | Middleware on every operation under `/v1/auth/` | Not on `/ops/auth/users` or `/ops/service-accounts` |
| [`BeforeLogin`](../methods/gorbital-authhttp.md#BeforeLogin), [`AfterLogin`](../methods/gorbital-authhttp.md#AfterLogin), [`OnRegister`](../methods/gorbital-authhttp.md#OnRegister) | Hooks: the app's code in sign-in and account creation | [Sign-in hooks](sign-in-hooks.md) |
| [`RegisterFields(save)`](../methods/gorbital-authhttp.md#RegisterFields) | Fields of the app's own on `POST /v1/auth/register` | Given once. [Extra registration fields](extra-registration-fields.md) |

### Invalid options stop the start

A value outside its limits, a `nil` hook, `RegisterFields` given twice or with `WithoutRegistration`: [`Authenticator.CheckConfig`](../methods/gorbital-authhttp.md#Authenticator.CheckConfig) reports each one after the environment's problems, so `gorbital.Main` exits with status 2 before it connects to anything.

```text
shelfie: invalid configuration:
authhttp: MinPasswordLength(8): a minimum is 12 to 128 characters; a lower one would weaken v0.1's policy
```

`RequireMFA` needs the permission catalog, so its mistakes (a role no module declares, or `user`) are reported by `Setup`, when `gorbital.New` builds the app, and the app doesn't start either.

## Passwords

```go
noAppName := func(_ context.Context, password string) error {
	if strings.Contains(strings.ToLower(password), "shelfie") {
		return errors.New("must not contain the app's name")
	}
	return nil
}
auth := authhttp.New(authhttp.MinPasswordLength(14), authhttp.PasswordPolicy(noAppName))
```

- A refused password answers 422 `weak_password`. The detail is `the password ` followed by the error's text, so word the error to follow it: "must not contain the app's name", "is too common".
- The minimum and the policies apply wherever a new password is chosen: registration, password reset, password change and `POST /ops/auth/users`. Lengths count characters, not bytes.
- A policy runs for every request before anything is stored, so its answer is the same whether or not the address has an account.
- A policy that calls another service, such as a breached-password lookup, gets the request's context: respect its deadline. Never log the password.

## Second factors for your roles

`platform_admin` and `ops_viewer` already require a second factor. `RequireMFA` does the same for roles your modules declare:

```go
authhttp.New(authhttp.RequireMFA("billing_admin"))
```

A session signed in without a second factor gets 403 `mfa_required` from the routes the role opens, and an API key never holds the role's permissions. An account holding the role can't turn two-factor authentication off (409 `mfa_required_by_role`). Grant the role with `go run ./cmd/api grant-role <email> billing_admin` ([authentication](authentication.md#your-first-administrator)).

## API key lifetime

`APIKeyMaxTTL(d)` narrows the runtime setting `auth.api_key_max_ttl` ([API keys](api-keys.md#limits-and-settings)): operators can set it to at most `d`, and its default becomes `d` when `d` is shorter than 90 days. Keys created before keep their expiry.

## Closing sign-up

`WithoutRegistration()` is for apps whose accounts come from operators or from the app's own flows, such as an internal tool:

| Flow | With `WithoutRegistration()` |
|---|---|
| `POST /v1/auth/register` | 404; the operation leaves the OpenAPI document |
| First Google, Apple or GitHub sign-in of an address without an account | 403 `registration_closed`; `#error=registration_closed` for web sign-ins. Nothing is written |
| Accounts operators create (`POST /ops/auth/users`) | Work, with their verification and password reset |
| Existing accounts: password, passkey, provider sign-in, linking a provider | Work |
| Methods modules add through `Authenticator.SignIn` | Work: they sign existing accounts in |

## Emails

`Brand` sets what every sign-in email has in common (`mail.Brand`): `LogoURL`, `SupportEmail`, `Footer`. An empty `Name` is the app's name (`gorbital.WithName`) and an empty `URL` is `APP_PUBLIC_URL`, as without the option. The dev console's email previews use the same brand. Sender name and address stay in the `mail.*` runtime settings ([email](email.md#set-the-sender)).

## Middleware on sign-in's routes

`RouteMiddleware` runs on every operation under `/v1/auth/`, after the app's middleware stack and before sign-in's own checks, such as a CAPTCHA check or a country filter:

```go
captcha := func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !validCaptcha(r) {
			httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusBadRequest, "captcha_required", "solve the CAPTCHA first"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
auth := authhttp.New(authhttp.RouteMiddleware(captcha))
```

Refuse with a problem and a code of the app's, following the rules for [writing middleware](guards-and-middleware.md#rules). It doesn't run on `/ops/auth/users` or `/ops/service-accounts`. Middleware for the whole app goes in `gorbital.WithMiddleware`.

## What stays in environment variables

Values that differ between environments, and secrets, stay where v0.1 has them. There is no option for them:

| Configured by | Variables |
|---|---|
| Authenticator apps | `AUTH_ENCRYPTION_KEYS` |
| Passkeys | `WEBAUTHN_RP_ID`, `WEBAUTHN_ORIGINS`, `WEBAUTHN_APPLE_APP_IDS`, `WEBAUTHN_ANDROID_APPS` |
| Google, Apple and GitHub | `GOOGLE_*`, `APPLE_*`, `GITHUB_*`, `APP_PUBLIC_URL`, `AUTH_DEFAULT_RETURN_TO` |
| Limits operators change at runtime | `auth.*` runtime settings ([authentication](authentication.md#settings)) |

A method is on when its variables are set and off when they are empty; [sign-in provider setup](auth-providers.md) lists every value, and `go run ./cmd/api auth-providers` shows which methods are on.

## Options that were planned and dropped

The roadmap listed more options. They were left out because each would break something that works today (ADR-0083, Phase 6 notes):

| Dropped | Why |
|---|---|
| `Prefix` (moving `/v1/auth`) | The callback URLs registered at Google, Apple and GitHub, Apple's server notifications URL, the stack's `CrossOrigin` exceptions and its `auth_ip` rate limit on `/v1/auth/`, and the frozen v0.1 contract all name the path |
| `Google`, `Apple`, `GitHub`, `Passkeys` | Their values differ per environment and include secrets, which belong in environment variables. Values in code would make `/ops/auth/providers` and `auth-providers` disagree with `.env`. A method is off by leaving its variables unset |
| `MFA(TOTP, RecoveryCodes)` | Turning authenticator apps off in code would lock out the users who enrolled one, and turning recovery codes off removes self-service recovery. Sign-in's defaults are never weakened silently; `RequireMFA` is what apps needed |

## Related

- [Sign-in hooks](sign-in-hooks.md): `BeforeLogin`, `AfterLogin` and `OnRegister`.
- [Extra registration fields](extra-registration-fields.md): `RegisterFields`.
- [Adding a sign-in method](adding-a-sign-in-method.md): `Authenticator.SignIn`.
- [Authentication](authentication.md): the flows, endpoints and error codes.
- [Shelfie, chapter 6](../examples/shelfie/06-accounts.md): options and hooks in an app.

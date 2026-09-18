# 4. The sign-in you didn't write

Plateful's `cmd/api/main.go` contains one line about authentication:

```go
auth := authhttp.New(signInOptions()...)
```

Behind it are sixty-six HTTP operations, twelve database tables, email verification, sessions, two-factor authentication, passkeys, Google, Apple and GitHub, API keys and an operator's account-management API. This chapter says exactly what you get, what you can change without touching any of it, and what it costs to take ownership of the code when you can't.

## 1. What one line gives you

**What we're doing.** Adding sign-in.

**Why.** Authentication is the part of an application that is least interesting to build and most expensive to get wrong. Password hashing, timing-safe lookups, verification-code expiry, session rotation, rate limits on the routes that matter, and answers that don't reveal whether an address has an account — none of it is your product.

**What the framework already gives us.** All of it.

**What we build ourselves.** In this chapter: two extra fields on the registration form.

**How.**

```go
auth := authhttp.New()

gorbital.Main(
	gorbital.WithAuth(auth),
	// …
)
```

**What just happened.** These routes now exist. This is the complete list with no options passed:

### Registration and verification

| Method and path | What it does |
|---|---|
| `POST /v1/auth/register` | Create an account; emails a 6-digit code, answers `202` |
| `POST /v1/auth/verify-email` | Verify an address with its code |
| `POST /v1/auth/verify-email/resend` | Send a new code |

### Signing in, and passwords

| Method and path | What it does |
|---|---|
| `POST /v1/auth/login` | Sign in — `200` with a session, or `202` with a second-factor challenge |
| `POST /v1/auth/password/forgot` | Email a reset code |
| `POST /v1/auth/password/reset` | Set a new password with a reset code |
| `PUT /v1/auth/password` | Change the password |

### The signed-in account and its sessions

| Method and path | What it does |
|---|---|
| `GET /v1/auth/me` | The signed-in user |
| `POST /v1/auth/logout` | Sign out this device |
| `POST /v1/auth/logout-all` | Sign out every device |
| `GET /v1/auth/sessions` | List signed-in devices |
| `DELETE /v1/auth/sessions/{id}` | Sign out one device |
| `DELETE /v1/auth/me` | Delete the account |

### Two-factor authentication

| Method and path | What it does |
|---|---|
| `POST /v1/auth/login/mfa` | Finish signing in with a second factor |
| `POST /v1/auth/mfa/totp` | Start setting up an authenticator app |
| `POST /v1/auth/mfa/totp/confirm` | Turn it on; returns ten recovery codes |
| `DELETE /v1/auth/mfa/totp` | Turn it off |
| `POST /v1/auth/mfa/recovery-codes` | Replace the recovery codes |

### Passkeys

| Method and path | What it does |
|---|---|
| `POST /v1/auth/passkeys/registration` | Start adding a passkey |
| `POST /v1/auth/passkeys` | Add one |
| `GET /v1/auth/passkeys` | List them |
| `PATCH /v1/auth/passkeys/{id}` | Rename one |
| `DELETE /v1/auth/passkeys/{id}` | Remove one |
| `POST /v1/auth/passkeys/verification` | Confirm a change with a passkey |
| `POST /v1/auth/passkeys/login/options` | Start signing in with a passkey |
| `POST /v1/auth/passkeys/login` | Sign in with one |
| `POST /v1/auth/login/mfa/passkey` | Start a passkey second factor |

### Google, Apple and GitHub

| Method and path | What it does |
|---|---|
| `GET /v1/auth/{provider}/start` | Begin a browser sign-in (`302`) |
| `GET /v1/auth/google/callback` | Google's return (`303`) |
| `GET /v1/auth/github/callback` | GitHub's return (`303`) |
| `POST /v1/auth/apple/callback` | Apple's return (`303`) |
| `POST /v1/auth/{provider}/nonce` | A nonce for a native sign-in |
| `POST /v1/auth/google/token` | Sign in with a Google ID token |
| `POST /v1/auth/apple/token` | Sign in with an Apple ID token |
| `GET /v1/auth/identities` | Linked accounts |
| `POST /v1/auth/identities` | Link a Google or Apple account |
| `POST /v1/auth/{provider}/link` | Start linking a GitHub account |
| `DELETE /v1/auth/identities/{id}` | Unlink one |
| `POST /v1/auth/apple/notifications` | Apple's server-to-server notifications |

Each provider is off until its environment variables are set. `go run ./cmd/api auth-providers` reports which are configured.

### API keys and service accounts

| Method and path | What it does |
|---|---|
| `GET /v1/auth/api-keys` | Your API keys |
| `POST /v1/auth/api-keys` | Create one; the key is shown once |
| `DELETE /v1/auth/api-keys/{id}` | Revoke one |
| `GET`/`POST` `/ops/service-accounts` | List and create service accounts |
| `GET`/`PATCH`/`DELETE` `/ops/service-accounts/{id}` | Manage one |
| `GET`/`POST` `/ops/service-accounts/{id}/keys` | Its API keys |
| `DELETE /ops/service-accounts/{id}/keys/{keyId}` | Revoke one |

### The operator's account API, under `/ops/auth/users`

Sixteen operations: list, create, get and delete accounts; mark an address verified; ban and unban; grant and revoke platform roles; end all or one session; remove a passkey; unlink a provider; enrol or reset an account's second factors; and, in development only, start a session as an account.

### Two more, conditionally

`GET /.well-known/apple-app-site-association` and `GET /.well-known/assetlinks.json` appear only when `WEBAUTHN_APPLE_APP_IDS` or `WEBAUTHN_ANDROID_APPS` are set — the association files iOS and Android need before a passkey works in a native app.

And organisation service accounts (`/v1/orgs/{orgId}/service-accounts`, eight operations) come from `orgshttp.Module(auth)`, not from `authhttp.New()` — which is why the organisations module takes the authenticator as an argument.

**What this is not.** There is no sign-in *page*. These are JSON APIs; the form, the redirect handling and the passkey ceremony in the browser are your client's. In development the Dev Portal has a sign-in tester so you can exercise them without writing one first.

The behaviour behind each route — rate limits, what users see, error codes, the `auth.*` settings — is in [Authentication](../guides/authentication.md). This chapter is about changing it.

## 2. The options

**What we're doing.** Adapting sign-in without owning it.

**Why.** Almost every app wants something slightly different: a longer password, a closed sign-up, a second factor for admins, its own branding on the emails.

**What the framework already gives us.** Eleven options. They are the whole supported surface, and passing an invalid one ends the program with exit code 2 before anything connects, rather than failing at the first request.

| Option | What it does |
|---|---|
| `MinPasswordLength(n int)` | Raises the minimum from 12, up to 128. Counts characters, not bytes |
| `PasswordPolicy(check func(ctx, password string) error)` | An extra check after the built-in rules. A non-nil error is `422 weak_password`. Several run in order |
| `RequireMFA(roles ...string)` | These roles' permissions are granted only to sessions that passed a second factor |
| `APIKeyMaxTTL(d time.Duration)` | Caps the `auth.api_key_max_ttl` setting, between 24 hours and 365 days |
| `WithoutRegistration()` | Removes `POST /v1/auth/register` from the mux *and* the OpenAPI document; a first social sign-in by an unknown address gets `403 registration_closed` |
| `Brand(b mail.Brand)` | Name and URL in sign-in's emails |
| `RouteMiddleware(mw ...func(http.Handler) http.Handler)` | Middleware on every `/v1/auth/` operation — not on `/ops/auth/users` or `/ops/service-accounts` |
| `BeforeLogin(hook)` | See below |
| `AfterLogin(hook)` | See below |
| `OnRegister(hook)` | See below |
| `RegisterFields[T](save)` | See below |

[Configuring sign-in](../guides/configuring-sign-in.md) documents each in full, with its limits. Read it before deciding you need more than it offers.

## 3. The four hooks

Three of them run inside a transaction, which is the reason they are useful: what they write commits with the account or the session, or not at all.

| Hook | When | Transaction | Can it refuse? |
|---|---|---|---|
| `BeforeLogin` | After every factor is verified, just before the session is created | Yes — the session's | **Yes** |
| `AfterLogin` | After the session is committed and audited | No | No |
| `OnRegister` | Inside the transaction that creates an account, **however** it was created | Yes | Yes, for some paths |
| `RegisterFields[T]` | Same transaction, after the `OnRegister` hooks | Yes | Validation only |

### `BeforeLogin`

```go
func BeforeLogin(hook func(ctx context.Context, tx pgx.Tx, a LoginAttempt) error) Option
```

It sees a sign-in whose every factor is already verified. Return `authhttp.Refuse(code, detail)` and the caller gets `403` with your code; any other error is `500`; `nil` lets it through. `tx` is the transaction that creates the session, so you can read your own tables in it and whatever you write commits with the session.

What it deliberately **cannot** see: unknown addresses, wrong passwords, failed second factors, or a banned account (refused before the hooks run). That is not an oversight — a hook that could observe those could be used to tell a stranger whether an address has an account here. A refusal is only ever shown to someone who passed every check.

This is where "is this account suspended in *our* sense?" goes.

### `AfterLogin`

```go
func AfterLogin(hook func(ctx context.Context, e LoginEvent) error) Option
```

No transaction, and it cannot change the outcome: its error is logged. The response waits at most **five seconds**, after which its context is cancelled and the response goes out regardless; a panic is recovered and logged. The event carries the session ID but never the token.

> **Don't do this:** call a slow third party here. You have a five-second budget and the user is waiting for it.
> **Do this instead:** enqueue a job. `AfterLogin` is for "record that this happened", not for "do a thing".

### `OnRegister`

```go
func OnRegister(hook func(ctx context.Context, tx pgx.Tx, a NewAccount) error) Option
```

It runs in the transaction that creates an account — for **every** way one is created: a password registration, a first Google/Apple/GitHub sign-in, and an operator creating one through `/ops` or `seed`. `a.Method` tells you which. Write the account's rows in `tx`: a profile, a default workspace, a job with `jobs.Client.InsertTx`.

An error rolls the account back, and what the client sees depends on the path:

| Created by | A hook error means |
|---|---|
| `POST /v1/auth/register` | The account is rolled back, the error logged — and the response is **still `202`** |
| A first social sign-in | Rolled back; `Refuse` gives `403` with your code, anything else `500` |
| An operator | Rolled back; `403` with a refusal's code, otherwise `500` |

That first row is the one to internalise. Registration must answer identically whether or not the address already has an account, and hooks run only for new ones — so any other answer would leak. **Refuse bad input in validation, not in a hook.**

### `RegisterFields[T]`

```go
func RegisterFields[T any](save func(ctx context.Context, tx pgx.Tx, a NewAccount, fields T) error) Option
```

`T` is a struct whose fields are added to `POST /v1/auth/register` beside `email` and `password`. Huma validates them from the struct tags — and from a `Resolve` method on `*T`, if you write one — for every request, before anything is stored, and the OpenAPI document shows them. `save` runs in the account's transaction, after the `OnRegister` hooks.

Four rules: `T` must be a struct; its JSON names may not be `email` or `password`; `RegisterFields` may be given once; and it cannot be combined with `WithoutRegistration`.

**Only email registration sends them.** An account from a first Google sign-in, or from an operator, runs the `OnRegister` hooks without `save`. Nothing downstream may assume the profile exists.

## 4. What Plateful does

**What we're doing.** Asking for a display name and a delivery address at registration.

**Why.** An account with nowhere to deliver to is an account that has to be interrupted the first time it orders.

**What the framework already gives us.** `RegisterFields`, validation, the transaction, and the OpenAPI schema.

**What we build ourselves.** A struct, a `Resolve` method, and a function that writes one row.

**How.** The whole sign-in customisation is two options:

<!-- include examples/apps/plateful/cmd/api/signin.go#sign-in-options -->

And the fields themselves live in the module that owns customers, not in `cmd/api`:

<!-- include examples/apps/plateful/internal/modules/orders/hooks.go#registration-fields -->

Three things in there are worth copying.

**`Resolve` runs before anything is stored.** A display name of three spaces is answered with `422 validation_failed` whether or not the address already has an account — because Huma checks the request, and the check doesn't touch the database. Put the rule in `Resolve` and a registration answers the same either way. Put it in `save` and it can't: `save` runs *after* the account exists, and its error produces the same `202` as success.

> **Don't do this:** validate in `save`. The response is `202` regardless, so the caller gets a success for a request you rejected, and the only trace is a log line.
> **Do this instead:** validate in `T` — struct tags for the shape, `Resolve` for the rules. `save` should only fail on something genuinely unexpected.

**`save` writes through the transaction it is given.** `repository.NewStoreOn(tx)` binds the store to `tx`, not to the pool. The account and the profile exist together or not at all.

**Nothing assumes the profile exists.** A customer who signed in with Google never filled this form, so Plateful's `PlaceOrder` falls back to the address in the request when there is no profile. That is the price of `RegisterFields` only applying to email registration, and it has to be paid somewhere — either a fallback like this one, or a "complete your profile" step in the client.

Note also what Plateful does *not* use: no `BeforeLogin`, no `AfterLogin`, no `OnRegister`, no `Brand`, no `RequireMFA`. Sign-in's defaults were right, apart from fourteen-character passwords — because customers pay for food here — and two fields on the form.

Deeper treatments, rather than repeating them here: [Sign-in hooks](../guides/sign-in-hooks.md) (the order of the sign-in, the refusal codes, what clients receive) and [Extra registration fields](../guides/extra-registration-fields.md) (validation, the transaction order, the "complete your profile" pattern, and why these fields are unverified input).

## 5. What you cannot change

Options and hooks are a deliberately closed set. These are fixed, and several were proposed as options and rejected:

| Fixed | Why |
|---|---|
| The `/v1/auth` path prefix | The callback URLs registered at Google, Apple and GitHub name it; so do the middleware stack's CORS exceptions and its `auth_ip` rate limit; so does the frozen v0.1 API contract |
| Whether Google, Apple, GitHub or passkeys exist as code | Their values differ per environment and include secrets, so they live in environment variables. A method is off by leaving its variables unset |
| Turning off authenticator apps or recovery codes | Turning TOTP off would lock out everyone who enrolled; turning recovery codes off removes self-service recovery. `RequireMFA` is the knob apps actually needed |
| The session cookie's name | |
| The error codes and their HTTP statuses | They are public API: new ones may be added, existing ones never change |
| The table layout and the migrations | |
| The permission names | |

If what you want is on that list, or is a change to what sign-in *does* — a different session model, another table layout, removing operations your API must not have — then options won't reach it, and the honest answer is ejection.

## 6. Ejecting, and what it costs

```bash
orb eject auth
```

It copies `gorbital.dev/gorbital/authhttp` — at the version your `go.mod` requires — into `internal/modules/auth`: the root package and its four layers, with tests, with import paths rewritten to your app's. It rewrites `cmd/api`'s imports so `main.go` builds the module from your copy with the same options and hooks (the package keeps its name, so **no call changes**). It copies the module's migrations into `db/migrations` under the same versions, so the database sees nothing new. Then it records the ejection in `gorbital.lock` and runs `go mod tidy`.

```text
✓ Ejected auth: gorbital.dev/gorbital/authhttp v0.2.0 is now internal/modules/auth
```

The API, the database and the behaviour are unchanged. That is the good news.

> [!WARNING]
> **The cost is permanent and it is a security cost.** An ejected module is your code. Fixes and features the library ships for it no longer reach your app — **security fixes included**.

The tooling helps, but only by telling you:

- `orb doctor` has an `ejected` check. It passes while the library's package at the version `go.mod` requires still matches what was copied; it warns when that package has changed, and quotes the changelog entries naming it.
- Acting on the warning is manual: read the entries, diff `internal/modules/auth` against the package in your module cache, and port the fixes you need.

Two practical notes:

- **Eject `orgs` before `auth`.** `orgshttp.Module` takes `*authhttp.Authenticator`, so the library's organisations module would not fit your copy. `orb eject` detects it and refuses with the order to use.
- **Going back is by hand.** There is no un-eject command.

`orb eject` also handles `flags`, `mailevents`, `ops` and `orgs`. And it is another thing that requires the v0.2 layout — see [chapter 1](01-create-the-app.md#2-why---preset-full-is-not-a-preference).

> **Don't do this:** eject to change one thing. Every later library release becomes your merge.
> **Do this instead:** work down the list — an option, then a hook, then a module of your own alongside sign-in (Plateful's `customers` table lives in the `orders` module, not in an ejected copy of `authhttp`), and only then eject. [Ejecting a module](../guides/ejecting-a-module.md) has a table mapping common wishes to the option or hook that covers them.

## What just happened

You got sixty-six operations for one line, changed two things about them with two options, and added two fields to the registration form by writing one struct and one function — both of which live in your own module, in the transaction that creates the account.

You also now know the boundary: what options reach, what hooks reach, and what only ejection reaches. That boundary is the same idea as [chapter 2](02-framework-and-your-app.md)'s, applied to the largest thing the library gives you.

Next: [chapter 5](05-the-restaurants-module.md) builds the first module of your own.

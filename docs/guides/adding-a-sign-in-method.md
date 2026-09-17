# Adding a sign-in method

How a module of the app adds a way to sign in that sign-in doesn't have, such as a code sent to a phone, and gets the same sessions, second factors, bans, hooks, rate limits and audit events as a password. The methods are [`Authenticator.SignIn`](../methods/gorbital-authhttp.md#Authenticator.SignIn) and [`Authenticator.User`](../methods/gorbital-authhttp.md#Authenticator.User); the decisions are in [ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md), Phase 6 of the [v0.2 roadmap](../v0.2-roadmap.md).

Before writing one, check sign-in doesn't already do what you need: passkeys, Google, Apple and GitHub are built in ([authentication](authentication.md)). A method of your own is code you must get right; this guide lists what that takes.

## How it fits

The module proves who the person is. `SignIn` does everything after that:

```text
module                                                    authhttp
──────                                                    ────────
POST /v1/phone-sign-in/code  {phone}
  rate limit, look up a confirmed phone,
  send a single-use code (or pretend to) ─► 202

POST /v1/phone-sign-in       {phone, code}
  rate limit, check the code, use it up,
  find its account's ID  ─────────────────────────────►  SignIn(ctx, {UserID, Method: "phone_code", Transport})
                                                           login rate limits, verified address,
                                                           2FA challenge, ban, BeforeLogin,
                                                           session, audit, AfterLogin
                                     ◄─────────────────── 200 session, or 202 challenge
```

`SignIn` doesn't know how the method works and checks nothing about it. Everything in [what the module must verify](#what-the-module-must-verify) is the module's job.

## The module takes the authenticator

A module that calls `SignIn` receives the `*authhttp.Authenticator` as an argument of its constructor, with anything else it needs, such as a text message sender:

```go
// Module returns the phone sign-in module. auth is the Authenticator main.go
// passes to gorbital.WithAuth; send delivers text messages.
func Module(auth *authhttp.Authenticator, send Sender) gorbital.Module {
	return gorbital.Module{
		Name: "phonelogin",
		Errors: []httpx.Mapping{
			{Err: ErrInvalidCode, Status: http.StatusUnauthorized, Code: "invalid_phone_code", Detail: "the code is wrong, used or expired"},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			h := &handler{auth: auth, send: send, db: d.DB}
			g := r.Group("/v1/phone-sign-in", gorbital.Tags("Phone sign-in"))
			gorbital.Post(g, "/code", h.sendCode, gorbital.Status(http.StatusAccepted),
				guard.Public(), guard.RateLimit(10, time.Hour, guard.ByIP()))
			gorbital.Post(g, "", h.signIn, gorbital.Summary("Sign in with a code sent to your phone"),
				guard.Public(), guard.RateLimit(30, time.Hour, guard.ByIP()))
		},
	}
}
```

`gorbital` doesn't import `authhttp`, so `Deps` has no sign-in field: the authenticator reaches a module only this way. main.go passes the same value it gives to `WithAuth`:

```go
func main() {
	auth := authhttp.New()
	gorbital.Main(
		gorbital.WithName("shelfie"),
		gorbital.WithAuth(auth),
		gorbital.WithModules(opshttp.Module(), flagshttp.Module()),
		gorbital.WithModules(modules.All()...),
		gorbital.WithModules(phonelogin.Module(auth, newSMSSender())), // takes arguments: not in modules.All
		gorbital.WithMigrations(migrations.FS),
	)
}
```

`orb gen modules` lists only modules whose package declares `func Module() gorbital.Module` without arguments ([the module list](main-go.md#the-module-list)), so a module that takes the authenticator isn't in `modules.All()`: add it with its own `WithModules`, as above. `newSMSSender` stands for the app's own text message client.

## The sign-in route

The route's handler checks the code and returns what `SignIn` returns, as it is:

```go
type signInInput struct {
	Body struct {
		Phone     string `json:"phone" pattern:"^\\+[1-9][0-9]{7,14}$"`
		Code      string `json:"code" pattern:"^[0-9]{6}$"`
		Transport string `json:"transport,omitempty" enum:"cookie,bearer" default:"cookie"`
	}
}

func (h *handler) signIn(ctx context.Context, in *signInInput) (*authhttp.SignedIn, error) {
	userID, err := h.verifyCode(ctx, in.Body.Phone, in.Body.Code) // the module's use case, with its limits
	if err != nil {
		return nil, err // 401 invalid_phone_code
	}
	return h.auth.SignIn(ctx, authhttp.SignInRequest{UserID: userID, Method: "phone_code", Transport: in.Body.Transport})
}
```

[`SignedIn`](../methods/gorbital-authhttp.md#SignedIn) is `POST /v1/auth/login`'s response, and its body schema is login's `LoginResponse` in the OpenAPI document, so clients handle both routes with the same code.

| `SignInRequest` field | Value |
|---|---|
| `UserID` | The account the verified credential belongs to, from the module's own records. **Never from the request** |
| `Method` | The method's name in audit events and hooks: lowercase snake_case of 2 to 32 characters, not `password`, `passkey`, `operator`, `google`, `apple`, `github`, `mfa`, `api_key` or `impersonation`. An invalid name is a plain error (500): a programming mistake |
| `Transport` | `cookie` (browsers, the default) sets the `__Host-session` cookie; `bearer` (native apps) returns the token in the body. As in `POST /v1/auth/login` |

## What SignIn does

In this order, as `POST /v1/auth/login` after a correct password:

| Case | Response |
|---|---|
| No account with `UserID`, or a deleted one | 401 `invalid_credentials`, audited as `auth.login.failed` |
| The account's login rate limits (`auth_login`, `auth_login_address`) are used up | 429 `too_many_attempts` |
| The address isn't verified | 403 `email_not_verified` |
| Two-factor authentication is on | 202 with `mfa.challenge_token`, `methods` and `expires_at`; the client finishes with `POST /v1/auth/login/mfa` |
| The account is banned | 403 `account_banned` |
| A [`BeforeLogin`](sign-in-hooks.md#when-beforelogin-runs) hook refuses | 403 with the hook's code |
| Otherwise | 200 with the user and session (and `token` for `bearer`), audited as `auth.login.succeeded` with `method` in its metadata; then the [`AfterLogin`](sign-in-hooks.md#afterlogin) hooks run |

Return these errors from the handler unchanged: they are problems, or sign-in's errors the app already maps. `SignIn` called before `gorbital.New` has set sign-in up returns a plain error; routes run only after it, so a handler never sees that.

v0.1 sends no "new device" emails, and neither does `SignIn`. Hooks and audit events see `Method` as `phone_code`, so a `BeforeLogin` hook can refuse the method for some accounts, such as administrators ([`LoginAttempt`](../methods/gorbital-authhttp.md#LoginAttempt)).

### Accounts with two-factor authentication

A phone code is one factor. For an account with two-factor authentication, `SignIn` answers 202 like a correct password does, and the client sends the challenge token with a code from an authenticator app, a recovery code or a passkey to `POST /v1/auth/login/mfa` ([authentication](authentication.md#two-factor-authentication)). The final audit event and the `BeforeLogin` hooks then know the sign-in started with `phone_code`.

The challenge token names the method: a sign-in that didn't start with a password gets a token ending in a dot and the method, here `.phone_code` (a password sign-in's token is v0.1's). Only the hash of the whole token is stored, so a client that changes the suffix has an unknown challenge. Clients treat the token as opaque, as they already do.

## What the module must verify

`SignIn` trusts the `UserID` it is given. Before calling it, the module must have established that the person controls a credential bound to that account:

| Rule | For a phone code |
|---|---|
| **The credential was bound by the account** | A phone number counts only after the signed-in account confirmed it with a code sent to it. Store `(user_id, phone, confirmed_at)`, one account per number |
| **Single-use, short-lived codes** | Six random digits from `crypto/rand`, valid for a few minutes, deleted or marked used on the first correct try |
| **Stored hashed** | Keep a hash of the code (`auth.HashToken` from `gorbital.dev/modules/auth`), never the code, so a database read can't sign anyone in |
| **Limited attempts per code** | Count wrong tries on the code's row; after 5, the code is dead and a new one must be sent |
| **Rate limits on every route** | `guard.RateLimit(…, guard.ByIP())` on sending and checking; a per-number limit on sending (texts cost money and annoy); `SignIn` adds the account's login limits |
| **The same answer whether or not an account exists** | Sending answers 202 for any well-formed number, confirmed or not, and takes about the same time; checking answers the same error for an unknown number as for a wrong code |
| **Constant-time comparison** | Compare hashes with `crypto/subtle.ConstantTimeCompare` |
| **Never a user ID from the request** | The route takes the phone and the code, and reads the account from the module's own row |
| **Never log codes** | Log the user ID and outcome, not the code or the full number |

A route outside `/v1/auth/` doesn't get what the stack does for sign-in's own routes: the `auth_ip` rate limit per client address, the maintenance-mode exception and [`RouteMiddleware`](configuring-sign-in.md#middleware-on-sign-ins-routes). Add the guards above yourself.

## Showing the account

A module that shows the account its rows point to uses [`Authenticator.User`](../methods/gorbital-authhttp.md#Authenticator.User):

```go
u, err := h.auth.User(ctx, row.UserID)
if err != nil {
	return nil, err // authhttp.ErrUserNotFound answers 404 user_not_found
}
```

`User` returns the ID, email, whether the address is verified, whether the account has a password or is banned, its creation time and its platform roles. It never returns the password hash.

## Testing

Pass a fake sender that keeps the codes, and test through the real routes with [gorbitaltest](testing-with-gorbitaltest.md):

```go
type fakeSender struct{ codes map[string]string } // phone → last code

func TestPhoneSignIn(t *testing.T) {
	auth := authhttp.New()
	sender := &fakeSender{codes: map[string]string{}}
	app := gorbitaltest.New(t, gorbital.WithAuth(auth), gorbital.WithModules(phonelogin.Module(auth, sender)))
	// create and verify an account, confirm its phone, then:
	app.Client().Post("/v1/phone-sign-in/code", map[string]any{"phone": "+447700900123"}).AssertStatus(t, http.StatusAccepted)
	res := app.Client().Post("/v1/phone-sign-in", map[string]any{"phone": "+447700900123", "code": sender.codes["+447700900123"], "transport": "bearer"})
	res.AssertStatus(t, http.StatusOK)
}
```

Test at least:

- a confirmed number signs in, with `cookie` and with `bearer`;
- an unknown number and a wrong code get the same answer, and sending answers 202 for both;
- a code works once, expires, and dies after its attempts;
- the rate limits answer 429;
- an account with two-factor authentication gets 202, and `POST /v1/auth/login/mfa` finishes it;
- a banned account gets 403 `account_banned`, and an unverified one 403 `email_not_verified`;
- the audit log has `auth.login.succeeded` with `method` `phone_code`.

## Related

- [Shelfie, chapter 7](../examples/shelfie/07-phone-code-sign-in.md): the phone code module, complete, with its tests.
- [Shelfie, chapter 6](../examples/shelfie/06-accounts.md): accounts, options and hooks.
- [Sign-in hooks](sign-in-hooks.md): they run for modules' methods too.
- [Guards and middleware](guards-and-middleware.md#rate-limits): rate limits.
- [Configuring sign-in](configuring-sign-in.md).

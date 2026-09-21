# Sign-in hooks

How an app runs its own code when someone signs in or an account is created, without editing sign-in: refuse a suspended reader, record a login, create a profile and a default workspace. Hooks are [options](configuring-sign-in.md) of `authhttp.New` ([Methods](../methods/gorbital-authhttp.md#BeforeLogin)); the decisions are in [ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md#phase-6-implementation-notes-sign-in-options-hooks-and-custom-methods-2026-09-17), Phase 6 of the [v0.2 roadmap](../v0.2-roadmap.md).

| Hook | Runs | Can refuse | Transaction |
|---|---|---|---|
| [`BeforeLogin(func(ctx, tx, LoginAttempt) error)`](../methods/gorbital-authhttp.md#BeforeLogin) | Once every factor of a sign-in is verified, just before the session is created | Yes: 403 with the app's code | The one that creates the session |
| [`AfterLogin(func(ctx, LoginEvent) error)`](../methods/gorbital-authhttp.md#AfterLogin) | After the session is committed and audited | No: errors are logged | None |
| [`OnRegister(func(ctx, tx, NewAccount) error)`](../methods/gorbital-authhttp.md#OnRegister) | While an account is created, however it is created | Rolls the account back; what the client sees depends on the path ([below](#what-clients-receive)) | The one that creates the account |

Each can be given several times. Hooks of one kind run in the order given, and `BeforeLogin` and `OnRegister` stop at the first error.

```go
auth := authhttp.New(
	authhttp.BeforeLogin(refuseSuspended),
	authhttp.AfterLogin(recordLogin),
	authhttp.OnRegister(defaultShelf),
)
```

## When BeforeLogin runs

`BeforeLogin` sees only sign-ins that passed every check. The order for every method:

```text
rate limits ─► first factor ─► checks of the method ─► 2FA on? ──yes──► 202 challenge ─► second factor ─┐
                                                          │                                               │
                                                          no ◄────────────────────────────────────────────┘
                                                          ▼
                                  banned? ─► BeforeLogin ─► session created ─► commit ─► audit ─► AfterLogin ─► 200
                                             └──────────── one transaction ────────────┘
```

The checks of the method are v0.1's: a verified address for a password or a module's method, the linking rules for Google, Apple and GitHub ([authentication](authentication.md#google-apple-and-github-sign-in)).

| Sign-in | First factor | `LoginAttempt.Method` |
|---|---|---|
| `POST /v1/auth/login` | A correct password for a verified address | `password` |
| Passkey sign-in | The passkey's assertion | `passkey` |
| Google, Apple, GitHub | The provider's identity | `google`, `apple`, `github` |
| A module's method through [`Authenticator.SignIn`](adding-a-sign-in-method.md) | What the module verified | The module's method name, such as `phone_code` |

For an account with two-factor authentication, `BeforeLogin` runs after `POST /v1/auth/login/mfa`, with `SecondFactor` set to `totp`, `recovery_code` or `passkey`, and `Method` still naming how the sign-in started. A passkey sign-in is verified with two factors at once, gets no challenge, and runs the hooks with `SecondFactor` empty. A hook can't skip the second factor or a ban: both are checked before it runs.

`BeforeLogin` never runs for an unknown address, a wrong password, an unverified address, a failed second factor or a banned account. So a hook can't become an oracle that tells someone whether an address has an account, or a timing difference that does, and a refusal is only ever shown to someone who passed every check. It doesn't run for impersonation from the dev console, which exists only in development.

[`LoginAttempt`](../methods/gorbital-authhttp.md#LoginAttempt) carries `User` (ID, email, whether it is verified, has a password or is banned, created at), `Method`, `SecondFactor`, and the client's `IP` and `UserAgent` after `APP_TRUSTED_PROXIES`. `User.Roles` is empty: read what you need in `tx`. It never carries a password, token or code.

## Refusing a sign-in

```go
// Declared once, as a package variable: Refuse panics on a reserved code.
var errSuspended = authhttp.Refuse("reader_suspended", "this account is suspended; write to help@shelfie.example")

func refuseSuspended(ctx context.Context, tx pgx.Tx, a authhttp.LoginAttempt) error {
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
```

| The hook returns | The client receives | Recorded |
|---|---|---|
| `nil` | The session, as without the hook | `auth.login.succeeded` |
| An error from [`authhttp.Refuse(code, detail)`](../methods/gorbital-authhttp.md#Refuse) | 403 with `code` and `detail` as a problem; `#error=<code>` for web sign-ins with Google, Apple or GitHub. No session | `auth.login.failed` with `reason` `refused` and `code` |
| Any other error | 500 `internal_error`; `#error=server_error` for web sign-ins. The cause is logged, never sent | The error on the span and in the log |

Nothing the hook wrote in `tx` is kept when it refuses or fails: it commits only with the session.

### Refusal codes

A code is lowercase snake_case of 3 to 64 characters, and can't be one of sign-in's or gorbital's own codes: every v0.1.0 code (`invalid_credentials`, `account_banned`, `mfa_required`, …), the generic code of each status (`forbidden`, `internal_error`, …) and the codes added since, such as `registration_closed`. Clients can then always tell the app's refusals from sign-in's.

`Refuse` panics on such a code. Declare refusals as package variables, as above, so a wrong code stops the program when it starts. A hook that builds its detail when it refuses can return a [`*authhttp.Refusal`](../methods/gorbital-authhttp.md#Refusal) directly; its code is checked when it is returned, and a reserved one answers 500 instead of 403.

Document the app's codes for its clients, with the other [error codes](authentication.md#error-codes).

## AfterLogin

```go
recordLogin := func(ctx context.Context, e authhttp.LoginEvent) error {
	logger.InfoContext(ctx, "signed in", "user_id", e.User.ID, "method", e.Method, "second_factor", e.SecondFactor)
	return nil
}
```

[`LoginEvent`](../methods/gorbital-authhttp.md#LoginEvent) is the `LoginAttempt` with `SessionID`, and `User.Roles` loaded. It never carries the session's token.

- The session is already committed and audited: `AfterLogin` can't change the sign-in.
- An error is logged. A panic is recovered and logged.
- The response waits for the hooks at most 5 seconds. Then their context is cancelled, a warning is logged, and the response goes out; a hook that ignores its context keeps running on its own.
- For slow or unreliable work, such as calling another service, insert a job instead and let the job retry ([background jobs](background-jobs.md)).

## OnRegister

```go
defaultShelf := func(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount) error {
	_, err := tx.Exec(ctx, `INSERT INTO shelves (owner_id, name) VALUES ($1, 'Reading')`, a.User.ID)
	return err // rolls the account back
}
```

`OnRegister` runs in the transaction that inserts the account, for every way an account is created. Write the app's rows for the account in `tx`: a profile, a default workspace, a job with `jobs.Client.InsertTx`. [`NewAccount`](../methods/gorbital-authhttp.md#NewAccount) carries `User`, `Method`, `Name` and the client's `IP` and `UserAgent`.

| How the account is created | `Method` | `Name` |
|---|---|---|
| `POST /v1/auth/register` | `password` | Empty; [registration fields](extra-registration-fields.md) carry what the person typed |
| A first Google, Apple or GitHub sign-in | `google`, `apple`, `github` | The name the provider gave, when it gave one (Apple only the first time) |
| `POST /ops/auth/users` | `operator` | Empty; so are `IP` and `UserAgent` |

It runs only for new accounts. Registering an unverified address again, a second provider sign-in, linking a provider and passkeys added later don't run it. Service accounts aren't user accounts and don't run it either.

### What clients receive

An `OnRegister` error always rolls the account back. What the client sees depends on the path:

| Path | A refusal (`Refuse`) | Any other error |
|---|---|---|
| `POST /v1/auth/register` | 202, as for any registration; the error is logged | 202; the error is logged |
| First Google, Apple or GitHub sign-in | 403 with the code (`#error=<code>` for web sign-ins) | 500 (`#error=server_error`) |
| `POST /ops/auth/users` | 403 with the code | 500 |

Email registration answers 202 whether or not the address already has an account, and hooks run only for new accounts: any other answer would tell whoever registers that the address was free. So refuse bad input before anything is stored, with [registration fields](extra-registration-fields.md)' validation or a [`PasswordPolicy`](configuring-sign-in.md#passwords), which run for every request. A provider has already proved the identity, and an operator is trusted, so those paths can answer with the refusal.

## Transactions and time

`BeforeLogin` and `OnRegister` receive sign-in's transaction as a `pgx.Tx`:

- Read and write the app's tables in `tx`, not through `Deps.DB`: a separate connection doesn't see the new account and waits on its locks.
- The transaction holds a database connection and row locks while the hook runs. Keep the hook to a few queries: no calls to other services, no sleeping. Anything slow goes in a job inserted in `tx` or in `AfterLogin`.
- Don't commit or roll back `tx`. Return an error to roll back.

## Tracing

Each hook kind is a span under the request's trace:

| Span | Attributes |
|---|---|
| `authhttp.BeforeLogin` | `gorbital.auth.method`; `gorbital.auth.refused` with the code when a hook refused |
| `authhttp.AfterLogin` | `gorbital.auth.method` |
| `authhttp.OnRegister` | `gorbital.auth.method` (`password`, `google`, `apple`, `github`, `operator`); `gorbital.auth.refused` |

A hook's error that isn't a refusal is recorded on its span with an error status. Spans are exported with the rest of the request's trace when `OTEL_EXPORTER_OTLP_ENDPOINT` is set ([environment variables](environment-variables.md)).

## Security rules

- **Don't create an oracle.** A hook's answer must not depend on whether an address has an account. `BeforeLogin` runs only after every check, and `OnRegister`'s errors don't change registration's answer; keep it that way in what the hook logs or sends.
- **Fail closed.** Return the database error from `BeforeLogin`: sign-in then fails with 500 instead of letting someone through whom the hook couldn't check.
- **Never log secrets.** Hooks receive no password, token or code. Log the user ID, not the email address.
- **Keep BeforeLogin fast.** It holds a transaction open during every sign-in.
- **Refusal codes are part of the API.** Clients branch on them; don't rename them.

## Testing

Test hooks through the real sign-in routes, with `gorbitaltest.New(t, gorbital.WithAuth(auth), …)` ([testing with gorbitaltest](testing-with-gorbitaltest.md)): register or create an account, sign in, and assert the status and code (`res.AssertProblem(t, 403, "reader_suspended")`) and the rows the hook wrote. Test the refused path and a database error path as well as the success path.

## Related

- [Configuring sign-in](configuring-sign-in.md): every option.
- [Extra registration fields](extra-registration-fields.md).
- [Adding a sign-in method](adding-a-sign-in-method.md): hooks run for modules' methods too.
- [Shelfie, chapter 6](../examples/shelfie/06-accounts.md): profiles, a default shelf and suspended readers.

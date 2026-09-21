# 6. Accounts

Readers of Shelfie have a display name other readers see and a country for local recommendations, every new reader starts with a shelf to put books on, and moderators can suspend a reader who spams book clubs. None of this needs Shelfie to own sign-in: this chapter asks for the extra fields at registration, runs Shelfie's code when an account is created and when someone signs in, and raises the password policy, all as options of `authhttp.New` ([configuring sign-in](../../guides/configuring-sign-in.md)).

## 1. Sign-in's options

`main.go` builds the authenticator first, so chapters 7 and 8 can hand it to modules too:

<!-- include examples/shelfie/cmd/api/main.go#main -->

and `cmd/api/signin.go` lists how Shelfie's sign-in differs from the library's:

<!-- include examples/shelfie/cmd/api/signin.go#sign-in-options -->

| Option | Does | Guide |
|---|---|---|
| `RegisterFields(profiles.SaveRegistration)` | `POST /v1/auth/register` takes `display_name` and `country`, validated when the request arrives, and saves them in the transaction that creates the account | [Extra registration fields](../../guides/extra-registration-fields.md) |
| `OnRegister(createDefaultShelf)` | Runs in the transaction that creates any account: registration, a first Google, Apple or GitHub sign-in, or an operator's | [Sign-in hooks](../../guides/sign-in-hooks.md#onregister) |
| `BeforeLogin(profiles.RefuseSuspended)` | Runs once a sign-in's password (or passkey, provider, phone code) and second factor are verified, before the session exists, and can refuse it | [Sign-in hooks](../../guides/sign-in-hooks.md#when-beforelogin-runs) |
| `MinPasswordLength(14)` | New passwords are at least 14 characters instead of 12 | [Configuring sign-in](../../guides/configuring-sign-in.md#passwords) |

Everything else about sign-in stays as it was: the endpoints, codes and emails, and the providers configured in `.env`. Methods: [`authhttp`](../../methods/gorbital-authhttp.md).

## 2. Registration fields

A new module, `profiles`, holds readers' profiles, with the same layout as `books`: `module.go`, then `domain`, `usecase`, `repository` and `delivery` with one file per operation. Its table:

<!-- include examples/shelfie/db/migrations/20260920000003_profiles.sql -->

The fields registration takes are a struct with Huma's validation tags, next to the hook that saves them, in `internal/modules/profiles/hooks.go`:

<!-- include examples/shelfie/internal/modules/profiles/hooks.go#registration-fields -->

- **Validation happens before anything is stored, for every request.** A missing `display_name` or a country that isn't two capital letters answers 422 `validation_failed`, and `Resolve` applies the profile's own rules (a display name of spaces). The answer is the same whether or not the address already has an account, so it tells nobody who has one.
- **`SaveRegistration` runs in sign-in's transaction.** It builds the profiles use cases on a store bound to `tx`, so the account and its profile are committed together:

<!-- include examples/shelfie/internal/modules/profiles/usecase/save_registration.go#save-registration -->

- **An error in the hook rolls the account back, and registration still answers 202**, as it does for an address that already has an account: any other answer would reveal which addresses are free. That is why the rules live in the struct, where a mistake gets a 422, and the hook only stores.
- **The OpenAPI document shows the fields**: `go run ./cmd/api openapi --dir api` regenerates `api/`, and the web and mobile apps' generated clients send them.

### Readers who signed up with Google

A first Google, Apple or GitHub sign-in creates an account without registration fields, so those readers have no profile. `GET /v1/profile` says so with 404 `profile_incomplete`, and the apps ask for a display name before going on; `PUT /v1/profile` creates or changes it:

<!-- include examples/shelfie/internal/modules/profiles/usecase/update_profile.go#update-profile -->

## 3. A shelf for every new reader

Shelves belong to the `shelves` module, which [chapter 9](09-generators.md) generates with `orb gen module`: `GET /v1/shelves` and the rest of its routes, its rules and the `shelves` table. The hook that gives every new account a "Reading" shelf, however the account is created, is in `cmd/api/signin.go`:

<!-- include examples/shelfie/cmd/api/signin.go#default-shelf -->

- **A hook receives `tx` rather than using `Deps.DB`**: a separate connection wouldn't see the account, which isn't committed yet, and a shelf written outside the transaction would stay behind if the account were rolled back.
- **The shelf follows the shelves module's rules.** `shelves.NewShelf` from its `domain` package trims and checks the name and sets the version and the timestamps, so the default shelf is one `PATCH /v1/shelves/{id}` can rename like any other.
- **The row is written on `tx` by the hook.** The generated store runs on the pool, and its `InTx` begins a transaction of its own, so the hook inserts into the columns of the shelves migration itself. If you would rather keep every SQL statement in the module, give `shelves/repository` a constructor on a `pgx.Tx`: the generated code is yours to change.
- **The modules still don't import each other**: `profiles`, `shelves` and sign-in meet only in `cmd/api`.

## 4. Suspended readers

Moderators set `profiles.suspended_at`. The `BeforeLogin` hook refuses those readers' sign-ins with a code of Shelfie's:

<!-- include examples/shelfie/internal/modules/profiles/hooks.go#before-login -->

- **`authhttp.Refuse` checks the code when the program starts**: `reader_suspended` isn't one of sign-in's or gorbital's codes. A code such as `account_banned` would panic before `main` runs, so clients can always tell Shelfie's refusals from sign-in's.
- **The hook runs only after every check.** Unknown addresses and wrong passwords get 401 `invalid_credentials` as before, an operator's ban still answers `account_banned`, and a reader with an authenticator app types the code first. Only Eve, with her password and second factor, learns she is suspended.
- **A database error fails closed**: sign-in answers 500 instead of letting a suspended reader in.
- The refusal is recorded as `auth.login.failed` with `reason: refused` and the code.

## 5. Test it

The tests in `cmd/api/accounts_test.go` build sign-in with the same options as `main.go`:

<!-- include examples/shelfie/cmd/api/accounts_test.go#new-accounts-app -->

Registration checks the fields and the password policy, then the profile and the default shelf exist, and the shelf's name is taken like any other:

<!-- include examples/shelfie/cmd/api/accounts_test.go#register-with-profile -->

A reader without a profile completes it:

<!-- include examples/shelfie/cmd/api/accounts_test.go#complete-profile -->

and a suspended reader can't sign in, while a wrong password answers as usual:

<!-- include examples/shelfie/cmd/api/accounts_test.go#suspended -->

## Next

[7. Phone-code sign-in](07-phone-code-sign-in.md): a sign-in method of Shelfie's own, with the sessions, second factors and hooks of this chapter.

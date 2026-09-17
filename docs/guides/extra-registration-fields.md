# Extra registration fields

How an app asks for more than an email address and a password when people register, such as a display name or a country, without owning sign-in. The option is [`authhttp.RegisterFields`](../methods/gorbital-authhttp.md#RegisterFields); the decisions are in [ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md#phase-6-implementation-notes-sign-in-options-hooks-and-custom-methods-2026-09-17), Phase 6 of the [v0.2 roadmap](../v0.2-roadmap.md).

## The fields

The fields are a struct of the app's, with Huma's validation tags:

```go
// Profile is Shelfie's registration fields.
type Profile struct {
	DisplayName string `json:"display_name" minLength:"1" maxLength:"50"`
	Country     string `json:"country,omitempty" pattern:"^[A-Z]{2}$" doc:"ISO 3166-1 alpha-2 code"`
}

// Resolve refuses a display name that is only spaces.
func (p *Profile) Resolve(huma.Context) []error {
	if p.DisplayName != "" && strings.TrimSpace(p.DisplayName) == "" {
		return []error{&huma.ErrorDetail{Location: "body.display_name", Message: "must not be blank", Value: p.DisplayName}}
	}
	return nil
}
```

`save` stores them, in the transaction that creates the account:

```go
save := func(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount, p Profile) error {
	_, err := tx.Exec(ctx, `INSERT INTO profiles (user_id, display_name, country) VALUES ($1, $2, $3)`, a.User.ID, p.DisplayName, p.Country)
	return err
}
auth := authhttp.New(authhttp.RegisterFields(save))
```

`POST /v1/auth/register` now takes:

```json
{"email": "ada@example.com", "password": "correct horse battery", "display_name": "Ada", "country": "GB"}
```

## Validation

Huma validates the fields for every request, before anything is stored:

- **Tags**: a field is required unless its `json` tag has `omitempty`; `minLength`, `maxLength`, `pattern`, `enum`, `minimum`, `maximum`, `format` and the other tags Huma reads apply as on any route ([modules and routes](modules-and-routes.md#routes)).
- **`Resolve(huma.Context) []error`** on `*T`, for rules tags can't express. Return `*huma.ErrorDetail` values with the location `body.<field>`.

A request that fails either gets 422 `validation_failed` with each problem, the same for an address that already has an account as for a new one, so validation never reveals whether an address is registered. Properties the struct doesn't name are still accepted and ignored, as in v0.1, so older clients keep working.

Validate in the struct, not in `save`. `save` runs only for a new account, so its error can't change registration's answer: the account is rolled back, the error is logged, and the response is still 202 ([sign-in hooks](sign-in-hooks.md#what-clients-receive)). Someone who typed a bad value would never learn why no email arrived.

## Order in the transaction

For an email registration of a new address:

1. The account is inserted.
2. The [`OnRegister`](sign-in-hooks.md#onregister) hooks run, in order.
3. `save` runs with the fields.
4. The verification code is issued and the transaction commits; the email is sent.

An error in step 2 or 3 rolls everything back.

## OpenAPI

The register operation's request body schema gains the struct's properties, their descriptions (`doc`) and constraints, and its required fields beside `email` and `password`. Clients generated from `openapi.json` send them; `go run ./cmd/api openapi --dir api` writes the updated document. Without `RegisterFields`, the body is v0.1.0's.

## Rules

| Rule | Why |
|---|---|
| `T` is a struct | Its fields sit beside `email` and `password` in one JSON object |
| No field named `email` or `password` (in any case, promoted fields included) | Those are sign-in's |
| `RegisterFields` is given once | One struct holds every registration field |
| Not with `WithoutRegistration` | Without registration there are no registration fields |

Each mistake is reported by `CheckConfig`, and the app exits with status 2 before it connects ([configuring sign-in](configuring-sign-in.md#invalid-options-stop-the-start)).

## Accounts created without the fields

Only `POST /v1/auth/register` sends fields. Accounts created by a first Google, Apple or GitHub sign-in, or by an operator (`POST /ops/auth/users`), run the `OnRegister` hooks but not `save`. So an app can't assume every account has what registration asked for.

### Complete your profile

The pattern for those accounts, as Shelfie's profiles module builds it ([chapter 6](../examples/shelfie/06-accounts.md)):

1. **The profile row is optional.** `save` creates it for email registrations; other accounts start without one, or with what an `OnRegister` hook knows, such as `NewAccount.Name` from the provider.
2. **`GET /v1/profile` says when it's missing**: 404 with a code of the app's, `profile_incomplete`. (Returning the profile with `"complete": false` works as well; pick one and document it.)
3. **`PUT /v1/profile` creates or updates the row** (`INSERT … ON CONFLICT (user_id) DO UPDATE`), with the same rules as registration.
4. **Clients check after sign-in**, and after a first social sign-in ask for what is missing before going on.

```go
// OnRegister: prefill a profile from the provider's name. Email
// registrations get theirs from save.
prefill := func(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount) error {
	if a.Method == "password" || a.Name == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO profiles (user_id, display_name) VALUES ($1, $2)`, a.User.ID, a.Name)
	return err
}
auth := authhttp.New(authhttp.OnRegister(prefill), authhttp.RegisterFields(save))
```

`OnRegister` runs before `save` in the same transaction, so a hook that inserts the row for every method leaves `save` to update it (`INSERT … ON CONFLICT (user_id) DO UPDATE`).

## Fields are unverified input

The fields come from whoever registered the address first, before anyone proved they own it. Registering an unverified address again doesn't create a new account, so `OnRegister` and `save` don't run again: the first registrant's fields stay until the owner, once signed in, edits them.

- Treat registration fields as unverified user input, like any profile text: escape them when shown, and don't use them for decisions such as permissions, billing or which organisation an account joins.
- Let signed-in users edit them (`PUT /v1/profile` above).
- Ask only for what the app needs: the fields are stored before the address is verified. An account left unverified is deleted after `auth.unverified_account_ttl` (7 days by default) and removed for good later by `auth_cleanup`; the app's rows for it stay unless the app removes them, for example with a foreign key to `auth_users (id)` and `ON DELETE CASCADE` when sign-in's tables are always in the same database ([database](database.md)).

## Related

- [Sign-in hooks](sign-in-hooks.md): `OnRegister` and what clients receive.
- [Configuring sign-in](configuring-sign-in.md): every option.
- [Authentication](authentication.md): registration and verification.

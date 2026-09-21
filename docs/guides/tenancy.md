# Tenancy

Most business applications separate their customers' data from each other. gorbital calls that separation a **scope**, and a scope is a contract your app fills in rather than a model it inherits. Your tenant can be an organisation, a merchant, a clinic, a restaurant or a school; it can have the roles your business has; its IDs can be shaped however you already shape them. Decision: [ADR-0088](../adr/0088-scope-tenancy-as-a-contract.md).

The framework never learns your table names and never queries them.

## The five things it needs

```go
gorbital.WithScope(gorbital.Scope{
	Name:         "merchant",                 // for messages and logs
	PathParam:    "merchantId",               // /v1/merchants/{merchantId}/orders
	NotFoundCode: "merchant_not_found",       // the code a 404 carries
	ValidID:      merchants.ValidID,          // is this string shaped like one of your IDs?
	Roles:        merchants.Roles,            // owner, manager, courier — your words
	Session:      postgres.WithScope,         // carry it into the request's connections
}, merchants.NewAuthorizer(db))
```

Every field has a working default, and the defaults are the organisation vocabulary of v0.1 and v0.2, so an app that changes nothing behaves exactly as it did.

Your table name is not in that list, because the framework never needs it. It lives in one function you write.

## The authorizer is where your tables are

```go
// internal/modules/merchants/authorizer.go — your file, your tables, your rules
func (a *Authorizer) AuthorizeScope(ctx context.Context, merchantID, permission string) (context.Context, error) {
	who, ok := actor.From(ctx)
	if !ok {
		return ctx, actor.ErrUnauthenticated
	}

	var role string
	err := a.db.QueryRow(ctx, `
		SELECT role
		FROM   merchant_members
		WHERE  merchant_id = $1
		  AND  user_id     = $2
		  AND  deleted_at IS NULL`,
		merchantID, who.ID,
	).Scan(&role)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ctx, gorbital.ErrScopeNotFound
	case err != nil:
		return ctx, err
	}

	if !a.catalog.RoleHas(role, permission) {
		return ctx, actor.ErrForbidden
	}
	who.OrgID, who.Permissions = merchantID, a.catalog.Permissions(role)
	return actor.With(ctx, who), nil
}
```

[`guard.Scope`](../methods/gorbital-guard.md#Scope) calls this on every scope route and turns the answer into a response:

| Your authorizer returns | The caller sees |
|---|---|
| `nil`, and a context acting in the scope | the handler runs |
| `gorbital.ErrScopeNotFound` | 404 with your `NotFoundCode` |
| `actor.ErrUnauthenticated` | 401 `unauthenticated` |
| `actor.ErrStepUpRequired` | 403 `mfa_required` |
| `actor.ErrForbidden` | 403 `forbidden` |
| anything else | 500, and the error is logged |

If your authorizer returns `nil` with a context that is **not** acting in the requested scope, the guard fails the request with 500 rather than letting it through. Silent non-enforcement is not a possible outcome.

## Why unknown and malformed look the same

A scope that doesn't exist, one that was deleted, one the caller isn't a member of, and an ID that isn't shaped like an ID at all are all answered with the same status, the same code and the same body. Otherwise the differences between them let anybody enumerate your customers.

`ValidID` runs **before** your authorizer, so a malformed ID never reaches your SQL. It is a narrowing, not the defence — IDs are always passed as query parameters, never interpolated — but it keeps junk out of your queries and out of your logs.

## Picking the word at creation

```bash
orb new shop-api --auth basic --scope merchant
```

writes everything in your word:

```text
db/migrations/…_merchants.sql        merchants, merchant_members
internal/modules/merchants/          model.go, store.go, authorizer.go, policy.go
routes                               /v1/merchants/{merchantId}/…
permissions                          merchants.order.read
roles                                owner, manager, staff
```

and records it, so nothing asks you again:

```yaml
# gorbital.yaml
scope:
  name: merchant
  plural: merchants
  param: merchantId
  table: merchants
  members: merchant_members
  column: merchant_id
```

`orb gen resource Order --scope tenant` then reads that and generates `merchant_id` columns and `/v1/merchants/{merchantId}/orders` without being told twice.

The other choices are `--scope none` (no tenants), `--scope single` (data owned by users) and `--scope custom` (the tables and an authorizer stub, and you write the membership rules from the first commit).

## Using the organisations module under your own word

If you want members, roles and invitations built for you but not the word *organisation*:

```go
gorbital.WithModules(orgshttp.Module(auth, orgshttp.ScopeName("merchant", "merchants", "merchantId")))
```

That changes the route paths, the path parameter, the refusal code and the OpenAPI tag. It does **not** rename the database tables, the migrations, the permission names or the role names: those are your data and your declared API, and renaming them is a migration you should choose deliberately, not a side effect of a flag.

## One session setting, always

`Scope.Session` carries the scope into the request's database connections so row-level-security policies can read it. There is exactly one setting, `gorbital.org_id`, whatever your scope is called. It is invisible to people, policies in live databases name it, and renaming it would change what those policies mean. Set `Session: postgres.WithScope` and don't think about it again. See [Row-level security](row-level-security.md).

## Renaming later

The framework side is three strings:

```go
Name:         "vendor",
PathParam:    "vendorId",
NotFoundCode: "vendor_not_found",
```

Your side is your own migration, because they are your tables.

Changing the **URL** is a breaking change for anybody already calling your API. The usual course is to add `/v1/vendors/…`, keep `/v1/merchants/…` working, mark the old one deprecated in the OpenAPI document, and remove it in your next API version — not to rename it in place.

## What stays named after organisations

Two things keep their old names on purpose, whatever you call your scope:

- **`actor.Actor.OrgID`** holds the scope the actor is acting in. It is in the stable `actor` package and is already written into every audit row ever recorded; renaming the field would change what that stored data means.
- **`gorbital.org_id`**, the session setting above.

Both are documented as *the scope*. Everything a person or an API client sees — paths, codes, roles, tables, permissions — is yours.

## Related

- [Access control](access-control.md) — what a role means, and who holds one
- [Resource access](resource-access.md) — per-resource rules, including ones the generator can't guess
- [Row-level security](row-level-security.md) — the database layer underneath
- [Organisations](../start/organisations.md) — the supplied implementation, in full

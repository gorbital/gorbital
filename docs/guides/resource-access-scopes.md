# Resource access scopes

`orb gen module --scope` states who may read and write the records a module serves. The generator never assumes: a `WHERE org_id = $1` nobody asked for is a guess about your business, and a missing one is a data leak, so neither is a safe default and the choice is made when the module is generated ([ADR-0091](../adr/0091-resource-access-policies.md)).

```bash
orb gen module Shelf     name:string                      # --scope user, the default
orb gen module ClubBook  title:string:unique  --scope tenant
orb gen module Catalogue name:string          --scope public
orb gen module Order     reference:string     --scope custom
```

| Scope | Whose records | Reads | Writes | Ownership column |
|---|---|---|---|---|
| `user` | the signed-in user's | `guard.Permission` | `guard.Permission` | `owner_id` |
| `tenant` | the tenant's, reached through a role | `guard.Scope` | `guard.Scope` | the tenant column, plus `created_by` |
| `public` | everyone's to read | `guard.Public()` | `guard.Permission` | none |
| `custom` | whatever `policy.go` says | `Policy.CanRead` behind `guard.Permission` | `Policy.CanWrite` | none the generator chooses |

The default is `tenant` in an app with a tenancy and `user` otherwise — what the command has always generated. `org` is the old name of `tenant` and is accepted for all of v0.x, so existing scripts keep working; `--org` is the old name of `--scope tenant`. Each generated module records its scope in `gorbital.yaml`, which is how `orb routes` and `orb doctor` read the rule back:

```yaml
modules:
  catalogues: public
  clubbooks: tenant
  shelves: user
  tickets: custom
```

## `user`

The record has an `owner_id`, every statement filters on it, and somebody else's record is a 404 rather than a 403: it does not exist for them. The generated tests check that across list, create, get, update and delete, and that a read-only API key is refused on the writes.

## `tenant`

The records belong to a tenant and its members reach them through their role. gorbital does not decide what a tenant is called ([ADR-0088](../adr/0088-scope-tenancy-as-a-contract.md)): the module takes its word, path parameter, column, roles and refusal code from the `scope` block in `gorbital.yaml`.

```yaml
scope:
  name: merchant
  roles: [owner, manager, courier]
```

generates `/v1/merchants/{merchantId}/orders`, a `merchant_id` column, `merchant_not_found`, `guard.Scope` and `Permission.ScopeRoles` naming the merchant's roles. An app that names no tenant — every app up to v0.2.1 — gets the organisation vocabulary it already had: `/v1/orgs/{orgId}/…`, `org_id`, `org_not_found`, `guard.OrgMember` and `OrgRoles`, byte for byte what v0.2.1 generated.

The generated tests take the same shape either way. Under organisations they sign up real accounts through `orgshttp` and check `Test<Plural>AreProtected`: a non-member on list, create, get, update and delete, an unknown and a malformed organisation ID (all 404 `org_not_found`, so IDs cannot be probed), another organisation's record under your own path, and an API key's scopes. Under the app's own tenancy they check the same routes against a `gorbital.ScopeAuthorizer` the test brings, because membership there is the app's and not the framework's.

## `public`

Reads are `guard.Public()` and serve callers who are not signed in. Writes are never public: create, update and delete keep the write permission. The table has no ownership column, so there is no tenancy constraint to leave out by accident, and the delivery layer carries the reason in full:

```go
// Every catalogue is world-readable: these routes serve them to
// callers who are not signed in. Do not add a field here that one
// catalogue's owner would not publish.
```

The generated test posts unauthenticated and expects 401, reads unauthenticated and expects 200.

## `custom`

`custom` is for a rule the framework could not have guessed — "the courier assigned to this order, until it is delivered". The generator writes `policy.go` into the module and adds **no** filter and **no** tenant column of its own:

```go
// CanRead reports whether the actor in ctx may read this order.
func (p Policy) CanRead(ctx context.Context, order domain.Order) error {
	return gorbital.ErrNotImplemented // decide: which actors may read one order?
}
```

`CanWrite` and `Filter` are the same. `policy.go` is your file from the moment it lands: the generator never rewrites it and `orb upgrade` never touches it. The repository asks `Filter` before every list query — it is the only thing between a list request and every row in the table — and the use cases ask `CanRead` and `CanWrite` before returning or changing a record.

**A custom module does not pass its own tests, and that is deliberate.** `policy_test.go` fails while any of the three still returns `gorbital.ErrNotImplemented`, the module answers 501 `not_implemented` to every request until they do not, and `orb doctor` names the module meanwhile. A module that serves every row to everyone must not look finished.

Narrow a list with `q.And`, whose `?` placeholders take the arguments after it; only the condition text becomes SQL:

```go
func (p Policy) Filter(ctx context.Context, q *repository.Query) error {
	a, ok := actor.From(ctx)
	if !ok {
		return domain.ErrUnauthenticated
	}
	q.And("created_by = ? OR courier_id = ?", a.ID, a.ID)
	return nil
}
```

## Reading the rule back

`orb routes` gains a `SCOPE` column: the app's tenant, `user`, `public` or `custom`, from the module's record in `gorbital.yaml`, or from the route's own guards when there is none. A plain permission guard says who may call a route, not whose records it serves, so nothing is inferred from one.

`orb doctor` names every `tenant` module whose generated repository queries do not mention the tenant column, and every `custom` module whose policy is still unimplemented. **It is a static check over generated files.** It reads the repository files `orb gen module` wrote and looks for the column; it cannot see SQL you added later, a query built at run time, or a view that widens the rows. A clean run is a reminder, not a proof that a module is isolated. [Row-level security](row-level-security.md) is the defence that does not depend on a query being right.

## What a role means is code; who holds a role is data

This is the split people ask about most, so it is worth stating plainly.

| Question | Where the answer lives | How it changes | Who reviews it |
|---|---|---|---|
| What does `manager` mean? | Go: `Permission{Name, Description, ScopeRoles}` in a module, frozen when `gorbital.New` returns | A deploy | A pull request |
| Who is a `manager`? | Your data: a membership row | An API call | Your own audit trail |

The framework never reads your membership tables and never learns their names. It asks your authorizer, which answers from them.

## Granting one person one extra permission

Real applications sometimes grant one person one extra thing, and "deploy a new role" is not always an answer. gorbital supports this as an extension, not a built-in: it supplies no table, no endpoint and no migration for it. Your authorizer appends to what the role grants, from your own table:

```go
func (a *Authorizer) AuthorizeScope(ctx context.Context, scopeID, permission string) (context.Context, error) {
	ctx, err := a.roles.AuthorizeScope(ctx, scopeID, permission)
	if err == nil {
		return ctx, nil
	}
	extra, lookupErr := a.grants.For(ctx, actorID(ctx), scopeID) // your table
	if lookupErr != nil || !slices.Contains(extra, permission) {
		return ctx, err
	}
	if slices.ContainsFunc(extra, func(p string) bool { return strings.HasPrefix(p, "ops.") }) {
		return ctx, fmt.Errorf("per-user grant of an operator permission: %v", extra)
	}
	// Put the actor back with the extra permissions it now holds.
	acting, _ := actor.From(ctx)
	acting.OrgID, acting.Permissions = scopeID, append(slices.Clone(acting.Permissions), extra...)
	return actor.With(ctx, acting), nil
}
```

Two warnings, and they are the whole feature:

- **A new role is usually the better answer.** A role's grants are declared in Go and reviewed in a pull request. A per-user row is an `INSERT` that nobody reviews, and it is invisible in the code that decides what the permission does.
- **Never grant an operator permission this way.** Refuse any permission whose name starts with `ops.`: operator permissions belong to platform roles, not to a scope's members, and the refusal is a hard error rather than a silent drop.

## Related

- [Generating code](generating-code.md) — what `orb gen module` writes, file by file
- [Guards and middleware](guards-and-middleware.md) — `guard.Public`, `guard.Permission`, `guard.Scope`
- [Row-level security](row-level-security.md) — the defence that does not depend on the query
- [ADR-0091](../adr/0091-resource-access-policies.md) — why there is no default scope

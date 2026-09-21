# Access control

Two questions look alike and have different answers, in different places:

| Question | Where the answer lives | Who changes it, and how |
|---|---|---|
| **What does the role `manager` mean?** | Your **code**, in a module's `Permissions` | A developer, in a commit, reviewed and deployed |
| **Is Ahmed a manager at Acme?** | Your **database**, in your own table | Anyone with permission, through your API, while the app runs |

That split is deliberate. If a role's permissions lived in a table, one `UPDATE` could hand somebody `orders.order.refund` with no review and no record of what changed. Keeping the grants in Go means every privilege change goes through the same scrutiny as any other code. Keeping the memberships in your database means adding a manager doesn't need a deploy.

The framework owns neither answer. It collects what your modules declare, checks it on the routes you ask it to, and refuses with 403. It never decides which permissions exist, what a role is called, or who holds one.

## Declaring permissions

A module declares the permissions it checks, and which roles hold them. Names are yours, and they are public API once released ([ADR-0015](../adr/0015-public-api-and-stability-tiers.md)):

```go
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "orders",
		Permissions: []gorbital.Permission{
			{Name: "orders.order.read", Description: "See orders", ScopeRoles: []string{"owner", "manager", "staff"}},
			{Name: "orders.order.write", Description: "Create and change orders", ScopeRoles: []string{"owner", "manager"}},
			{Name: "orders.order.refund", Description: "Refund an order", ScopeRoles: []string{"owner"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) { /* … */ },
	}
}
```

`ScopeRoles` are roles held **inside a scope** — an organisation, a merchant, a clinic, whatever your app calls its tenant ([Tenancy](tenancy.md)). `Roles` are platform roles, held across the whole app:

```go
{Name: "reports.export.run", Description: "Export the ledger", Roles: []string{"platform_admin"}},
```

A permission sets one or the other, never both: it is held on the platform or in a scope. `gorbital.New` fails naming the module if it sets both.

Nothing else declares permissions. There is no registry to remember, no string to keep in step in a second file, and a typo fails at start-up rather than at run time — `gorbital.New` refuses a guard that names a permission no module declared.

## Checking them

A route says what it needs, next to the route:

```go
orders := r.Group("/v1/merchants/{merchantId}/orders", gorbital.Tags("Orders"))
gorbital.Get(orders, "/{id}", h.getOrder, guard.Scope("orders.order.read"))
gorbital.Post(orders, "/{id}/refund", h.refund, guard.Scope("orders.order.refund"))
```

- [`guard.Scope`](../methods/gorbital-guard.md#Scope) checks membership of the scope in the path **and** the permission.
- [`guard.Permission`](../methods/gorbital-guard.md#Permission) checks a platform permission, with no scope.
- [`guard.Public`](../methods/gorbital-guard.md#Public) lets a route be reached without an actor at all.

Routes are deny by default ([ADR-0082](../adr/0082-routes-guards-and-middleware.md)): a route with no `guard.Public()` needs an authenticated actor, so a forgotten guard is a 401, not a hole.

In a use case, check again where the rule is about the record rather than the route:

```go
if err := actor.Require(ctx, "orders.order.refund"); err != nil {
	return err
}
```

The guard protects the door. The use case protects the decision. A route guard alone is not enough when the answer depends on the row — see [Resource access](resource-access.md).

## Giving somebody a role

Three ways, none of them a deploy.

<div class="steps">

1. **When you invite them**

   ```http
   POST /v1/merchants/mch_1/invitations
   {"email": "ahmed@example.com", "role": "manager"}
   ```

   They accept, a membership row is written, and they are a manager. The endpoint is in your app's own code, so the rules around it are yours: whether managers may invite, whether there can be two owners, whether an invitation expires in a day or a week.

2. **By changing a member's role**

   ```http
   PATCH /v1/merchants/mch_1/members/usr_ahmed
   {"role": "owner"}
   ```

   Underneath it is one statement against your table:

   ```sql
   UPDATE merchant_members SET role = $3 WHERE merchant_id = $1 AND user_id = $2;
   ```

3. **From the terminal, for the first administrator**

   ```bash
   go run ./cmd/api grant-role ahmed@example.com platform_admin
   ```

   This is for **platform** roles, not scope roles, and exists because somebody has to be an administrator before anybody can sign in and appoint one.

</div>

## Per-user permissions

There is no built-in way to give one person one extra permission, and that is on purpose: per-user grants are hard to audit and harder to remember six months later. It is nonetheless your app, and the permission list is built by your code, so you can:

```go
func (a *Authorizer) AuthorizeScope(ctx context.Context, scopeID, permission string) (context.Context, error) {
	role, err := a.store.MemberRole(ctx, scopeID, actorID(ctx))
	if err != nil {
		return ctx, gorbital.ErrScopeNotFound
	}

	perms := a.catalog.Permissions(role)                    // what the role grants
	extra, err := a.store.ExtraPermissions(ctx, scopeID, actorID(ctx))
	if err != nil {
		return ctx, err
	}
	perms = append(perms, extra...)                         // what this person also has
	// …
}
```

Two rules if you do:

- **Never grant `ops.*` this way.** Those are the operations API's own permissions. Refuse them in the appender rather than trusting the table.
- **Record who granted it and why.** An `audit.Record` at the point of granting is the difference between an exception and a mystery.

If you find yourself adding the same extra to several people, that is a role asking to exist. A role is easier to explain, easier to audit and easier to take away.

## What the framework does with all this

Four things, all mechanism:

1. **Collects** every module's declarations into one catalog at start-up, so a permission named by a guard and declared by nobody fails `gorbital.New` rather than 403-ing in production.
2. **Checks** `actor.Can(permission)` at the guard and answers 403 `forbidden`, or 403 `mfa_required` when the role grants it only to a session verified with a second factor.
3. **Carries** the granted permissions through the request in [`actor.Actor`](../methods/actor.md), so use cases, audit events and jobs see the same answer the guard saw.
4. **Lists** them at `/ops/auth/permissions`, so an operator can see who can do what without reading the source.

It never decides which permissions exist, who holds them, or what your roles are called.

## Related

- [Tenancy](tenancy.md) — the scope a role is held in, and naming it after your own business
- [Resource access](resource-access.md) — choosing the rule for a resource, and writing your own
- [Guards and middleware](guards-and-middleware.md) — every guard, and the order they run in
- [Row-level security](row-level-security.md) — the database layer underneath all of this

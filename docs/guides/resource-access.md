# Resource access

When you generate a resource, `orb` asks one question it will not answer for you: who may see and change these records?

```bash
orb gen resource Order status:enum(placed,paid,delivered) total:int --scope tenant
```

There are four answers. Picking one is required, and the choice is visible in the generated code rather than hidden in the generator. Decision: [ADR-0091](../adr/0091-resource-access-policies.md).

| `--scope` | Who owns a record | What the generator writes |
|---|---|---|
| `user` | the person who created it | an `owner_id` column, `guard.Permission`, and a filter by actor |
| `tenant` | the scope in the path | a scope column, `guard.Scope`, a filter by scope, and isolation tests |
| `public` | nobody; everybody may read | no owner column, `guard.Public()` reads, guarded writes |
| `custom` | you decide | a `policy.go` you own, no filter and no column |

`--scope org` still works and means `tenant`.

## Why there is no default

A generator that quietly adds `WHERE org_id = $1` to your queries is guessing about your business. A generator that quietly leaves it out is a data leak. Neither is a defensible default, so the rule is:

> The generator states the access rule. It never assumes one.

That is why `custom` writes a file that does not compile into a working policy until you have made a decision, rather than a file that silently permits everything.

## `tenant`

The common case in a multi-tenant app. The generated code uses your app's own vocabulary, read from `gorbital.yaml` ([Tenancy](tenancy.md)):

```go
orders := r.Group("/v1/merchants/{merchantId}/orders", gorbital.Tags("Orders"))
gorbital.Get(orders, "/{id}", h.getOrder, guard.Scope("orders.order.read"))
```

```sql
SELECT id, status, total
FROM   orders
WHERE  id = $1
  AND  merchant_id = $2;
```

You also get isolation tests, in your words, asserting that a caller in one merchant cannot read, update, delete or list another merchant's orders, and cannot reach them through a list filter or sort. They run against Docker PostgreSQL with the rest of your suite.

## `public`

Reads are open; writes are not. The delivery file says so plainly, because the next person to add a field to this resource needs the warning more than you did:

```go
// Every order is world-readable: these routes serve them to callers who
// are not signed in. Do not add a field here that one order's owner would
// not publish.
```

## `custom`

For rules the framework could not have guessed: a record visible to its author and to whoever it was assigned to; a record readable while it is in one state and not another; a record shared between two tenants.

The generator writes `policy.go` into the module. It is yours from the moment it lands — the generator never rewrites it and `orb upgrade` never touches it:

```go
// Policy decides who may see and change an Order. gorbital does not know,
// so nothing here is implemented: fill these in and delete the errors.
type Policy struct{ /* the app's dependencies */ }

func (p Policy) CanRead(ctx context.Context, order domain.Order) error {
	return ErrNotImplemented // decide: which actors may read one order?
}

func (p Policy) CanWrite(ctx context.Context, order domain.Order) error {
	return ErrNotImplemented // decide: who may change one, and in which states?
}

// Filter narrows a list query to the rows the actor in ctx may see. It is
// the only thing standing between a list request and every row in the table.
func (p Policy) Filter(ctx context.Context) (repository.Filter, error) {
	return repository.Filter{}, ErrNotImplemented
}
```

The repository and the use cases call all three. A generated test fails until you have implemented them, and `orb doctor` lists the module as unfinished in the meantime. Neither is an obstacle to work around: a `custom` resource with an unimplemented `Filter` is a list endpoint that would return every row in the table.

## What `orb` can and cannot check for you

`orb routes` shows the rule for every route:

```text
GET   /v1/merchants/{merchantId}/orders/{id}   scope: merchant   orders.order.read
GET   /v1/catalog/{id}                         public
GET   /v1/reports/{id}                         custom
```

`orb doctor` reports a `tenant` module whose repository queries have lost their scope column. Be clear about its limit: it reads the **generated** repository files. SQL you wrote yourself afterwards, in a new file or a new method, is not something a static check can follow — that is what the isolation tests are for, and why they are generated with the resource rather than left as an exercise.

## Changing your mind

The scope is a property of the module, not of the generator run, so changing it is an edit to your own code plus a migration. There is no `orb gen resource --rescope`: dropping a tenant column, or adding one to a table that already has rows, is a data decision with a backfill and a deployment order, and a generator that pretended otherwise would be the most dangerous thing in the toolchain.

## Related

- [Tenancy](tenancy.md) — the scope itself, and naming it after your own business
- [Access control](access-control.md) — permissions, roles, and who holds one
- [Generating code](generating-code.md) — everything `orb gen` writes, and how to change it afterwards
- [Row-level security](row-level-security.md) — the database layer under `tenant`

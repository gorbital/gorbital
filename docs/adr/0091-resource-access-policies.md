# ADR-0091: Resource access policies

**Status:** Proposed (2026-09-21) · **Amends:** ADR-0039, ADR-0048 · **Builds on:** ADR-0021, ADR-0088 · **Related:** ADR-0090, ADR-0092

## Context

`orb gen resource` writes a whole module — domain rules, use cases, a repository with hand-written SQL, endpoints, tests and a migration — and in doing so it decides who may read and write those records. Today it makes that decision from one flag with two values, and when the flag is absent it guesses from the app's tenancy. The guess is right for the two shapes gorbital has shipped and wrong for every other one, and the developer cannot see that a guess was made: the filter is simply in the SQL.

ADR-0088 removed the assumption that a tenant is called an organisation. This record removes the assumption that a record belongs to one.

Facts checked on 2026-09-21 against this branch:

| Area | Today | Evidence |
|---|---|---|
| The flag | `--scope` accepts `user` or `org`; anything else is a usage error | `cli/internal/cli/gen_resource.go:113`, `cli/internal/cli/gen_plan.go:123`, `cli/internal/recipes/resource.go:382` |
| The values | `ScopeUser = "user"`, `ScopeOrg = "org"` | `cli/internal/recipes/resource.go:325` |
| The default | `org` when `gorbital.yaml` records `tenancy: multi`, `user` otherwise | `cli/internal/cli/gen_plan.go:137`, `gen_resource.go:131` for the `gorbital.Main` layout |
| The path | `/v1/orgs/{orgId}/<route>` for an organisation resource, `/v1/<route>` otherwise | `resourceRoute`, `gen_resource.go:54` |
| The guard | `guard.Public()`, `guard.Permission(name)`, `guard.OrgMember(permission)` | `gorbital/guard/guard.go:41`, `:59`, `:131` |
| The column | `org_id` plus `created_by`, or `owner_id`; the indexes and the RLS policy are built on it | `cli/internal/recipes/module/migration.sql.tmpl`, `repository_store.go.tmpl:44` |
| What is printed | "Every organisation role gets `<pkg>.<name>.read` and `.write`", or the same sentence for the user role | `gen_resource.go:186–190` |
| Permissions | `Permission{Name, Description, Roles, OrgRoles}`; a permission with `OrgRoles` is an organisation permission and may not also have `Roles` | `gorbital/module.go:98` |
| A module's files | `domain/`, `usecase/`, `repository/`, `delivery/`, one file per operation in the `gorbital.Main` layout | `cli/internal/recipes/module/`, `moduleTemplates`, `module.go:218` |
| Isolation today | `Test<Plural>AreProtected`: a non-member gets 404 on list, create, get, update and delete, and another organisation's record is absent from the list | `cli/internal/recipes/module/module_org_test.go.tmpl:147` |

Two gaps follow. An app whose records are world-readable, or whose access rule is "the courier assigned to this order, until it is delivered", has no value to pass, so it takes `user` or `org` and edits the SQL afterwards — and the isolation tests the generator wrote still assert the rule it no longer follows. And the vocabulary in the generated code, the route and the printed messages is the word *organisation*, which ADR-0088 has just stopped being the framework's business.

## Options

### What the generator assumes about access

| Option | Verdict |
|---|---|
| Always filter by tenant | Rejected: wrong for a public resource and wrong for any custom rule, and a `WHERE org_id = $1` the developer did not ask for is a guess about their business |
| Never filter; the developer adds the rule | Rejected: the common case becomes a footgun, and the failure is silent — a missing filter reads exactly like a correct query |
| **The developer chooses at generation time, and the choice is visible in the generated code** | **Chosen**: the generator states the access rule instead of assuming one |

### How a custom rule is expressed

| Option | Verdict |
|---|---|
| A configuration file the framework interprets | Rejected: a policy language is a worse Go, with no type checking, no debugger and no tests |
| An interface the app implements somewhere else | Rejected: nothing in the generated module points the developer at it, so the module compiles and serves while the rule is missing |
| **A generated `policy.go` inside the module, with failing stubs** | **Chosen**: it is in the file the developer opens next, it does not compile past a lie, and its test is red until the rule exists |

### Where per-user grants live

| Option | Verdict |
|---|---|
| A framework table of user-to-permission rows | Rejected: a privilege change with no code review, in a table gorbital would then own — against ADR-0092 |
| Unsupported; a new role every time | Rejected: real applications grant one person one extra thing, and telling them to deploy a role is not an answer |
| **The app's `ScopeAuthorizer` appends extra permissions from the app's own table, documented with its warning** | **Chosen**: the mechanism already exists, the table is the app's, and the warning is honest |

## Decision

### 1. The rule the generator follows

The generator must state the access rule and never assume one. A silent `WHERE org_id = $1` inserted into code nobody asked for is a guess about somebody's business; a missing one is a data leak. Neither default is safe, so there is no default: the choice is made at generation time and is legible in the generated file afterwards.

### 2. Four scopes

`orb gen resource --scope` accepts `user`, `tenant`, `public` and `custom`. `org` stays as a deprecated alias for `tenant` for all of v0.x, so every script and every documented command keeps working.

| Scope | Ownership column | Reads | Writes |
|---|---|---|---|
| `user` | `owner_id` | `guard.Permission` | `guard.Permission` |
| `tenant` | the scope column, plus `created_by` | `guard.Scope` | `guard.Scope` |
| `public` | none | `guard.Public()` | `guard.Permission` |
| `custom` | none the generator chooses | `Policy.CanRead` behind `guard.Permission` | `Policy.CanWrite` |

The default stays what it is today — `tenant` in an app with a scope, `user` otherwise — because changing it would silently rewrite what an existing command generates. Every generated module records its scope in a header comment and in `gorbital.yaml`, so `orb routes` and `orb doctor` can read it back.

### 3. `tenant` speaks the app's words

A `tenant` resource takes its name, path parameter, column and refusal code from the app's `Scope` (ADR-0088) instead of the hardcoded organisation words: `/v1/merchants/{merchantId}/orders`, `merchant_id`, `merchant_not_found`, and `guard.Scope` rather than `guard.OrgMember`. Roles come from `Scope.Roles`, so the sentence printed after generation names the app's roles. An app on the supplied organisations scope generates byte-identical output to v0.2.1; that equality is a release gate.

Those two sentences pull against each other, because v0.2.1 wrote `guard.OrgMember` and `OrgRoles`. As implemented, the vocabulary decides: an app whose `gorbital.yaml` names a scope gets `guard.Scope`, `Permission.ScopeRoles` and its own words, and an app that names none — every app up to v0.2.1 — gets the organisation words and the two deprecated names, byte for byte what it got before. The release gate wins for apps that never asked for anything else, and `--scope org` and `--scope tenant` are one value with two spellings, so the alias cannot drift from the scope.

### 4. `public`

Reads are `guard.Public()`. Writes are `guard.Permission`, never public. There is no owner column, and the migration has no tenancy constraint to omit by accident. The delivery file carries the comment in full, because the next reader of this module needs it more than the person who typed the flag:

```go
// Every ⟦.PluralHuman⟧ is world-readable: these routes serve them to
// callers who are not signed in. Do not add a field here that one
// ⟦.Human⟧'s owner would not publish.
```

### 5. `custom`: the module owns its policy

The generator writes `policy.go` into the module. It is the app's file from the moment it lands — the generator never rewrites it, and `orb upgrade` never touches it:

```go
// Policy decides who may see and change a ⟦.Human⟧. gorbital does not know,
// so nothing here is implemented: fill these in and delete the errors.
type Policy struct{ /* the app's dependencies */ }

// CanRead reports whether the actor in ctx may read this ⟦.Human⟧.
func (p Policy) CanRead(ctx context.Context, ⟦.Var⟧ domain.⟦.Ident⟧) error {
	return ErrNotImplemented // decide: which actors may read one ⟦.Human⟧?
}

// CanWrite reports whether the actor in ctx may create, update or delete it.
func (p Policy) CanWrite(ctx context.Context, ⟦.Var⟧ domain.⟦.Ident⟧) error {
	return ErrNotImplemented // decide: who may change one, and in which states?
}

// Filter narrows a list query to the rows the actor in ctx may see. It is
// the only thing standing between a list request and every row in the table.
func (p Policy) Filter(ctx context.Context, q *repository.Query) error {
	return ErrNotImplemented // decide: which rows appear in a list?
}
```

The repository calls `Filter` before every list and search query; the use cases call `CanRead` and `CanWrite` before returning or changing a record. The generator adds **no** implicit tenant filter and **no** tenant column to the migration: with `custom`, isolation is the module's own, and pretending otherwise would hide a rule the developer never wrote. A test shipped beside it fails until all three methods return something other than `ErrNotImplemented`, so a `custom` module cannot reach a green build unimplemented.

### 6. What a role means is code; who holds a role is data

This is the split users ask about most, so it is stated once here.

| Question | Where the answer lives | How it changes | Who reviews it |
|---|---|---|---|
| What does `manager` mean? | Go: `Permission{Name, Description, ScopeRoles}` in a module, frozen when `gorbital.New` returns | A deploy | A pull request |
| Who is a `manager`? | The app's data: a membership row | An API call | The app's own audit trail |

The framework never reads the app's membership tables and never learns their names. It asks the authorizer, which answers from them.

### 7. Per-user grants are an extension, not a built-in

An app that must grant one person one extra permission appends to what the role grants, in its own authorizer, from its own table:

```go
func (a *Authorizer) AuthorizeScope(ctx context.Context, scopeID, permission string) (context.Context, error) {
	ctx, err := a.roles.AuthorizeScope(ctx, scopeID, permission)
	if err == nil {
		return ctx, nil
	}
	extra, lookupErr := a.grants.For(ctx, actorID(ctx), scopeID) // the app's table
	if lookupErr != nil || !slices.Contains(extra, permission) {
		return ctx, err
	}
	return actor.WithExtraPermissions(ctx, extra), nil
}
```

gorbital supplies no table, no endpoint and no migration for this. The documented warning is that a new role is usually the better answer: a role's grants are declared in Go and reviewed in a pull request, whereas a per-user row is an `INSERT` that nobody reviews. An appended permission whose name starts with `ops.` is refused — operator permissions are reserved to platform roles and are not grantable per user.

### 8. Generated isolation tests

For `user` and `tenant` the generator writes the tests that exist today for organisations, in the app's own vocabulary: one owner or tenant cannot read, update, delete or list another's records, and cannot reach them through a list filter or sort. For `custom` it writes one test asserting that the policy is implemented, and nothing else — the framework cannot know what isolation means there.

### 9. `orb routes` and `orb doctor`

`orb routes` gains a SCOPE column: the scope the route requires, or `public`, or `custom`. `orb doctor` lists every module whose declared scope is `tenant` and whose **generated** repository queries do not mention the scope column, and every `custom` module whose policy is still unimplemented. The check is static and reads generated repository files only. Its output says so in those words: it cannot see SQL the developer added later, a query built at runtime, or a view that widens the rows. It is a reminder, not a proof, and a clean `orb doctor` is not evidence that a module is isolated.

## Threat model

| Threat | Mitigation |
|---|---|
| A `custom` module never implements its policy and serves every row to everyone | The three stubs return `ErrNotImplemented` and the shipped test fails until they do not; `orb doctor` names the module while it is unimplemented; `orb routes` shows the route as `custom`, not as guarded |
| A `tenant` module's hand-written query drops the scope column | The generated isolation tests exercise read, update, delete and list across two tenants and fail. The static check in `orb doctor` does not help here — it reads generated files only, and the developer's query is not one. Row-level security (ADR-0061) is the defence that does not depend on the query being right |
| A `public` resource's write path is left unguarded | `public` guards only reads. A write route generated under `public` always carries `guard.Permission`, and registration fails when a non-`GET` route in the module is public; the generated test posts unauthenticated and expects 401 |
| A per-user grant escalates to operator permissions | The appender refuses any permission whose name starts with `ops.`; operator permissions belong to platform roles, not to scope members, and the refusal is a hard error rather than a silent drop |

## Consequences

- The generator stops having an opinion about the business, and starts having one about visibility. That is the trade this record makes.
- Four scopes instead of two: four sets of templates, four isolation-test shapes and nine documented combinations with the sign-in profiles (ADR-0090).
- `custom` generates a module that does not pass its own tests. This is deliberate and needs saying in the CLI output, the guide and the release notes, or it reads as a bug.
- `org` survives as an alias for all of v0.x, so two words for one scope appear in help text, `gorbital.yaml` files and existing scripts.
- `orb doctor` gains a check whose honest description is longer than the check. Accepted: a security check that oversells itself is worse than none.
- Per-user grants are documented without being supported in code, so the quality of that documentation is the whole feature. ADR-0039's single `--scope` axis is amended, and ADR-0048's organisation-shaped resource becomes one scope among four.

# ADR-0088: Scope — tenancy as a contract

**Status:** Proposed (2026-09-21) · **Amends:** ADR-0023, ADR-0048, ADR-0061 · **Builds on:** ADR-0081, ADR-0082, ADR-0083 · **Related:** ADR-0092

## Context

gorbital has had multi-tenancy since v0.1 and, since v0.2, a seam for it: the app tells the framework who may act in a tenant, and the framework enforces it on every route that asks. The seam is the right shape. The vocabulary around it is not: nine things are fixed to the word *organisation*, and one of them — the ID format — is not a matter of taste but a wall.

Facts checked on 2026-09-21 against `main` (`93040dff`, v0.2.1):

| Area | Today | Evidence |
|---|---|---|
| The seam | `OrgAuthorizer.AuthorizeOrg(ctx, orgID, permission) (context.Context, error)`, set once by a module with `Platform.SetOrgAuthorizer` | `gorbital/orgs.go` |
| The guard | `guard.OrgMember(permission)`; `New` fails when a route uses it and no module set an authorizer | `gorbital/guard/guard.go`, `checkOrgRoutes` |
| Path parameter | `route.OrgIDParam = "orgId"`; registration fails for a path without `{orgId}` | `gorbital/internal/route/route.go` |
| ID format | `orgs.ParseID(orgID)`, called by the composition **before** the authorizer is asked | `gorbital/orgs.go`, `authorizeOrg` |
| Refusals | 404 `org_not_found` (unknown, deleted, forbidden and malformed alike), 403 `forbidden`, 403 `mfa_required` | `gorbital/orgs.go` |
| Roles | `owner`, `admin`, `member`, in that order, with fixed descriptions | `orgRoleOrder`, `orgRoleDescriptions` |
| Permissions | `Permission.OrgRoles` | `gorbital/module.go` |
| Actor | `actor.Actor.OrgID`, written into every audit event | `actor/actor.go` |
| Database session | `postgres.WithOrg(ctx, id)` setting `gorbital.org_id`, read by row-level-security policies | `modules/postgres`, ADR-0061 |
| Coupling | `gorbital` imports `modules/orgs` for `ParseID`, `ErrOrgNotFound` and the role names | `gorbital/orgs.go` imports |

Two consequences follow. An app can already supply any membership logic, but cannot supply any vocabulary: a merchant, clinic or restaurant gets `/v1/orgs/{orgId}/…`, the problem code `org_not_found`, and roles it never chose. And an app whose tenant IDs are UUIDs, or `merchant_01H…`, cannot use `guard.OrgMember` at all, because `orgs.ParseID` rejects the ID before the app's own authorizer is consulted.

The second is the real defect. The first is a promise: the framework must not decide what the app's business calls things (ADR-0092).

## Options

### What the app supplies

| Option | Verdict |
|---|---|
| Leave it; document that tenants are called organisations | Rejected: the ID format blocks real applications, not only naming preferences, and it contradicts ADR-0092 |
| A `Tenant` interface with a method per fact (`Name()`, `PathParam()`, `ValidID()`, `Roles()`, …) | Rejected: six methods to implement in order to rename one word, and no useful zero value |
| **A `Scope` struct of five fields, plus a separate one-method authorizer** | **Chosen**: the data is data, the behaviour is behaviour, and every field has a working default |

### Renaming

| Option | Verdict |
|---|---|
| Rename `Org*` to `Scope*` outright | Rejected: breaks every v0.2 app, and every copy of `orgshttp` already sitting in an app's repository |
| Keep only `Org*` and give it parameters | Rejected: `guard.OrgMember` on `/v1/merchants/{merchantId}` reads as a bug in the app |
| **Add `Scope*` as the general form; `Org*` delegates to it and is deprecated for all of v0.x** | **Chosen** |

### Where the ID format is checked

| Option | Verdict |
|---|---|
| Keep `orgs.ParseID` in the composition | Rejected: the composition keeps importing `modules/orgs`, and no other format is possible |
| No validation; pass every string to the authorizer | Rejected: a malformed ID reaches the app's SQL, and whether IDs can be probed then depends on the app getting its own error handling right |
| **`Scope.ValidID`, checked before the authorizer, refusing exactly as an unknown scope does** | **Chosen**: the safe behaviour is the default, and the app may widen it |

### `actor.Actor.OrgID` and `postgres.OrgSetting`

| Option | Verdict |
|---|---|
| Rename both to `ScopeID` and `gorbital.scope_id` | Rejected: `OrgID` is in the stable `actor` package and is already written into every audit row; `OrgSetting` is named in row-level-security policies inside users' live databases. Renaming either changes the meaning of stored data |
| **Keep both names and values; document them as *the scope*. New apps use `gorbital.scope_id` through `postgres.WithScope`; existing apps keep `gorbital.org_id`** | **Chosen** |

## Decision

### 1. The contract

```go
// A Scope is the app's tenancy: what a tenant is called, how its IDs look,
// which roles it has, and how a request's tenant reaches the database.
type Scope struct {
	Name         string                                        // "organisation", "merchant", "restaurant"
	PathParam    string                                        // "orgId", "merchantId"
	NotFoundCode string                                        // "org_not_found", "merchant_not_found"
	ValidID      func(string) bool                             // nil accepts any non-empty string
	Roles        []ScopeRole                                   // most privileged first
	Session      func(context.Context, string) context.Context // nil leaves connections unscoped
}

type ScopeRole struct {
	Name        string // "owner", "manager", "courier"
	Description string
}
```

The framework never learns the app's table names, and never queries them. Membership lives entirely behind the authorizer.

### 2. The authorizer

```go
type ScopeAuthorizer interface {
	AuthorizeScope(ctx context.Context, scopeID, permission string) (context.Context, error)
}
```

Its error contract is `AuthorizeOrg`'s, unchanged:

| Returns | Answer |
|---|---|
| `nil`, and a context whose actor has `OrgID == scopeID` | the request proceeds |
| `gorbital.ErrScopeNotFound` (or `orgs.ErrOrgNotFound`) | 404 `Scope.NotFoundCode` |
| `actor.ErrUnauthenticated` | 401 `unauthenticated` |
| `actor.ErrStepUpRequired` | 403 `mfa_required` |
| `actor.ErrForbidden` | 403 `forbidden` |
| anything else | 500 |

`gorbital.ErrScopeNotFound` is new, so an app writing its own authorizer need not import `modules/orgs` to refuse correctly. An authorizer that returns `nil` with a context that is not acting in `scopeID` fails the request with 500 and a message naming the authorizer: silent non-enforcement is not a possible outcome.

### 3. Setting it

```go
func WithScope(s Scope, a ScopeAuthorizer) Option         // the app
func (p *Platform) SetScope(Scope, ScopeAuthorizer) error // a module, from Module.Platform
```

An app has exactly one scope. A second `SetScope` is an error naming both sources.

### 4. The guard

`guard.Scope(permission)` replaces `guard.OrgMember(permission)`. Registration fails when the route is public, or its path lacks `{` + `Scope.PathParam` + `}`. `New` fails when any route uses it and nothing set a scope, naming the routes.

On success the actor acts in the scope — `OrgID` set, permissions those of the member's role, limited by an API key's scopes — and the request's database connections carry it through `Scope.Session`.

### 5. Roles and permissions

`Permission.ScopeRoles` replaces `Permission.OrgRoles`. Role descriptions and order come from `Scope.Roles` instead of the `orgRoleOrder` table. A permission with both fields set fails `New`, naming the module.

### 6. Compatibility

Everything below keeps working unchanged for all of v0.x, deprecated in the documentation and still listed by `apicheck`:

| Kept | Now means |
|---|---|
| `OrgAuthorizer`, `SetOrgAuthorizer` | `SetScope(orgshttp.DefaultScope(), adapter{a})` |
| `guard.OrgMember(p)` | `guard.Scope(p)` |
| `Permission.OrgRoles` | `ScopeRoles` |
| `postgres.WithOrg` | `postgres.WithScope(postgres.OrgSetting)` |
| `actor.Actor.OrgID` | the scope the actor is acting in |

`orgshttp.DefaultScope()` returns exactly today's vocabulary, so the OpenAPI document of a multi-tenant app is byte-identical to v0.2.1. That equality is a release gate.

### 7. Not configurable

The database session setting for a **new** app is fixed at `gorbital.scope_id`. It is invisible to users, generated row-level-security policies depend on it, and making it a choice buys nothing but variance. `Scope.Session` is still a function, so an app may set anything it likes — but `orb new` never writes a different one.

## Threat model

| Threat | Mitigation |
|---|---|
| Scope IDs can be probed by comparing responses | Unknown, deleted, forbidden and malformed IDs all return 404 with the same code and body. `ValidID` runs before the authorizer, so a malformed ID never reaches the app's SQL and cannot be distinguished by timing a database round trip |
| A custom `NotFoundCode` leaks existence through a different code | The code is one string used for every one of those four cases; a scope whose `NotFoundCode` is empty fails `New` |
| A permissive `ValidID` lets injection reach the app's query | IDs are passed as query parameters, never interpolated; `ValidID` is a narrowing, not the defence. Documented as such |
| A hand-written authorizer returns success without checking membership | The guard verifies the returned context is acting in the requested scope, and fails closed with 500 otherwise. `byo-identity` is the worked reference, and the generated isolation tests exercise the cross-scope case |
| Row-level security is bypassed because `Session` is nil | `orb doctor` reports a scope with RLS migrations and no `Session`; `orb add rls` sets it |
| A role gains permissions nobody reviewed | Role-to-permission grants stay in Go, declared by modules and frozen at startup. Which role a subject holds is the app's data; what a role *means* is code |

## Consequences

- `gorbital` stops importing `modules/orgs`. The composition layer's dependency budget (ADR-0019) falls by one module.
- Organisations become one supplied `Scope` (ADR-0048 amended), and the same module can be mounted under another word.
- Apps can use any tenant ID format, which was impossible before.
- Two vocabularies exist in the public API for all of v0.x. `apicheck` lists both; the documentation teaches only `Scope`.
- `actor.Actor.OrgID` now has a name that is narrower than its meaning. Accepted: renaming it would cost every app and every stored audit row more than the clarity is worth.

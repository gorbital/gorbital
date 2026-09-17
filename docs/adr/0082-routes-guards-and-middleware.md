# ADR-0082: Routes, guards and middleware

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0027, ADR-0052 · **Builds on:** ADR-0081

## Context

In v0.1 a module registers each operation with `huma.Register` and protects it by wrapping the `huma.Operation` in a hand-written function (`signedIn` in `delivery/projects.go`) that sets `Security` and the 401/403 responses; the handler then calls `actor.Require`. The app's middleware chain is a slice literal in `internal/app/routes.go`; per-IP limits cover `/v1/auth/` only; idempotency applies to every signed-in POST and PATCH outside `/v1/auth/`.

| Problem | Consequence |
|---|---|
| Protection is opt-in per operation | A forgotten wrapper exposes a route; nothing warns |
| A route's rules are spread out | Authentication in the wrapper, the permission in the use case, rate limits in `routes.go`, idempotency by path prefix |
| Middleware attaches only to the whole app | A module can't add behaviour to its own routes without editing `routes.go` |
| Changing the chain means editing generated code | Merges on every upgrade |

Developers asked for Nest.js-style routes that appear without editing several files.

Facts about Huma v2.39.1, which this design relies on: `huma.Register[I, O]` takes a typed handler; `Operation.Middlewares` is `[]func(huma.Context, func(huma.Context))` and wraps the operation's handler **including input parsing**, so an operation middleware runs before the body is read; `humago.Unwrap(ctx)` returns the `*http.Request` and `http.ResponseWriter`, and `humago.NewContext(op, r, w)` builds a context from them; `huma.WriteErr` writes an error through `huma.NewErrorWithContext`, which `openapi.InstallErrors` maps to problem+json with the request ID.

## Options

### How routes are declared

| Option | Verdict |
|---|---|
| File-based routing (Next.js): the folder path is the URL | Rejected: one Go package per URL segment, awkward `[id]` folders, handlers found by location instead of "go to definition" |
| Annotations (`//gorbital:route POST /v1/books`) generated into `routes.gen.go` (Nest.js decorators) | **Not in v0.2** (maintainer decision, 2026-09-17): a comment grammar to support for years, a parser and file watcher, and code in comments the compiler doesn't check, for a gain the explicit API mostly delivers. May be reconsidered after v0.2 as a generator on top of this API |
| Methods on a router (`r.Post(path, handler)`) | Rejected: Go methods can't have type parameters, so handlers would lose their typed input and output, and with them validation and OpenAPI |
| **Generic package-level functions over a router group: `gorbital.Post(group, path, handler, opts...)`, declared in `Module.Routes`** | **Chosen**: typed, checked by the compiler, one line per route, same pattern as `huma.Post` |

### Default protection

| Option | Verdict |
|---|---|
| Opt-in: `guard.Auth()` per route or group | Rejected: today's forgotten-wrapper problem |
| **Deny by default: every operation requires an authenticated actor unless it has `guard.Public()`** | **Chosen** |

### Middleware type

| Option | Verdict |
|---|---|
| A gorbital middleware type, or Huma's `func(huma.Context, next)` | Rejected: developers would learn a second type, and standard middleware wouldn't fit |
| **`httpx.Middleware` (`func(http.Handler) http.Handler`) at every level; route and group middleware adapted onto the operation with `humago.Unwrap`/`humago.NewContext`** | **Chosen** |

### Changing the built-in stack

| Option | Verdict |
|---|---|
| Positional options `Before(step)`, `After(step)`, `Replace`, `Without`, plus an override flag for protected steps | Rejected: a mini-language whose result is hard to see |
| **`WithMiddleware(mws...)` for the common case (after authentication), and `WithStack(func(gorbital.Stack) []httpx.Middleware)` returning the whole order in plain Go** | **Chosen** |

## Decision

### 1. Router and verbs

```go
type Router struct{ /* the huma.API, the module, inherited options */ }

func (r *Router) Group(prefix string, opts ...RouteOption) *Router

func Get[I, O any](r *Router, path string, h func(context.Context, *I) (*O, error), opts ...RouteOption)
// Post, Put, Patch, Delete alike.
```

- Options: `Summary`, `Description`, `Status`, `Tags`, `OperationID`, `Deprecated`, `Use`, and every guard. Groups pass their options to the routes and groups inside them.
- Operation IDs default to the module's name followed by the ID Huma generates from the method and path (`books-get-v1-books-by-id`); `OperationID` sets one explicitly. Handler names aren't used: reading them needs reflection on function values. A duplicate fails `Mount` naming both modules.
- The router requires the `humago` adapter (`openapi.New` already uses it).

### 2. Deny by default

- Every operation gets an operation middleware that refuses a request without an authenticated actor (`actor.From`) with 401 `unauthenticated`, before input parsing.
- `guard.Public()` removes that check and the operation's `Security`. `orb routes` and the Dev Portal list public routes.
- The check reads the actor set by the authentication step of the stack (ADR-0083), whichever authenticator is configured. With no authenticator, only public routes can succeed, and `gorbital.New` logs which routes are unreachable.

### 3. Guards

| Guard | Refuses with | Built on |
|---|---|---|
| `guard.Public()` | — | — |
| `guard.Permission(name)` | 403 `forbidden`; 403 `mfa_required` for a step-up permission | `actor.Actor.Can`, `auth.Catalog` |
| `guard.RecentReauth()` | 403 `reauthentication_required` (**new code**: the session must have signed in or verified a second factor within `auth.RecentVerification`) | `auth.Principal.RecentlySignedIn`, `RecentlyVerified` |
| `guard.RateLimit(n, window, opts...)` | 429 `rate_limited` with `Retry-After` | `ratelimitpg` (in-memory fallback), keyed `ByUser` (default), `ByAPIKey` or `ByIP`; limiter names appear in `/ops/auth/rate-limits` |
| `guard.OrgMember(permission)` (Phase 7) | 403/404 as `orgs.RequireMember` | `orgs`, through the app's organisation authorizer (`gorbital.OrgAuthorizer`, set by `orgshttp`; [ADR-0083 Phase 7 notes](0083-modules-stack-migrations-and-ejection.md#guardorgmember)) |
| `guard.New(guard.Spec{Name, Statuses, Check})` | the error `Check` returns, mapped by the module's `Errors` (or an `*httpx.Problem`) | — |

- Guards run as operation middleware in the order declared (group guards first), before input parsing. A refusal is written with `huma.WriteErr`, so it is problem+json with the request ID like every other error.
- `Check func(ctx context.Context, req guard.Request) error`; `guard.Request` exposes `PathParam`, `Query`, `Header` and `Operation`. `nil` allows; a mapped error refuses; an unmapped error is a 500, logged once. `Spec.Statuses` lists the statuses for the OpenAPI document; the mappings stay in the module's `Errors`, the one place a module maps errors.
- Not built, by decision during Phase 2 (2026-09-17): `guard.Role`, because actors carry permissions, not roles, and checks belong on permissions; `guard.Idempotent`, because the default stack already applies idempotency keys to every signed-in POST and PATCH, so a route option would add nothing.
- Each guard adds its security requirement and error responses to the operation's OpenAPI, and names itself in `x-gorbital-guards`.
- Refusals are counted by guard name and route pattern (bounded cardinality); spans record the refusing guard. No log line per refusal.

### 4. Middleware at four levels

```text
request → app stack (ADR-0083) → module Middleware → group Use → route Use → guards → input parsing → handler
```

| Level | API |
|---|---|
| App | `gorbital.WithMiddleware(mws...)`, after the authentication, rate-limit and idempotency steps; `gorbital.WithMiddlewareFunc(func(Deps) httpx.Middleware)` when it needs dependencies |
| Module | `Module.Middleware []httpx.Middleware` |
| Group, route | `gorbital.Use(mws...)`. There is no way to remove an inherited middleware from one route: routes that need different middleware go in their own group. `guard.Public()` is the one exception, for the default authentication check |
| Whole stack | `gorbital.WithStack(func(s gorbital.Stack) []httpx.Middleware)`; `Stack` has one field per built-in step; omitting `Recover` or `Auth` logs a warning at start and is reported by `orb doctor` (Phase 3) |

The route adapter builds each route's middleware chain once at registration. Per request it stores Huma's continuation in the request's context, runs the chain, and continues with a Huma context rebuilt from the (possibly replaced) request and writer, so context values set by middleware reach the handler. Measured cost: a fixed 5 allocations per request whatever the number of middlewares ([benchmarks](../benchmarks.md)).

Rate-limit guards resolve their limiter at registration: from `Deps.RateLimits` (`ratelimitpg`, shared across instances) when set, otherwise an in-memory `ratelimit.Limiter` per instance. A limiter name used with two different limits fails `Mount`.

`httpx.Capture(w)` exposes status, bytes and headers to middleware that reads the response (extracted from `AccessLog`).

## Threat model

| Threat | Mitigation |
|---|---|
| A route exposed by omission | Deny by default; `orb routes` lists public routes; tests assert 401 on every non-public route of the golden apps |
| A guard bypassed by ordering | Guards run as operation middleware after the whole app stack (authentication and trusted proxies resolved) and before parsing; order is declaration order and documented |
| Body parsed for an unauthenticated caller | Guards and the default check run before input parsing |
| Rate-limit key spoofing | `ByIP` uses the address after `httpx.TrustedProxies` (ADR-0052); `ByUser` and `ByAPIKey` use authenticated identities only |
| Custom guard leaking internals | Unmapped errors are 500 with a generic detail; the error is logged once with the request ID |
| Middleware writing after the handler | Documented rule; `httpx.Capture` for reading responses; no framework support for mutating a written response |

## Why

- Typed generic functions keep Huma's validation and OpenAPI, and the compiler checks every route.
- Deny by default turns the most likely mistake into a 401 instead of a data leak.
- One middleware type everywhere means every existing Go middleware works unchanged.

## Trade-offs

- One line per route instead of none: explicit over automatic, by decision.
- The route adapter costs at least one context allocation per route middleware; budgeted in [benchmarks](../benchmarks.md) (≤ 1 extra allocation and ≤ 5 % latency versus direct `huma.Register` with no route middleware).
- No per-route removal of inherited middleware (`Skip`): Go can't compare function values, and naming every middleware to make it removable adds API for a case groups already handle.

## Consequences

- `orb gen module` writes routes with `gorbital.Post(...)`; `orb gen middleware` writes middleware and guards with tests (Phase 8).
- v0.1 modules keep calling `huma.Register`; both work against the same API.

## Implementation

[v0.2 roadmap](../v0.2-roadmap.md), Phases 1 and 2.

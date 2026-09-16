# ADR-0060: Idempotency keys

**Status:** Accepted (2026-09-16)

## Context

Mobile and web clients retry requests when a connection drops or times out. A retried `GET`, `PUT` or `DELETE` is harmless; a retried `POST` creates a second project, sends a second invitation or runs a job twice, because the client can't know whether the first request reached the server. The v1.1 roadmap adds an `Idempotency-Key` header: the response is stored per caller for 24 hours and replayed, and a different body or a request still in progress is refused.

| Area | Today | Evidence |
|---|---|---|
| Creating requests | `POST /v1/projects` (or `/v1/orgs/{orgId}/projects`), `POST /v1/orgs`, invitations, `/ops` actions such as run-now and test emails: none deduplicate retries | `internal/modules/*/delivery` |
| Existing idempotency | Only outgoing email: the mail worker sets `mail.Message.IdempotencyKey` to `job-<id>` for the provider | ADR-0025, ADR-0033 |
| Middleware chain | Recover → trusted proxies → request ID → tracing → access log → security headers → CORS → cross-origin → body limit → maintenance → session authentication (actor in context) → per-IP auth limit; then the mux and per-operation Huma middlewares | `internal/app/routes.go` |
| Callers | `actor.Actor{Kind, ID}` set by `auth.Middleware` for users; service accounts (ADR-0058, in progress) are `actor.KindService`. The organisation of a request is only known inside its operation | `actor/actor.go`, `modules/auth/middleware.go` |
| Sensitive responses | `/v1/auth/*` answers with session tokens, `Set-Cookie`, TOTP secrets and recovery codes; API key creation (ADR-0058) shows a key once | `internal/modules/auth/delivery` |
| Shared state across instances | PostgreSQL only; `modules/ratelimitpg` shows the pattern: one statement decides atomically, hashed keys, database clock, a cleanup job | ADR-0052 |
| CORS | Default allowed headers `Authorization, Content-Type, X-Request-ID`; exposed `X-Request-ID, Retry-After` | `httpx/middleware.go` |

Constraints: PostgreSQL is the only required service (ADR-0014); modules don't import modules and core stays free of pgx (ADR-0019); error codes, settings and job names are public API (ADR-0015, ADR-0054); `/ops` changes must be additive (`TestOpsAPICompatible`); tunables are runtime settings (ADR-0031).

## Options

| Option | Verdict |
|---|---|
| Let each use case deduplicate (unique client-generated IDs per resource) | Rejected: every resource and generated module would reimplement it, and responses still differ between the first try and the retry |
| Huma per-operation middleware | Rejected: Huma contexts can't wrap the response writer portably, and the header must also cover non-Huma handlers |
| Store keys in memory per instance | Rejected: a retry reaching another instance runs again |
| Store the key after the request finishes (no in-progress state) | Rejected: two concurrent retries both run |
| **HTTP middleware with a PostgreSQL store: claim the key atomically before running, store the response after** | **Chosen**: one statement decides who runs across every instance; the stored response replays exactly; works for any handler |
| Scope keys by user and the organisation in the path | Rejected in favour of fingerprinting the path (below): the organisation isn't known before routing, and the path already carries it |

## Decision

### 1. `modules/idempotency`

A new module like `ratelimitpg`: core (`actor`, `httpx`), `modules/postgres`, pgx and the OpenTelemetry metric API.

```go
store, err := idempotency.NewStore(pool, idempotency.WithRetention(s.idempotencyRetention.Get))
handler = idempotency.Middleware(store, idempotency.WithSkip(isAuthPath))(handler)
```

| Topic | Decision |
|---|---|
| Methods | `POST` and `PATCH` carrying `Idempotency-Key`; other methods ignore the header |
| Key | 1–255 visible ASCII characters (0x21–0x7E), one header; otherwise 400 `invalid_idempotency_key` |
| Caller (scope) | `ActorScope`: the actor's kind and ID, for users and service accounts. Anonymous and system callers ignore the header, so keys are never shared between callers. `WithScope` replaces it |
| Fingerprint | SHA-256 of method, request target (path and query) and the body's SHA-256. The path includes the organisation ID in multi-tenant apps, so the same key sent to another organisation is refused rather than replayed |
| Storage | `idempotency_keys (id bytea PK, fingerprint, lock_token, locked_until, created_at, status, header jsonb, body bytea)`, logged (not UNLOGGED: a lost row would let a retry run twice). `id` is SHA-256 of scope and key: no user IDs or client keys are stored |
| Claim | One `INSERT … ON CONFLICT DO UPDATE … WHERE <expired or stale with the same fingerprint> RETURNING` claims a new key or takes over one; otherwise a `SELECT` reads it: another fingerprint → `ErrKeyReused`, no status yet → `ErrInProgress`, stored → the response. A key that disappears between the two statements is claimed again (at most three times) |
| Lock | A random token per claim; storing and releasing apply only while the token matches (`ErrLockLost`), so a request that outlived its lock can't overwrite its successor's response. Lock TTL 5 minutes (`DefaultLockTTL`), above the server's 60-second write timeout |
| Stored response | Status, `Content-Type`, `Location`, `ETag` and body up to 1 MiB (`WithMaxResponseBytes`) |
| Replay | The stored status, headers and body, plus `Idempotent-Replayed: true` |
| Released, not stored | 5xx, panics (released in a deferred call before the panic reaches `httpx.Recover`), 401, 403, 408, 429, responses with `Set-Cookie`, bodies over the cap, and requests whose handler calls `idempotency.DontStore(ctx)` (for responses shown once, such as new API keys). The next request with the key runs |
| Stored 4xx | Other client errors (such as 409 `project_name_taken` or 422 validation) are final outcomes of that request and are replayed |
| Concurrent request | 409 `idempotency_in_progress` with `Retry-After: 1` |
| Store unavailable | 503 `unavailable` without running the request: the client asked for at-most-once, and a write would need the database anyway |
| Time | The database clock; `WithClock` for tests |
| Retention | Read on every use from a function (`WithRetention`), applied to `created_at`, so shortening it applies to stored responses at once |
| Cleanup | `Store.DeleteExpired(ctx, limit)`, oldest first through a `created_at` index; `Store.Oldest` for `/ops/retention` |
| Metrics | `idempotency.requests` with `outcome` = stored, released, replayed, in_progress, key_reused or unavailable |

### 2. Full apps

| Piece | Where |
|---|---|
| Store and middleware | `internal/app/idempotency.go`; middleware last in the chain, after session authentication and the per-IP auth limit |
| Excluded routes | `/v1/auth/*`: their responses carry session tokens and cookies |
| Setting | `idempotency.retention`: 24 h, 1 h–7 days, group `retention`, reason required |
| Job | `idempotency_cleanup`, hourly, batches of 1 000 |
| Retention report | `idempotency_keys` in `GET /ops/retention` with its oldest key; the table isn't otherwise exposed through `/ops` |
| OpenAPI | `documentIdempotencyKey` adds the optional header parameter to every POST and PATCH operation outside `/v1/auth/` after registration; `/ops` gains an optional parameter only |
| CORS | `Idempotency-Key` allowed and `Idempotent-Replayed` exposed |
| Migration | `db/migrations/20260918000040_idempotency_keys.sql` |

## Why

- Claiming before running is the only way two concurrent retries on two instances can't both run; one statement makes it atomic without advisory locks or transactions held during the handler.
- A middleware covers every endpoint, generated or hand-written, with no per-resource code.
- Hashing the scope into the key's ID and ignoring anonymous requests make cross-caller replays impossible by construction.
- Releasing on errors a client can fix keeps the key useful: a retry after signing in, stepping up or waiting runs the request.

## Trade-offs

- Two extra statements on every POST or PATCH with a key (claim, then store), and the request body is buffered in memory (already bounded by `APP_MAX_BODY_BYTES`).
- A request running longer than the lock TTL can be taken over by a retry and run twice; a crashed instance's keys answer 409 for up to 5 minutes.
- A replay returns the stored response even if the caller has since lost access to the resource (the same as Stripe's behaviour); the response is the caller's own.
- Stored responses can contain personal data for up to `idempotency.retention` (at most 7 days).
- If storing the response fails after the handler ran, the key stays locked until its TTL: retries get 409 instead of the replay.
- A key reused across organisations by one user is refused (422) instead of processed independently.

## Consequences

- New module `modules/idempotency` (stable, ADR-0054), in CI's test, lint and govulncheck lists and CODEOWNERS' security-sensitive paths.
- New error codes `invalid_idempotency_key`, `idempotency_key_reused`, `idempotency_in_progress`; setting `idempotency.retention`; job `idempotency_cleanup`; recorded in both apps' `api/surface.json`.
- Every POST and PATCH outside `/v1/auth/` documents the header; `api/openapi.json`, Postman collections and templates are regenerated.
- API keys (ADR-0058) should call `idempotency.DontStore` when returning a new key.

## Implementation notes (2026-09-16)

- Library API: `NewStore(pool, WithRetention, WithLockTTL, WithLogger, WithClock)`, `Store.Claim`, `Lock.Complete`, `Lock.Release`, `Store.DeleteExpired`, `Store.Oldest`, `Fingerprint`, `Middleware(store, WithSkip, WithScope, WithMaxResponseBytes)`, `ActorScope`, `DontStore`, `ErrInProgress`, `ErrKeyReused`, `ErrLockLost`, `Header`, `ReplayedHeader`, `MaxKeyLength`, `Migrations`.
- A stale in-progress key (lock TTL passed, nothing stored) is taken over only by the same fingerprint; a different request is still refused with 422, because the first request may have had effects.
- A request body the server's body limit refuses isn't fingerprinted: the handler gets the same read error and answers 413 as without a key.
- Deferred: an idempotency key count in `GET /ops/system`; `GET /ops/retention` shows the oldest key.

| Check | Result |
|---|---|
| `TestConcurrentRequests` | A second request while the first is running gets 409 `idempotency_in_progress` with `Retry-After`; after the first finishes, a retry is replayed; the handler ran once |
| `TestRacingRequestsRunOnce` (race detector) | 20 identical concurrent requests: the handler runs once, one 201, the other 19 are 409 or replays |
| `TestReplayFidelity` | Status, `Content-Type`, `Location`, `ETag` and body replayed with `Idempotent-Replayed: true`; other headers not; stored 409 replayed for PATCH; GET, PUT and DELETE ignore the header |
| `TestFingerprintMismatch` | Another body, path, query or method with the key: 422 `idempotency_key_reused`, handler not run |
| `TestCallersDontShareKeys` | Two users and a service account with the same key and body each run and replay only their own response; anonymous and system callers are never replayed; no raw key or user ID in the table |
| `TestReleasedResponses` | 500, 503, a panic, 401, 403, 429, `Set-Cookie`, a body over the cap and `DontStore` release the key (the retry runs); a body exactly at the cap is stored; nothing is left in the table |
| `TestStaleLockExpires` | A claim left by a "crashed" request answers 409 until the lock TTL, then a different body is refused and the same request runs; the old lock gets `ErrLockLost` and can't delete the new response |
| `TestRetentionAndCleanup`, `TestSkippedRequests`, `TestInvalidKeys`, `TestRequestBodyLimit`, `TestStoreUnavailable` | Shortened retention applies to stored keys; `DeleteExpired` and `Oldest`; skipped routes; key syntax (255 accepted, 256, spaces, non-ASCII, control characters and two headers refused); 413 passes through; 503 `unavailable` with the database down |
| Apps `TestIdempotencyKeys` (both) | Retrying a project creation replays it and creates one project; another body 422; invalid key 400; another user's same key creates their own project; 409 `project_name_taken` replayed; 10 racing retries create one project; anonymous `/v1/echo` and `/v1/auth/login` (a new `Set-Cookie` each time) ignore the header |
| Apps `TestIdempotencyKeyDocumented` | The header is documented on `POST /v1/echo` and `POST /ops/mail/test`, not on `/v1/auth/login` or GET operations |
| `TestIdempotencyKeysStayInTheirOrganisation` (full-multi) | The same key and body sent to another organisation is refused with 422 and creates nothing there |
| `TestOpsRetention`, `TestPublicSurface`, `TestOpsAPICompatible`, `TestOpenAPIUpToDate`, `apicheck` | `idempotency_keys` policy listed; new names recorded; `/ops` changes additive; API files regenerated; `api/modules-idempotency.txt` recorded |

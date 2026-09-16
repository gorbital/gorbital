# Life of a request

What happens between a client sending a request to a Full app and receiving the response, in order, with the file and function responsible for each step. Read it next to `examples/full-single/internal/app/routes.go`.

```text
client
  │  POST /v1/projects   Authorization: Bearer …   {"name": "First"}
  ▼
http.Server (httpx.NewServer)          timeouts, APP_ADDR
  │
  ▼  middleware, outermost first (routes.go, buildHTTP)
Recover ─ RequestID ─ telemetry ─ AccessLog ─ SecureHeaders ─ CORS ─ CrossOrigin ─ BodyLimit ─ auth.Middleware ─ ratelimit
  │
  ▼
http.ServeMux                         method + path pattern
  │
  ▼
Huma operation (delivery/)            decode, validate → 422, call use case
  │
  ▼
use case (usecase/)                   actor.Require(permission), domain rules, transaction, audit event
  │
  ▼
repository (repository/)              hand-written SQL through pgx
  │
  ▼
PostgreSQL
  │
  ▼  back up the stack
use case returns a value or a domain error
delivery returns the response, or the error
httpx.Mapper turns a domain error into problem+json
AccessLog writes one line; the span ends
```

## 1. The server

`cmd/api/main.go` loads configuration (`app.LoadConfig`), builds the app (`app.New`) and calls `Run`. `Run` starts `httpx.NewServer(cfg.Addr, handler)`, next to the background workers (settings listener, job client, job definitions manager, release tracker, request collector), under `gorbital.dev/app`'s lifecycle: on SIGINT or SIGTERM, `/readyz` starts failing and live streams end, the server waits the drain delay (5 s in production, 0 in development) so load balancers stop sending traffic, then gives the server and workers up to 25 s to finish, then the cleanup stack closes what `New` registered (the database pool, then telemetry) in reverse order of creation.

## 2. Middleware

`buildHTTP` wraps the mux with `httpx.Chain(telemetry.RecordRoute(observability.RecordRoute(mux)), middlewares...)`; the first in the list runs first. The two `RecordRoute` wrappers hand the matched route pattern back to the telemetry and request-count middleware, because the session middleware passes a copy of the request to the mux and `http.ServeMux` sets the pattern on that copy.

| # | Middleware | Package | What it does | Can answer |
|---|---|---|---|---|
| 1 | `httpx.Recover` | core `httpx` | Catches a panic further down, logs `panic recovered` with the stack and request ID, and answers 500 `internal_error` if no response was started. `http.ErrAbortHandler` is re-panicked | 500 |
| 2 | `httpx.TrustedProxies` | core `httpx` | On requests from `APP_TRUSTED_PROXIES`, sets `RemoteAddr` to the client named by `X-Forwarded-For`; other requests keep their address ([ADR-0052](../adr/0052-shared-rate-limits.md)) | — |
| 3 | `httpx.RequestIDFrom` | core `httpx`, `requestid` | Generates `req_…`, puts it in the context and the response header. A valid incoming `X-Request-ID` is kept only from `APP_TRUSTED_CALLERS`, so clients can't give their requests another request's ID in logs, audit events and jobs | — |
| 4 | `tel.HTTPMiddleware` | `modules/telemetry` (`otelhttp`) | Starts the server span and records HTTP metrics; later log lines carry its `trace_id` and `span_id`. Each request starts a new trace, linked to an incoming `traceparent`; only `APP_TRUSTED_CALLERS` continue theirs. The span's `client.address` is `RemoteAddr`; metrics leave out the `Host` header and are labelled with the route pattern (`http.route`), never the path. The same metrics are served on `METRICS_ADDR` when set ([production](production.md#prometheus-metrics)) | — |
| 5 | `collector.Middleware` | `modules/observability`, app `observability.go` | Counts the request per minute, method and route pattern (`""` when no route matched): status class and duration in a histogram, written to PostgreSQL every 15 seconds for `/ops/observability` and incidents. A handler that panics counts as 500. Paths and query strings are never kept ([observability](observability.md)) | — |
| 6 | `httpx.AccessLog` | core `httpx` | After the response, one `http request` log line with `method`, `route`, `status`, `duration_ms`, `bytes`, `request_id`, `trace_id`, `span_id` | — |
| 7 | `httpx.SecureHeaders` | core `httpx` | Security headers on every response; HSTS for 365 days in production | — |
| 8 | `httpx.CORS` | core `httpx` | Answers preflight `OPTIONS` and sets CORS headers for `APP_CORS_ORIGINS`. Allowed request headers: `Authorization`, `Content-Type`, `X-Request-ID`, `Idempotency-Key`; exposed: `X-Request-ID`, `Retry-After`, `Idempotent-Replayed` | 204 preflight |
| 9 | `exceptCrossSitePosts(httpx.CrossOrigin)` | core `httpx`, app `routes.go` | `http.CrossOriginProtection`: refuses state-changing browser requests from other origins, using `Sec-Fetch-Site` and `Origin`, unless the origin is in `APP_CORS_ORIGINS`. Non-browser clients send neither header and pass. Skipped for Apple's two cross-site POSTs (callback and notifications), which carry their own proof | 403 `cross_origin_request_denied` |
| 10 | `httpx.BodyLimit` | core `httpx` | Refuses a declared `Content-Length` above `APP_MAX_BODY_BYTES`; cuts off undeclared bodies at the limit | 413 `request_too_large` |
| 11 | `auth.Middleware` | `internal/modules/auth`, `modules/auth` | Resolves the session: see below | 503 `auth_unavailable` |
| 12 | `ratelimit.Middleware` | core `ratelimit` | `auth.ip_requests_per_minute` requests a minute (default 60) per client IP, counted in PostgreSQL across instances, for non-GET requests under `/v1/auth/`, and Google and Apple `start` and `callback` redirects. Other requests have no key and pass | 429 `rate_limited` |
| 13 | `idempotency.Middleware` | `modules/idempotency`, app `idempotency.go` | For signed-in POST and PATCH requests with `Idempotency-Key` outside `/v1/auth/`: claims the key in PostgreSQL, replays a stored response, or refuses a reused key or one still in progress ([idempotency](idempotency.md)) | 400 `invalid_idempotency_key`, 409 `idempotency_in_progress`, 422 `idempotency_key_reused`, 503 `unavailable`, a replayed response |

Order matters: a panic anywhere is caught; the client's address is resolved before anything records it; the request ID and span exist before anything logs; CORS answers preflights before the cross-origin check; the body limit applies before anything reads the body. The auth middleware and rate limiter are added only when the auth module exists, which is always in a Full app.

## 3. Authentication

`auth.Middleware` never rejects a request for missing credentials: it only establishes who is calling. Endpoints decide whether that's required.

1. `authlib.TokenFrom` reads `Authorization: Bearer <token>`, or else the `__Host-session` cookie.
2. No token: continue anonymously.
3. The token is hashed with SHA-256 and looked up in `auth_sessions` with its user and roles. If the store fails, answer 503 `auth_unavailable` rather than treating the caller as anonymous.
4. Not found, expired (idle 14 days, absolute 90 days) or revoked: continue anonymously.
5. Found: put the principal (user, session, roles, whether the second factor was used and when) and its `actor` into the context. If the session's last-seen time is at least a minute old (`authlib.SessionTouchInterval`), update it, which also extends the idle expiry.

Roles are read on every request, so granting or revoking a role applies at once.

## 4. Routing and decoding

`http.ServeMux` matches method and path pattern. Unmatched requests reach the catch-all, which answers 404 `not_found` with `no route matches <METHOD> <path>`.

API routes are Huma operations registered in each module's `delivery/` package through `registerModules` (`internal/app/modules.go`). Huma decodes path, query, header and body into the operation's Go input type and validates it against the struct tags (`minLength`, `enum`, `format`, …). Invalid input never reaches the handler: it answers 422 `validation_failed` with `errors[]` naming each field. Unknown JSON fields are ignored ([ADR-0027](../adr/0027-api-contract-and-docs.md)).

## 5. The use case

The handler calls one use case method with plain values. A use case, such as `projects.Service.Create`:

1. **Authorizes.** `actor.Require(ctx, permission)` or the module's own ownership rule; unauthenticated callers get `ErrUnauthenticated` (401), missing permissions `ErrForbidden` (403), and roles that need a second factor without one `mfa_required` (403). Multi-tenant resources call `orgs.RequireMember` first and hide other organisations as 404.
2. **Applies domain rules** from `domain/`: validation beyond the API schema, state transitions.
3. **Writes in a transaction** through the `TxManager` port when more than one statement must commit together, such as the row and its audit event, or the row and a job.
4. **Records an audit event** through `audit.Recorder` (`modules/auditpg`), with the actor, request ID and resource. Audit writes use `context.WithoutCancel`, so a client disconnecting doesn't lose the record.
5. **Returns** a domain value or a domain error. Use cases never import `net/http` or pgx.

## 6. The repository

Each operation is one Go file with one SQL constant, run through `postgres.DBTX` (the pool or a transaction). Expected database conditions become domain errors, such as a unique violation on the `projects_owner_name` index becoming `ErrProjectNameTaken`; anything else is wrapped with `%v` so driver types don't leak. Every query is a traced span with its SQL text and never its arguments.

## 7. The response or the error

On success, Huma encodes the output type as JSON with the status the operation declares.

On error, the error travels up unchanged to the delivery layer, which hands it to the app's `httpx.Mapper` (built in `buildHTTP`):

1. An `*httpx.Problem` anywhere in the chain is used as it is.
2. Otherwise the first mapping whose error matches with `errors.Is` gives the status, code and detail. Mappings are registered by `openapi.InstallErrors`, by `buildHTTP` for pagination errors, and by each `module_<name>.go`.
3. Otherwise: 500 `internal_error` with a generic detail, and the full error is logged once with the request ID. Internal messages never reach the client.

`httpx.WriteProblem` writes `application/problem+json` with `Cache-Control: no-store` and fills `request_id`. See [error handling](error-handling.md).

## 8. After the response

`AccessLog` writes its line, the span ends and is exported if `OTEL_EXPORTER_OTLP_ENDPOINT` is set. Work queued during the request, such as an email, is a River job row that committed with the request's transaction or on its own; a worker in the same process picks it up, carrying the request ID, trace and actor in the job's metadata ([background jobs](background-jobs.md)).

## Following one request

Every response carries `X-Request-ID`, and every problem body its `request_id`. Search the logs for it to find the access log line, any error logged for it, and (with tracing on) the trace with every SQL query and job it caused:

```text
level=INFO msg="http request" service=acme-api method=POST route="/v1/projects" status=201 duration_ms=4 bytes=180 request_id=req_… trace_id=… span_id=…
```

# The middleware stack

Every request to an app on `gorbital.Main` passes through the same built-in middleware before it reaches a route: recovery, client addresses, request IDs, telemetry, logs, security headers, CORS, cross-site protection, body limits, maintenance mode, authentication, rate limits and idempotency keys. `gorbital.New` builds each step from the configuration as `gorbital.Stack`, one field per step ([Methods](../methods/gorbital.md#Stack), [ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md#4-the-default-stack)). This page replaces the middleware section of [Life of a request](request-lifecycle.md) for v0.2 apps; the order is the same as a v0.1 app's `routes.go`.

## The default order

Outermost first:

| # | `Stack` field | What it does | Can answer | Configured by |
|---|---|---|---|---|
| 1 | `Recover` | Catches a panic further down, logs it with the stack and request ID, and answers 500 `internal_error` if nothing was written yet | 500 | — |
| 2 | `TrustedProxies` | On requests from a trusted load balancer, sets the client's address from `X-Forwarded-For` ([ADR-0052](../adr/0052-shared-rate-limits.md)) | — | `APP_TRUSTED_PROXIES` |
| 3 | `RequestID` | Gives each request a `req_…` ID in the context and the `X-Request-ID` header; keeps an incoming one only from trusted callers | — | `APP_TRUSTED_CALLERS` |
| 4 | `Telemetry` | Starts the server span and records HTTP metrics, labelled by route pattern | — | `OTEL_EXPORTER_OTLP_ENDPOINT`, `METRICS_ADDR` |
| 5 | `Observability` | Counts the request per minute and route for `/ops/observability` and automatic incidents ([observability](observability.md)) | — | — |
| 6 | `AccessLog` | One structured `http request` line after the response, with the user noted by authentication | — | `APP_LOG_LEVEL`, `APP_LOG_FORMAT` |
| 7 | `SecureHeaders` | Security headers; HSTS for a year in production | — | `APP_ENV` |
| 8 | `CORS` | Answers preflights and sets CORS headers for the allowed origins | 204 | `APP_CORS_ORIGINS` |
| 9 | `CrossOrigin` | Refuses state-changing browser requests from other sites (`http.CrossOriginProtection`), except Apple's sign-in callback and notifications, which carry their own proof | 403 `cross_origin_request_denied` | `APP_CORS_ORIGINS` |
| 10 | `BodyLimit` | Refuses larger bodies, and cuts off undeclared ones at the limit | 413 `request_too_large` | `APP_MAX_BODY_BYTES` |
| 11 | `Maintenance` | While `maintenance.enabled` is on, answers every request with 503 except health checks, `/version`, docs, `/.well-known/`, `/ops/` and `/v1/auth/` (`httpx.Maintenance`) | 503 `maintenance` | Runtime settings `maintenance.enabled`, `maintenance.message`, `maintenance.retry_after` |
| 12 | `Auth` | Runs the authenticator's middleware ([`WithAuth`](main-go.md#the-file)), which sets who is calling; passes requests on unchanged without an authenticator | Whatever the authenticator answers, such as 503 `auth_unavailable` | `WithAuth` |
| 13 | `RateLimit` | Limits non-GET requests under `/v1/auth/`, and sign-in redirects, per client address, shared by every instance | 429 `rate_limited` | Runtime setting `auth.ip_requests_per_minute` |
| 14 | `Idempotency` | Replays the stored response of a signed-in POST or PATCH retried with the same `Idempotency-Key` ([idempotency](idempotency.md)) | 400, 409, 422, 503, or the replayed response | Runtime setting `idempotency.retention` |
| — | [`WithMiddleware`](#adding-your-own) | Your middleware, in the order the options are given | Anything | — |

Then the router matches the route, the module's, group's and route's own middleware run, then its guards, and only then is the body parsed ([Guards and middleware](guards-and-middleware.md)).

Why this order: a panic anywhere is caught; the client's address is known before anything records it; the request ID and span exist before anything logs; CORS answers preflights before the cross-site check; the body limit applies before anything reads a body; maintenance answers before authentication spends a database query; rate limits and idempotency keys need the client and the caller.

In development with `DEV_CONSOLE_TOKEN`, the [dev console](dev-console.md) sits in front of the whole stack: requests to `/_dev/` never reach it.

## Adding your own

Most middleware belongs after the stack, where the caller is known. Use `WithMiddleware`:

```go
gorbital.Main(
	gorbital.WithModules(modules.All()...),
	gorbital.WithMiddleware(requireClientVersion("2.4.0")),
)
```

When your middleware needs the database, a runtime setting or the logger, build it with the app's dependencies:

```go
gorbital.WithMiddlewareFunc(func(d gorbital.Deps) func(http.Handler) http.Handler {
	return auditReads(d.Audit)
})
```

Any `func(http.Handler) http.Handler` works. For middleware on one module, group or route instead of every request, use `Module.Middleware` or `gorbital.Use` ([Guards and middleware](guards-and-middleware.md)).

## Changing the order

`WithStack` receives every built-in step and returns the order to run them in. It is plain Go: keep, drop, move or insert steps.

```go
gorbital.WithStack(func(s gorbital.Stack) []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		s.Recover, s.TrustedProxies, s.RequestID,
		tenantFromHost, // yours, before anything is logged
		s.Telemetry, s.Observability, s.AccessLog, s.SecureHeaders, s.CORS, s.CrossOrigin,
		s.BodyLimit, s.Maintenance, s.Auth, s.RateLimit, s.Idempotency,
	}
})
```

`s.Default()` returns the default order, for small changes:

```go
gorbital.WithStack(func(s gorbital.Stack) []func(http.Handler) http.Handler {
	return slices.Insert(s.Default(), 1, tenantFromHost) // right after Recover
})
```

`WithMiddleware` still runs after whatever `WithStack` returns.

Leaving out `Recover` or `Auth` is allowed, because it's explicit code, but `New` logs a warning at every start:

```text
level=WARN msg="the middleware stack has no Auth step: no request is authenticated, so only public routes succeed (gorbital.WithStack)"
```

Leaving out other steps is silent: check what you give up in the table above. Without `CrossOrigin`, cookie-authenticated requests from other sites are accepted; without `BodyLimit`, a client can send an unbounded body.

## Maintenance mode

`Maintenance` is `httpx.Maintenance` from core, which you can use in any `net/http` server:

```go
h := httpx.Maintenance(httpx.MaintenanceOptions{
	Enabled:    enabled,    // config.Value[bool], such as a runtime setting
	Message:    message,    // config.Value[string]; empty sends a generic message
	RetryAfter: retryAfter, // config.Value[time.Duration], sent as Retry-After
	Open:       []string{"/livez", "/readyz", "/ops/"},
})(mux)
```

A path in `Open` ending in `/` keeps everything under it open; other paths match exactly. Keep health checks open, or your load balancer takes every instance out of rotation. In an app on `gorbital.Main`, operators switch it with `PUT /ops/settings/maintenance.enabled` (Phase 4), and every instance follows within a second.

## Related

- [Your main.go](main-go.md)
- [Guards and middleware](guards-and-middleware.md): middleware and guards on modules, groups and routes.
- [Life of a request](request-lifecycle.md): what happens after the stack.

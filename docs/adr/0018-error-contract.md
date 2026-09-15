# ADR-0018: Error contract and problem+json

**Status:** Accepted (2026-09-14)

## Context

Errors cross four boundaries: module → framework → application → HTTP client. Without a contract, driver errors leak into public APIs (making pgx part of gorbital's API), a shared "error kinds" package couples every module, clients parse message strings, and the same error gets logged several times.

## Options

1. A central `errs` package with generic kinds (NotFound, Invalid, …) that every module wraps into.
2. Domain errors implement `HTTPStatus()`; the framework reads it.
3. Modules own their errors; mapping to HTTP happens at the composition root; RFC 9457 problem+json responses with stable codes.

## Decision

Option 3.

```text
module        → exported sentinel or typed errors; driver errors translated or wrapped with %v
framework     → one HTTP error handler: look up mapping, else 500; log once with request_id and trace_id
application   → owns the mapping table (internal/app/errors.go, generated, editable)
HTTP client   → application/problem+json {type, title, status, code, detail, request_id, errors[]}
```

| Rule | Decision |
|---|---|
| Public errors | Only exported sentinels (`ErrInvalidCredentials`) and typed errors carrying data (`*ThrottledError{RetryAfter}`), documented on the functions that return them |
| Sentinel vs typed | Sentinel when callers only match; typed when callers need data |
| Return type | Exported functions return `error`, never concrete error types |
| Wrapping | `%w` inside a module; at the module boundary, `%v` for anything not documented as public, so driver errors (`pgx.ErrNoRows`, `pgconn.PgError`) never become matchable API |
| Translation | Repositories translate expected driver conditions into domain errors (unique violation → `ErrProjectNameTaken`) |
| Error codes | Stable snake_case (`project_name_taken`); public API under ADR-0015; owned by the module defining the error |
| Messages | Error strings are lowercase, without punctuation, and not API |
| HTTP mapping | In the app's `internal/app/errors.go`; domain packages never know HTTP status codes |
| Validation errors | 400 with `errors[]` of `{field, code, message}` |
| Unknown errors | 500 with a generic body; details only in logs and traces |
| Logging | Modules return errors and never log errors they return. Only edges log: HTTP error handler, job runner, `main`. |
| Panics | Recover middleware → 500, logged once, span marked as error; nothing echoed to the client |

Core package `gorbital.dev/httpx` provides the problem type, the handler and the mapping helper. There is no shared error-kinds package.

## Why

- Modules stay independent, and domain code stays free of HTTP.
- Clients get one predictable error format with stable codes.
- Operators get each error logged once, correlated to its request.

## Trade-offs

- Each module declares its own errors; mapping entries must be kept up to date (the generator adds them with each module).

## Consequences

- CI checks that every exported sentinel error in official modules has a mapping in the golden reference apps.
- Error codes appear in generated API documentation.

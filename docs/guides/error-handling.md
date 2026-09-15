# Error handling

How errors are created, passed, mapped to HTTP responses and logged, from the domain layer to the client. The contract is [ADR-0018](../adr/0018-error-contract.md); the code is `apistock.dev/httpx` (`problem.go`) and `apistock.dev/modules/openapi` (`errors.go`).

## The rules

1. **Errors are values with meaning in the layer that creates them.** A domain returns `ErrProjectNotFound`, never `404`. A repository returns `ErrProjectNameTaken`, never a pgx error.
2. **Driver and library errors are wrapped with `%v`, not `%w`,** so callers can't depend on pgx or provider types: `fmt.Errorf("insert project: %v", err)`.
3. **Only the app maps errors to HTTP**, through one `httpx.Mapper` built in `internal/app/routes.go`. Modules register their own mappings in `internal/app/module_<name>.go`.
4. **Errors are logged once, at the edge.** Lower layers return errors without logging them. The mapper logs only what it can't map (500s); expected errors (404, 409, 422) aren't logged as errors, and the access log line already records the status.
5. **Clients get stable codes, never internal messages.** `code` is public API ([ADR-0015](../adr/0015-public-api-and-stability-tiers.md)); `title` and `detail` may change.

## The response: problem details

Every error response is `application/problem+json` ([RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)), extended with `code` and `request_id`:

```json
{
  "title": "Conflict",
  "status": 409,
  "code": "project_name_taken",
  "detail": "you already have a project with this name",
  "request_id": "req_9f86d081884c7d65"
}
```

| Field | Type | Always? | Meaning |
|---|---|---|---|
| `type` | string | No | URI of the problem type, when one exists |
| `title` | string | Yes | `http.StatusText(status)` |
| `status` | integer | Yes | The HTTP status, repeated |
| `code` | string | Yes | Stable `snake_case` code. Branch on this |
| `detail` | string | Usually | Human-readable explanation. Don't parse it |
| `request_id` | string | Yes | The request's `X-Request-ID`; the same ID is in the logs and the trace |
| `errors` | array | Validation only | `[{"location": "body.name", "message": "expected length >= 1"}]`. Never echoes the submitted value, which may be a password |

Responses also set `Cache-Control: no-store`.

## How an error becomes a response

```text
repository   postgres.UniqueViolation(err) → return domain.ErrProjectNameTaken
    │
usecase      return err  (unchanged, or wrapped with %w to add context)
    │
delivery     return nil, err
    │
Huma         huma.NewErrorWithContext → openapi.InstallErrors → mapper
    │
httpx.Mapper.Match
    ├─ err is or wraps *httpx.Problem  → use it
    ├─ errors.Is(err, mapping.Err)     → NewProblem(mapping.Status, mapping.Code, mapping.Detail or err.Error())
    └─ no match                        → log "unhandled error" with request_id; 500 internal_error
    │
httpx.WriteProblem  → Content-Type: application/problem+json, Cache-Control: no-store, request_id filled
```

`openapi.InstallErrors(mapper)` replaces Huma's `huma.NewError` and `huma.NewErrorWithContext`, so Huma's own errors (validation, malformed JSON, unsupported media type) and handler errors all go through the same mapper and come out in the same shape. Outside Huma, middleware writes problems directly with `httpx.WriteProblem`, and the catch-all route answers 404 `not_found`.

## Mappings

A mapping connects a sentinel error to a status and code:

```go
// internal/app/module_projects.go
mapper.Add(
	httpx.Mapping{Err: projectsdomain.ErrProjectNotFound, Status: http.StatusNotFound, Code: "project_not_found", Detail: "no project of yours has this ID"},
	httpx.Mapping{Err: projectsdomain.ErrProjectNameTaken, Status: http.StatusConflict, Code: "project_name_taken", Detail: "you already have a project with this name"},
	httpx.Mapping{Err: projectsdomain.ErrProjectVersionConflict, Status: http.StatusConflict, Code: "project_version_conflict", Detail: "the project changed since you read it; get it again and retry"},
)
```

`Mapper.Add` validates each mapping when the app starts, so mistakes fail at boot rather than in production:

| Check | Error |
|---|---|
| `Err` is nil | `httpx: mapping error must not be nil` |
| Status outside 400–599 | `httpx: mapping "x": status 200 is not an error status` |
| Code not `snake_case` | `httpx: mapping code "X" must be snake_case` |
| The same error mapped twice | `httpx: duplicate mapping for "x"` |
| One code with two statuses | `httpx: code "x" is mapped with statuses 404 and 409` |

Several errors may share a code with the same status, such as `unauthenticated` from different modules. Matching uses `errors.Is`, so wrapped errors (`fmt.Errorf("create: %w", ErrProjectNameTaken)`) still match. Mappings are checked in registration order.

When `Detail` is empty, the error's own message is used, so write domain error messages for API clients: lowercase, no internal names.

## Codes without a mapping

When Huma or middleware produces a status with no specific code, `httpx.DefaultCode(status)` names it:

| Status | Code |
|---|---|
| 400 | `bad_request` |
| 401 | `unauthorized` |
| 403 | `forbidden` |
| 404 | `not_found` |
| 405 | `method_not_allowed` |
| 409 | `conflict` |
| 413 | `request_too_large` |
| 422 | `validation_failed` |
| 429 | `rate_limited` |
| 503 | `unavailable` |
| other 5xx | `internal_error` |
| other 4xx | `error` |

## Codes in a Full app

| Where | Codes | Reference |
|---|---|---|
| Middleware | `cross_origin_request_denied` (403), `request_too_large` (413), `auth_unavailable` (503), `rate_limited` (429), `internal_error` (500 from a panic), `not_found` (404, no route) | [Life of a request](request-lifecycle.md) |
| Pagination (`apistock.dev/page`) | `invalid_cursor`, `invalid_sort`, `invalid_limit` (400) | `routes.go` |
| Authentication | `unauthenticated`, `forbidden`, `mfa_required`, `mfa_unavailable`, `passkeys_unavailable`, and each flow's codes | [Authentication](authentication.md#error-codes) |
| Ops APIs | `setting_not_found`, `setting_version_conflict`, `job_definition_disabled`, `invalid_recipient`, … | [Ops API](ops-api.md#error-codes) |
| Resources | `<resource>_not_found`, `<resource>_<field>_taken`, `<resource>_version_conflict` | `internal/app/module_<name>.go` |

## Adding an error

1. Declare the sentinel in the module's `domain/errors.go`:

   ```go
   var ErrInvoicePaid = errors.New("a paid invoice can't be changed")
   ```

2. Return it from the domain or use case.
3. Map it in `internal/app/module_invoices.go`:

   ```go
   httpx.Mapping{Err: invoicesdomain.ErrInvoicePaid, Status: http.StatusConflict, Code: "invoice_paid"},
   ```

4. Document the response on the Huma operation (`Errors: []int{http.StatusConflict}`) so it appears in `/docs`, then export `api/openapi.json`.
5. Test the status and code in `internal/app/<names>_test.go`.

For errors that carry data, such as a retry delay, define a type implementing `error` and match it with `errors.As` in the use case, or return an `*httpx.Problem` built with `httpx.NewProblem` from delivery code.

## Logging

- **Unmapped errors:** `level=ERROR msg="unhandled error" err=… request_id=…`, once, by the mapper.
- **Panics:** `level=ERROR msg="panic recovered" panic=… stack=… request_id=…`, by `httpx.Recover`.
- **Every request:** `level=INFO msg="http request" … status=… request_id=…`, by `httpx.AccessLog`.
- **Background jobs:** failed attempts are recorded on the job (visible in `GET /ops/jobs/runs/{id}`) and retried; `mail.ErrRejected` and other permanent errors cancel the job instead of retrying ([background jobs](background-jobs.md#failures-retries-and-shutdown)).
- **Never logged:** passwords, tokens, codes, secrets (`config.Secret` prints `[redacted]`), email addresses, request bodies.

## Startup errors

Configuration errors don't reach HTTP: `LoadConfig` collects every problem and `cmd/api` exits with `invalid configuration:` and one line per variable. Construction errors (`app.New`: database connection, invalid mappings, invalid keys) exit with the wrapped cause, such as `postgres: connect: …`. See [Troubleshooting](../start/troubleshooting.md#configuration-errors-at-start).

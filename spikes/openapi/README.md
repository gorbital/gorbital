# Spike: code-first OpenAPI with Huma in a layered module

**Status:** done · **Date:** 2026-09-14 · **Decides:** [ADR-0027 API contract and documentation](../../docs/adr/0027-api-contract-and-docs.md)

Throwaway code. Nothing outside `spikes/` may import it.

## Question

Can developers write endpoints **only in Go** (no YAML) and get OpenAPI, docs, validation and problem+json errors automatically, while keeping the third-party HTTP library confined to the `delivery/` layer of our layered modules?

## What was built

```text
cmd/api/main.go                               server; `api openapi` prints the spec
internal/app/app.go                           composition root: humago on http.ServeMux, access control, request ID, /docs
internal/app/errors.go                        Problem type (RFC 9457 + code + request_id), error mapping table
internal/app/architecture_test.go             layer import rules
internal/modules/projects/
  module.go                                   wires layers
  domain/project.go                           entity + rules + errors (stdlib only)
  usecase/{ports.go,service.go,_test.go}      create, list, get, archive
  repository/memory.go                        in-memory adapter (org-scoped)
  delivery/projects.go                        Huma operations (the only layer importing Huma)
```

Run:

```bash
go test ./...
go test ./internal/app -run TestPrintSamples -v
go run ./cmd/api            # http://127.0.0.1:8088/docs
go run ./cmd/api openapi    # prints OpenAPI 3.1 JSON
```

Environment: Go 1.25.3, Huma v2.39.1, macOS arm64.

## Results

| Check | Result |
|---|---|
| Endpoints written only in Go; OpenAPI generated | ✅ 3 paths / 4 operations, 14.8 KB OpenAPI 3.1 document |
| Huma confined to `delivery/` (+ `module.go`, `internal/app`) | ✅ enforced by `architecture_test.go` |
| Domain has no JSON tags or third-party imports | ✅ |
| Standard `net/http` ServeMux (no custom router) | ✅ `humago` adapter |
| Per-operation auth and permissions | ✅ Huma middlewares; `security` documented as bearer |
| Org scoping in routes | ✅ `/v1/orgs/{orgId}/projects`; non-member → 403; cross-org ID lookup → 404 |
| Handlers return domain errors unchanged | ✅ app maps them to problem+json with stable codes |
| Request validation from Go types | ✅ 422 with field locations (`body.name`) |
| Error responses carry `request_id` | ✅ |
| `Problem` schema (with `code`, `request_id`) documented for all error statuses | ✅ |
| Docs page | ✅ served at `/docs`; visual check deferred to first-run spike (static preview can't run scripts) |
| Tests | ✅ all pass: usecase tests, HTTP table tests (9 cases), OpenAPI checks, layer import test |

### Sample responses

```text
POST /v1/orgs/org_acme/projects → 201 application/json
{"id":"prj_7c3ca4a8f7d0","name":"Website","archived":false,"created_at":"2026-09-14T17:14:13.820106+03:00"}

POST (duplicate) → 409 application/problem+json
{"title":"Conflict","status":409,"code":"project_name_taken","detail":"project name is already taken","request_id":"req_981e299b5872"}

POST {"name":"","extra":1} → 422 application/problem+json
{"title":"Unprocessable Entity","status":422,"code":"validation_failed","detail":"validation failed","request_id":"req_e8c182a445ec",
 "errors":[{"message":"expected length >= 1","location":"body.name","value":""},
           {"message":"unexpected property","location":"body.extra","value":{"extra":1,"name":""}}]}

GET as non-member → 403 application/problem+json
{"title":"Forbidden","status":403,"code":"forbidden","detail":"missing permission projects:read","request_id":"req_b77a0d1c3167"}
```

### Cost

| Measure | Value |
|---|---|
| Direct dependency | `github.com/danielgtaylor/huma/v2` only (no other third-party packages linked into the binary) |
| Binary size (stripped) | 6.4 MB |
| `delivery/projects.go` | 121 lines for 4 endpoints (≈30 per endpoint including input/output types and registration) |
| `domain` / `usecase` | 38 / 55 lines |
| `internal/app/errors.go` | 85 lines (shared by all modules) |

## Findings

1. **Code-first works cleanly with the layered layout.** Developers write Go types and a handler; docs, validation and error schemas follow automatically. Huma stays in `delivery/`.
2. **Error customisation needs Huma's package-level `NewError` / `NewErrorWithContext` hooks.** Overriding them once, in the composition root, gives every error (domain, validation, auth) the same problem+json shape with `code` and `request_id`, and documents the `Problem` schema. This is the only package-level assignment allowed; the generated `architecture_test.go` must enforce that only `internal/app` assigns it.
3. **Built-in Scalar docs load from a CDN** (unpkg). `/docs` must be our own handler serving an embedded, pinned Scalar asset to work offline (ADR-0027).
4. **`huma.DefaultConfig` adds a `$schema` link to response bodies** via create hooks; set `CreateHooks = nil` for clean responses.
5. **Unknown request fields are rejected by default** (`unexpected property`). Strict input catches client typos, but older servers would reject newer clients sending added fields. Needs an explicit v0.1 decision.
6. **Timestamps used local time** (`+03:00`). Generated clocks must return UTC.
7. **403 details name the missing permission.** Acceptable for authenticated members; never reveal whether an organisation exists to non-members (consider 404 for non-members in v0.4).

## Decision input for ADR-0027

Adopt **code-first with Huma v2**, confined to `delivery/`, with: error hooks installed only in `internal/app`; own `/docs` with embedded Scalar; `CreateHooks = nil`; UTC clocks; spec exported by `my-api openapi` into `api/openapi.json`, committed and checked for breaking changes in CI.

Open for v0.1: unknown-field policy (finding 5); non-member response code (finding 7).

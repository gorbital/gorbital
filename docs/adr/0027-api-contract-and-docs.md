# ADR-0027: API contract and documentation

**Status:** Accepted (2026-09-14), amended by [ADR-0049](0049-public-docs-and-website.md), [ADR-0051](0051-operations-v0-5.md) (Postman collection and `llms.txt` exported with the spec), [ADR-0053](0053-internal-security-review.md) (docs and the OpenAPI document switched together, off by default in production) · decided by [spikes/openapi](../../spikes/openapi/README.md)

## Context

Developers must test the API immediately after `orb new`: interactive docs at `/docs`, an OpenAPI document and a Postman collection. Documentation should look like Mintlify API reference pages. All of these need one source of truth, and the choice shapes how every handler is written. The priority is a seamless developer experience: developers focus on business logic, gorbital handles contract, docs and validation.

## Options

| Option | How it works | For | Against |
|---|---|---|---|
| A. Spec-first | Owned `openapi.yaml` → generated Go interfaces (oapi-codegen) → handlers | Contract-first; no framework in handlers | Developers write YAML; two steps per endpoint |
| **B. Code-first (Huma)** | Typed Go inputs/outputs and handlers generate OpenAPI 3.1 | Go only; docs and validation automatic; RFC 9457 errors; runs on `net/http` | Third-party dependency in the HTTP layer |
| C. Annotations (swaggo) | Comments generate specs | Easy start | Comments drift from behaviour |

## Decision

**Option B: code-first with Huma v2, confined to the `delivery/` layer.**

| Topic | Decision |
|---|---|
| Where Huma is allowed | `internal/modules/*/delivery`, `internal/modules/*/module.go`, `internal/app`. Never `domain/`, `usecase/`, `repository/` (enforced by `architecture_test.go`) |
| Router | Standard `http.ServeMux` via the `humago` adapter |
| Handlers | Typed input/output structs; handlers return domain errors unchanged |
| Errors | `internal/app` installs `huma.NewError` and `huma.NewErrorWithContext` once at startup, producing `Problem` (RFC 9457 + `code` + `request_id` + `errors[]`) from the app's error mapping table (ADR-0018). This is the only permitted package-level assignment; the architecture test enforces it |
| Validation | From Go struct tags; 422 `validation_failed` with field locations |
| Auth and tenancy | Per-operation Huma middlewares for session, org membership and permission; `security` documented |
| Config | `CreateHooks = nil` (no `$schema` links in responses); built-in docs disabled |
| Interactive docs | Own `/docs` handler, offline with no CDN, Mintlify-style layout with examples and try-it; configurable enabled / disabled / ops-only. Originally an embedded, pinned Scalar asset; since ADR-0049, the gorbital reference rendered by `modules/openapi/reference` |
| Spec export | `my-api openapi` writes `api/openapi.json`; `orb dev` refreshes it; committed so API changes appear in pull requests |
| Breaking changes | CI compares `api/openapi.json` with the base branch and fails on breaking changes |
| Postman and AI | `api/postman_collection.json` and `api/llms.txt` generated from the exported spec (v0.5) |
| Undocumented routes | Architecture test fails if a route is registered outside Huma operations (except `/docs`, `/livez`, `/readyz`, `/.well-known/*`) |
| Time values | Generated clocks return UTC |
| Generator | `orb gen resource` and `orb gen endpoint` create input/output types, operation registration, handler and use-case stubs |
| Project docs | `gorbital.dev/docs` built with Mintlify; MDX and OpenAPI kept in this repository |

### Unknown request fields: tolerant

Huma rejects unknown request-body fields by default. gorbital apps **ignore unknown fields** (tolerant reader) so older server versions keep accepting requests from newer mobile and web clients. Validation still applies to every known field. Huma has no public global switch (its registry setting is unexported), so the generator adds `` _ struct{} `json:"-" additionalProperties:"true"` `` to every request body type, and a template test checks it. Verified in the [first-run spike](../../spikes/firstrun/README.md).

### Docs asset size

Embedding Scalar added about 3.5 MB per binary, so the template embedded a pre-compressed asset served with `Content-Encoding`. Since ADR-0049 the reference is rendered by gorbital itself: about 100 KB of fonts plus its own stylesheet and script, under a Content-Security-Policy without `'unsafe-eval'` or inline styles. `/docs` stays configurable (enabled, disabled, ops-only).

### Open for v0.4

- **Non-member responses:** 403 vs 404 for organisations the caller doesn't belong to.

## Why

- Developers write only Go; docs, validation and error schemas can't drift from code.
- The layered architecture limits the dependency to one layer: replacing Huma would rewrite `delivery/` only.
- Huma is actively maintained (v2.39.1), MIT-licensed, uses the standard router and RFC 9457 errors.
- The spike showed a single direct dependency and a 6.4 MB stripped binary.

## Trade-offs

- A third-party library shapes handler signatures in every generated app.
- Error customisation relies on Huma's package-level hooks.
- If Huma is abandoned, gorbital must fork it or migrate `delivery/` layers with a codemod.

## Consequences

- gorbital pins and tests Huma versions; upgrades go through the compatibility matrix.
- `delivery/` code is part of the scaffold compatibility promise (ADR-0016).
- ADR-0022's delivery layer description refers to Huma operations.

## Security review fixes (2026-09-16)

HTTP-3: `APP_DOCS_ENABLED=false` removed `/docs` but not the OpenAPI document, which `openapi.New` always served at `/openapi.json`, `/openapi.yaml` and `/openapi-3.0.*`, listing every `/ops` operation. Docs were also on by default in the production image.

- `openapi.WithoutSpecEndpoints()` stops `New` from serving the document; `WriteSpec` still exports it. Golden apps pass it when docs are off, so `APP_DOCS_ENABLED` controls both.
- `APP_DOCS_ENABLED` defaults to `true` in development and `false` in production. A public API reference in production is an explicit `APP_DOCS_ENABLED=true`. The "ops-only" docs mode from the decision above stays open.
- The public website isn't affected: it renders the committed `api/openapi.json`, not a running app.
- `api openapi` loads configuration from `app.ExportSource` (development defaults, no environment variables), so the exported document is the same everywhere and needs no `APP_ENV`, which apps now require at start (ADR-0020).

| Check | Result |
|---|---|
| `modules/openapi` `TestWithoutSpecEndpoints` | `/openapi.json`, `.yaml`, `-3.0.json` and `-3.0.yaml` answer 404; `WriteSpec` still writes the document. Fails without the option (200) |
| Apps `TestDocs` | With `APP_DOCS_ENABLED=false`, `/docs`, `/openapi.json`, `/openapi.yaml` and `/openapi-3.0.json` answer 404 |
| Apps `TestLoadConfigSecureDefaults` | Docs on in development, off in production, and either way when set |
| Export | `env -u APP_ENV go run ./cmd/api openapi --dir api` in all three apps leaves `api/` unchanged; `TestOpenAPIUpToDate` passes |


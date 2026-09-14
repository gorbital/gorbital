# ADR-0027: API contract and documentation

**Status:** Accepted (2026-09-14) · decided by [spikes/openapi](../../spikes/openapi/README.md)

## Context

Developers must test the API immediately after `aps new`: interactive docs at `/docs`, an OpenAPI document and a Postman collection. Documentation should look like Mintlify API reference pages. All of these need one source of truth, and the choice shapes how every handler is written. The priority is a seamless developer experience: developers focus on business logic, apistock handles contract, docs and validation.

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
| Interactive docs | Own `/docs` handler serving an **embedded, pinned Scalar** asset (offline, no CDN), Mintlify-style layout with examples and try-it; configurable enabled / disabled / ops-only |
| Spec export | `my-api openapi` writes `api/openapi.json`; `aps dev` refreshes it; committed so API changes appear in pull requests |
| Breaking changes | CI compares `api/openapi.json` with the base branch and fails on breaking changes |
| Postman and AI | `api/postman_collection.json` and `api/llms.txt` generated from the exported spec (v0.5) |
| Undocumented routes | Architecture test fails if a route is registered outside Huma operations (except `/docs`, `/livez`, `/readyz`, `/.well-known/*`) |
| Time values | Generated clocks return UTC |
| Generator | `aps gen resource` and `aps gen endpoint` create input/output types, operation registration, handler and use-case stubs |
| Project docs | `apistock.dev/docs` built with Mintlify; MDX and OpenAPI kept in this repository |

### Unknown request fields: tolerant

Huma rejects unknown request-body fields by default. apistock apps **ignore unknown fields** (tolerant reader) so older server versions keep accepting requests from newer mobile and web clients. Validation still applies to every known field. The exact Huma configuration is implemented and tested in v0.1.

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
- If Huma is abandoned, apistock must fork it or migrate `delivery/` layers with a codemod.

## Consequences

- apistock pins and tests Huma versions; upgrades go through the compatibility matrix.
- `delivery/` code is part of the scaffold compatibility promise (ADR-0016).
- ADR-0022's delivery layer description refers to Huma operations.

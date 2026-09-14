# ADR-0027: API contract and documentation

**Status:** Proposed (2026-09-14) · pending the OpenAPI spike

## Context

Developers must test the API immediately after `aps new`: interactive docs at `/docs`, an OpenAPI document and a Postman collection. The maintainer wants documentation in the style of Mintlify API reference pages. All of these must come from one source of truth, and the choice shapes how every handler is written.

## Options

| Option | How it works | For | Against |
|---|---|---|---|
| **A. Spec-first** | Owned `api/openapi.yaml` → oapi-codegen strict server interfaces (derived) → handwritten delivery handlers | Contract is the source of truth; fits owned/derived model; plain `net/http`; no framework lock-in | Developers edit YAML; two steps per endpoint |
| **B. Code-first** | Typed Go handlers (Huma) generate OpenAPI | Stay in Go; fast to write | Every handler depends on a third-party framework |
| **C. Annotations** | Comments (swaggo) generate specs | Easy start | Comments drift from behaviour; not recommended |

## Proposed decision

Option A, subject to the spike.

| Topic | Decision |
|---|---|
| Source of truth | `api/openapi.yaml` (OpenAPI 3.1), owned by the app |
| Generated code | `internal/api` (types, strict server interfaces, request validation), derived, never edited |
| Recipes | Contribute endpoints with `mergeOpenAPI` (auth adds `/v1/auth/*`) |
| `aps gen resource` | Adds the spec section, regenerates `internal/api`, and creates the delivery handler stub |
| Interactive docs | **Scalar** (MIT) served at `/docs`, assets embedded in the binary (offline, no CDN), Mintlify-style layout with request/response examples, code samples and a try-it console |
| Machine-readable | `GET /openapi.json` |
| Postman | `api/postman_collection.json` generated from the spec (v0.5) |
| AI tools | `api/llms.txt` generated and served at `/llms.txt` (v0.5) |
| Contract tests | `test/e2e/contract_test.go` checks responses against the spec |
| Production | `/docs` configurable (enabled, disabled, or restricted to ops roles) |
| Project docs | `apistock.dev/docs` built with Mintlify; content (MDX + OpenAPI) kept in this repository for portability |

## Spike acceptance criteria

1. A layered module (`domain/usecase/repository/delivery`) implemented with both A and B.
2. Compare: lines of code per endpoint, compile-time safety, request validation, error mapping to problem+json, readability for newcomers, dependency weight, regeneration workflow.
3. Scalar renders both specs correctly with examples and try-it.

## Consequences

This ADR becomes Accepted, or is revised to Option B, before v0.1 implementation starts.

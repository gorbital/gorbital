# ADR-0022: Generated application layout

**Status:** Accepted (2026-09-14) · **Amended by:** ADR-0032

## Context

The generated app is what developers see every day. It must be easy to navigate, keep business rules free of HTTP and database details, scale to teams, and make its rules enforceable by tooling. Two candidates were considered: layers as files inside one package per feature, or layered sub-packages per module (the style the maintainer used successfully in a previous production API).

## Options

1. **Flat feature packages:** `handler.go`, `service.go`, `store.go` in one package.
2. **Layered modules:** `domain/`, `usecase/`, `repository/`, `delivery/` sub-packages per module.
3. **Global layers:** `handlers/`, `services/`, `repositories/` across all features.

## Decision

Option 2, with rules that avoid its common pitfalls.

```text
internal/
├── app/                     composition root: build, wire, run, shut down (only env reader)
│   └── architecture_test.go
├── modules/<module>/
│   ├── module.go            wires the module's layers; exposes Routes(), Jobs()
│   ├── domain/              entities, value objects, rules, domain errors, events (stdlib only)
│   ├── usecase/             application logic; ports.go defines the interfaces it needs
│   ├── repository/          adapters implementing ports: hand-written SQL with pgx, one file per operation (ADR-0032)
│   └── delivery/            HTTP adapter: Huma operations and request/response types ↔ use cases (ADR-0027)
└── jobs/  emails/  wellknown/
```

### Dependency rules (enforced by `architecture_test.go` in every generated app)

```text
delivery ──► usecase ──► domain
repository ─► usecase (implements ports) + domain
module.go ──► its own layers
internal/app ──► each module.go
modules never import other modules
```

| # | Rule | Prevents |
|---|---|---|
| 1 | Domain structs have no `json` or `db` tags; delivery maps to its request/response types, repository scans into domain fields or maps from unexported row structs | API contract changing when domain or schema changes |
| 2 | No package-level setters or global state; everything through constructors | Untestable, unsafe shared state |
| 3 | Ports live in `usecase/ports.go`, not in `repository/` | Use cases depending on adapters |
| 4 | No imports between modules; cross-module needs go through consumer ports wired in `internal/app`; IDs passed as values | Tangled modules |
| 5 | Transactions via a `TxManager` port; use cases never import pgx | Database leaking into business logic |
| 6 | Generated code uses clear import aliases (`projectdomain`, `projectusecase`) | Ambiguous `domain`/`usecase` imports |
| 7 | Business rules in domain constructors and methods; use cases coordinate permissions, transactions and audit | Anemic pass-through layers |
| 8 | No `platform/` folder; cross-cutting concerns come from the gorbital library | Copied infrastructure drifting per app |
| 9 | Only `internal/app` reads environment variables | Hidden configuration |
| 10 | Tests per layer: domain (unit), usecase (fake ports), repository (real PostgreSQL), delivery (`httptest` against the OpenAPI contract) | Slow, fragile test suites |

### Commands and other directories

- `cmd/api`, `cmd/worker` (optional separate job process), `cmd/migrate`, `cmd/seed`.
- `api/openapi.json` exported from code (ADR-0027), plus generated `postman_collection.json` and `llms.txt`.
- `db/migrations` (single ordered history); SQL lives next to the repository method that runs it (ADR-0032).
- `test/e2e`, `test/testutil`; `docs/` with an ADR folder for the app's own decisions.
- `compose.yaml`, `Dockerfile`, `.env.example`, `gorbital.yaml`, `gorbital.lock`, `ARCHITECTURE.md`, `AGENTS.md`.

## Why

- Package boundaries make the dependency rule enforceable, not just a convention.
- The structure is familiar to teams using DDD and layered architecture and is easy for AI tools to navigate.
- Folders name business capabilities.

## Trade-offs

- More packages and files per module; mapping code between layers (generated).
- Thin modules (`auth`, `ops`) keep all four layers for consistency even when some files are short.

## Consequences

- `ARCHITECTURE.md` in every app explains the layers, rules and how to add a module.
- A feature that outgrows one module is split into two modules, not into deeper nesting.

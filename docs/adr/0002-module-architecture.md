# ADR-002: Module architecture

**Status:** Superseded by ADR-0019 and ADR-0021 (2026-09-14)

**Context:** Modules need runtime code, installation glue, migrations and docs, from both official and third-party authors.

**Options:** (a) Go plugins / dynamic loading; (b) runtime registry with self-registering `init()`; (c) Go module + declarative recipe, wired explicitly in app code.

**Decision:** (c). Constructor + small optional interfaces at runtime; `recipe/apistock-module.yaml` for tooling; capability-based `needs/provides`.

**Why:** Go plugins are fragile (same toolchain, platform limits). `init()` registration hides wiring. Explicit wiring is readable and debuggable.

**Tradeoffs:** Adding a module edits `app.go`; no runtime hot-plugging.

**Consequences:** The manifest schema becomes a public contract that must be versioned (`apiVersion: apistock.dev/v1`).

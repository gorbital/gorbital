# ADR-001: Framework boundaries

**Status:** Proposed

**Context:** The vision covers runtime libraries, CLI, generator, modules, dashboard, GitHub and mobile. Unclear boundaries lead to runtime coupling to tooling and SaaS.

**Options:** (a) One integrated platform; (b) kit + CLI with optional, separately built control plane; (c) runtime library only, no generator.

**Decision:** (b). Runtime = core + modules only. Tooling (CLI, generator, dev console) never runs in production. Control plane and mobile are separate products that consume standard protocols.

**Why:** Keeps "delete apistock and keep shipping" true; lets each part succeed or fail independently.

**Tradeoffs:** Less "magical" integration between dashboard and app; some duplication (console vs control plane UI).

**Consequences:** Any proposal that adds a runtime import of tooling, or a private app↔control plane protocol, is rejected by default.

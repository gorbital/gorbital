# ADR-010: Dashboard architecture

**Status:** Accepted (2026-09-14), amended by ADR-0026 (ops APIs, API only) and ADR-0028 (dev console in v1.1), ADR-0066 (the Dev Portal: `orb dev` serves the local console)

**Context:** "Dashboard" mixes local dev tooling, production admin and fleet monitoring.

**Options:** In framework; optional runtime module; separate SaaS; split by purpose.

**Decision:** Split: local dev console in the CLI (Phase 5); admin features as a generated app module (post-v1); hosted control plane as a separate optional product consuming OTLP/health/admin API (only with demand).

**Why:** Each need gets the right trust boundary; no app requires a SaaS.

**Tradeoffs:** Three UIs over time instead of one; less "platform" feel early.

**Consequences:** The admin API contract must be designed as a public, authenticated, versioned API if a control plane is built.

# ADR-010: Dashboard architecture

**Status:** Proposed

**Context:** "Dashboard" mixes local dev tooling, production admin and fleet monitoring.

**Options:** In framework; optional runtime module; separate SaaS; split by purpose.

**Decision:** Split: local dev console in the CLI (Phase 5); admin features as a generated app module (post-v1); hosted control plane as a separate optional product consuming OTLP/health/admin API (only with demand).

**Why:** Each need gets the right trust boundary; no app requires a SaaS.

**Tradeoffs:** Three UIs over time instead of one; less "platform" feel early.

**Consequences:** The admin API contract must be designed as a public, authenticated, versioned API if a control plane is built.

# ADR-013: Multi-tenancy

**Status:** Superseded by ADR-0023 (2026-09-14)

**Context:** Many SaaS apps need organisations; many other apps don't. Tenancy touches every query.

**Options:** Core concept (every table has `tenant_id`); official module; leave to apps.

**Decision:** Not in core, not in v1. Later official `orgs` module: organisations, memberships, invitations, org-scoped roles, `org_id` column convention, optional Postgres RLS. Core only reserves `tenant` on the actor and a nullable `tenant_id` on audit events.

**Why:** Forcing tenancy on single-tenant apps adds complexity everywhere; retrofitting RLS into a core design is worse than adding a module.

**Tradeoffs:** Apps that later add orgs must add `org_id` to their own tables via migrations.

**Consequences:** Auth's role model must allow a scope (global vs org) without breaking changes. Design that field in v1 even though only global scope ships.

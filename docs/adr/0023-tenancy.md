# ADR-0023: Tenancy

**Status:** Accepted (2026-09-14) · **Supersedes:** ADR-0013 · **Amended by:** ADR-0033, ADR-0048 (org roles in `org_members`, 404 for non-members, invitation and personal workspace rules, `orb add orgs` moved to v0.5), ADR-0061 (the optional row-level security layer)

## Context

Some apps serve one user base; B2B SaaS apps serve many organisations whose data must be isolated. Tenancy affects every table, query, permission and audit event. Retrofitting it into a live app is expensive, and a single missed tenant filter leaks customer data.

## Options

1. Single-tenant only; tenancy left to developers.
2. Always multi-tenant.
3. A creation-time choice, with both modes generated and tested, and a guided path from single to multi.

## Decision

Option 3.

### The choice

```text
? Will different companies or teams use your app, each with their own separate data?
  ● No  — one user base (single-tenant)
  ○ Yes — organisations with members (multi-tenant)
```

- Default (`--yes`): single-tenant. Flag: `--tenancy=single|multi`.
- Stored in `gorbital.yaml` (`tenancy`, `personal_workspace`); every later command reads it.

### Single-tenant

| Topic | Decision |
|---|---|
| Ownership | Resources belong to a user (`owner_id`) or are global |
| Roles | Platform roles only |
| Routes | `/v1/<resources>` |

### Multi-tenant

| Topic | Decision |
|---|---|
| Model | Shared database and schema; tenant-owned rows have `org_id NOT NULL` |
| Personal workspace | Each user gets a personal organisation at signup (default on) |
| Membership | Users may belong to many organisations |
| Routes | Explicit: `/v1/orgs/{orgId}/...`; no implicit current-org header |
| Roles | Platform roles (staff, `/ops/*`) separate from org roles (`owner`, `admin`, `member`, custom from the permission catalog) |
| Owners | At least one owner; last owner can't leave; ownership transferable |
| Invitations | Email; hashed single-use tokens; 7-day expiry; revocable; existing or new users |
| Lifecycle | Create, rename, transfer, soft delete → purge job; all audited |

### Isolation (all four layers required)

1. **HTTP:** `orgs.RequireMember(permission)` runs before handlers.
2. **Code:** generated org-scoped repositories require an `OrgID` parameter.
3. **Database:** `UNIQUE (org_id, …)` constraints and composite foreign keys including `org_id`.
4. **Tests:** generated cross-org denial tests for every org-scoped resource.

Row-level security is an optional additional layer in v1.1 ([ADR-0061](0061-row-level-security.md): `orb add rls`). `org_id` is carried in audit events, job metadata and log attributes.

### Generator

- Multi-tenant = base recipes + the `orgs` recipe; resource templates `resource/single` and `resource/org`.
- `orb gen resource <Name> --scope=org|user|global`.
- gorbital's own library tables (audit, sessions, settings) always include a nullable `org_id`. River's job tables don't; a job's org ID is in its metadata (ADR-0033).
- Golden apps `examples/full-single` and `examples/full-multi`, both tested.

### Changing modes

| Change | Support |
|---|---|
| Single → multi | `orb add orgs`: adds the module, switches mode, new resources org-scoped; generates a data-migration skeleton and checklist for existing resources |
| Multi → single | Not supported |

### Not supported

Schema-per-tenant, database-per-tenant. Later: subdomain tenant resolution, per-org quotas and billing, per-org enterprise SSO.

## Why

The shared-schema model is the standard for SaaS; personal workspaces avoid painful later migrations; layered isolation means one mistake doesn't leak data.

## Trade-offs

- Two generation variants and golden apps to maintain.
- Multi-tenant apps carry org IDs in every route.

## Consequences

- v0.2 creates library tables with nullable `org_id` and role-scope columns so v0.4 needs no rewrite.
- Every org-scoped generated resource ships with isolation tests.

# ADR-0039: Resource module template

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0022, ADR-0023 · **Amended by:** ADR-0048 (org-scoped resources: `--scope org`, the default in multi-tenant apps), ADR-0058 (user-scoped resources check `<module>.<resource>.read`/`.write`, granted by the `user` role)

## Context

`orb gen resource` (ADR-0021, ADR-0023, ADR-0027, ADR-0032) creates a one-shot layered module, but nothing yet shows what that module looks like. `examples/full-single` has `auth` (too large and specialised to copy), `ops` (no table) and `ping` (no repository). Its generated output can't be golden-tested until a hand-written module exists, and `orb new --preset=full` is generated from the golden app.

Once generated, a resource module belongs to the app and is never upgraded (ADR-0021). Every choice made in the template spreads into every app that runs the generator and can't be fixed later with `orb upgrade`. The template has to be decided before it is written.

## Decision

`examples/full-single` gets `internal/modules/projects`, written by hand with all four layers. It is the reference for `resource/single` with `--scope=user`. The generator's golden test regenerates it from a field list and compares the result byte for byte, the same way `examples/minimal` is checked in v0.1.

### Scope

| `--scope` | Ownership | Access | Template |
|---|---|---|---|
| `user` (default in single-tenant apps) | `owner_id NOT NULL` → `auth_users.id` | Only the owner; no permission from the catalog is needed | `resource/single`, this ADR |
| `global` | none | Permissions from the catalog: `<module>.<resource>.read` / `.write`, deny by default | Later: `resource/single` with ownership removed, permissions added |
| `org` | `org_id NOT NULL` | `orgs.RequireMember(permission)`, four isolation layers | `resource/org`, v0.4 (ADR-0023) |

`auth_users.id` is a stable identity column (ADR-0015), so the foreign key doesn't break the rule that modules never import each other (ADR-0022 rule 4). The key is `ON DELETE CASCADE`: when `auth_cleanup` purges an account, its projects go with it. Until then, a soft-deleted owner can't sign in, so their projects can't be reached.

### Example fields

Only fields the generator can express from `[fields]` (allowlisted types, ADR-0021). The example uses one of each type it will support:

| Field | Type | Rule |
|---|---|---|
| `name` | `string`, required | Trimmed, 1–100 characters; unique per owner, case-insensitive (`UNIQUE (owner_id, lower(name))`) → `ErrProjectNameTaken` |
| `description` | `text` | 0–2000 characters |
| `status` | `enum(active,archived)` | Default `active`; a database `CHECK` and a domain type |
| `id`, `owner_id`, `version`, `created_at`, `updated_at` | fixed | Always present; not in `[fields]` |

IDs are text with a prefix from the resource name (`prj_`), 128 random bits, the same as auth IDs (ADR-0038).

### Layers

| Layer | Contents |
|---|---|
| `domain/` | `Project` with no tags; `NewProject` and `Project.Apply(Changes)` validate every field and return a `*ValidationError` listing each invalid field; `Status` enum; sentinel errors `ErrProjectNotFound`, `ErrProjectNameTaken`, `ErrProjectVersionConflict` |
| `usecase/` | `ports.go` (`Store` with `InTx`); `Create`, `Get`, `List`, `Update`, `Delete`; each takes the actor from the context, returns `ErrUnauthenticated` without one, and passes the owner ID to every store call |
| `repository/` | `store.go` and one file per operation (`insert_project.go`, `select_project.go`, `select_projects.go`, `update_project.go`, `delete_project.go`), `scan.go`, tests against `pgtest` (ADR-0032) |
| `delivery/` | Huma operations with input and output types (ADR-0027); nothing but mapping |
| `module.go` | `New(pool, deps)`, `Register(api)`; a nil module registers operations for OpenAPI export, like `auth` |
| App wiring | One wiring file, `internal/app/module_projects.go`, that builds the module from the shared `services` (database pool, audit recorder, logger) and maps its errors, and one `registerProjects(api, mapper, svc),` line after `//orb:anchor modules` inside `errors.Join` in `internal/app/modules.go` (ADR-0018, ADR-0021). Pagination errors (`invalid_cursor`, `invalid_sort`, `invalid_limit`) are mapped once in `routes.go` for every module |

### Owner isolation

Isolation for `--scope=user` mirrors the org isolation layers (ADR-0023):

1. **Code:** every repository method takes `ownerID`; no query reads or writes a row without `owner_id = $n`.
2. **HTTP:** someone else's project returns 404 `project_not_found`, never 403, so IDs can't be probed.
3. **Database:** uniqueness and indexes lead with `owner_id`.
4. **Tests:** generated cross-owner tests check that a second user gets 404 on get, update and delete and never sees the project in the list.

### Endpoints

| Endpoint | Result |
|---|---|
| `POST /v1/projects` | 201 with the project |
| `GET /v1/projects` | 200 `page.Result`; `limit`, `cursor`, `sort` (`created_at`, `updated_at`, `name`; default `-created_at`), `status` filter |
| `GET /v1/projects/{id}` | 200 |
| `PATCH /v1/projects/{id}` | 200; body carries the `version` it read |
| `DELETE /v1/projects/{id}` | 204 |

Error codes: `unauthenticated` (401), `forbidden` (403, an API key scoped without the permission; ADR-0058), `validation_failed` (422 with `errors[]` of `{location, message}`, as `httpx.FieldError`), `project_not_found` (404), `project_name_taken` (409), `project_version_conflict` (409), `invalid_cursor`, `invalid_sort` (400).

| Topic | Decision |
|---|---|
| Pagination | Keyset through the core `page` package; the cursor encodes the sort key and ID, which is the tiebreaker; one fixed query per allowlisted sort, never `ORDER BY` built from input (ADR-0032) |
| Updates | `PATCH` with optional fields and a required `version`; `UPDATE … WHERE id AND owner_id AND version` increments the version; zero rows after a successful get → `ErrProjectVersionConflict` |
| Deletes | Hard delete. The audit event records who deleted what and when; soft delete stays an app choice. `RowsAffected() == 0` → not found |
| Transactions | Update runs select and update in `InTx`; the other operations are one statement |
| Audit | `projects.project.created`, `.updated` (metadata: changed field names, never values), `.deleted`, recorded through `audit.Recorder` after the change, with failures logged. This is the same pattern as `auth` (ADR-0038) |
| Rate limits | None in the template; the app's global limits apply |

### Fixed versus filled from fields

| Filled from `orb gen resource <Name> [fields]` | Fixed in the template |
|---|---|
| Names (module, table, ID prefix, routes, errors, audit actions), columns, domain fields and validation, request/response fields, allowlisted sort fields (string and time fields), filters (enum fields), test values | Layer layout, ownership and isolation, pagination, versioned updates, hard delete, audit pattern, error mapping, test structure |

A migration is created with the module (`db/migrations/<timestamp>_<resources>.sql`), not a separate `orb gen migration`.

### Generator (implemented 2026-09-15)

```text
orb gen resource <Name> <field:type>... [--plural P] [--id-prefix p] [--scope user]
                 [--dry-run] [--json] [--allow-dirty] [--yes] [--no-input] [--plain]
```

| Field | Rule |
|---|---|
| `name:string` | 1–100 characters, required, sortable; `name:string:unique` is unique per owner, ignoring case |
| `notes:text` | Up to 2000 characters, optional |
| `status:enum(a,b)` | 2–20 snake_case values, the first by default; lists filter by it |

| Topic | Decision |
|---|---|
| Names | Field names are snake_case, up to 20 characters; names every resource has, query parameters and PostgreSQL reserved words are refused; generated Go names are checked for clashes. At least one string field; the first is the title the tests sort by. Up to 20 fields |
| Derived names | `Project` → package, table and route `projects`, ID prefix `prj` (first letter and next consonants), audit actions `projects.project.*`; `--plural` for irregular plurals, `--id-prefix` to choose the prefix |
| Migration version | The current UTC time, or one after the newest migration, so it always runs last |
| Output | The 20 files above and one line in `modules.go`; one-shot, not recorded in `gorbital.lock` (ADR-0021) |
| Scope | Only `user` until organisations (v0.4) and the global template exist |
| Golden test | `orb gen resource Project name:string:unique description:text 'status:enum(active,archived)'` reproduces `examples/full-single`'s projects module byte for byte; `-update` regenerates it from the templates for review |
| Generality test | A resource with several unique and enum fields and one with neither are generated into a copy of `examples/full-single`, which is vetted and runs their tests on PostgreSQL |

The example's tests use generic sample values (`Example name`, `Website`, `Docs`) so that the same test code works for any resource.

### Not in the template

Soft delete, search, bulk operations, nested resources, sharing between users, file fields, relations between generated resources (for now, add them by hand).

## Why

- Developers own and control their business code. Every layer, SQL statement and rule of a resource is in their app, where they can read and change it (for example, what happens when a project is created or how users are inserted), with nothing hidden in the library, just as `auth` is app-owned (ADR-0038).
- A real, owned example is easier to read, review and test than a template file. A golden test keeps the two in sync.
- User scope gives single-tenant apps real isolation now and practises the layers org scope requires in v0.4.
- Versioned updates and keyset pagination are hard to add after clients depend on an API; putting them in the template makes them the default.
- A 404 for others' resources avoids leaking which IDs exist.

## Trade-offs

- Every generated resource needs a signed-in user; public read-only resources need hand edits or `--scope=global`.
- Requiring `version` on `PATCH` is stricter than many clients expect.
- Audit after commit can miss an event if the audit write fails; a failure is logged, the same as `auth`.
- Hard delete loses data that soft delete would keep; the audit event records only metadata.
- Moving to organisations (`orb add orgs`) needs a data migration for user-scoped tables; the skeleton comes from ADR-0023.

## Consequences

- `examples/full-single` gains `internal/modules/projects`, its migration, cross-owner tests and an end-to-end test.
- `orb gen resource` is golden-tested against it; ADR-0022's example aliases (`projectdomain`, `projectusecase`) become real.
- Error codes and audit actions above are public API for the example app (ADR-0015); generated apps get their own names.
- Architecture open item "Example business module with its own repository" is resolved when the module lands.

## Permissions for user-scoped resources (2026-09-16, ADR-0058)

Owner isolation alone left API key scopes without effect on user-scoped resources: a key limited to reading could create, change and delete. The template now also checks a permission, as org-scoped resources always did:

| Part | Change |
|---|---|
| `usecase/<names>.go` | `PermRead` and `PermWrite` (`<module>.<resource>.read`/`.write`) for both scopes; every use case calls `ownerID(ctx, PermRead)` or `ownerID(ctx, PermWrite)` |
| `usecase/service.go` | `ownerID` checks `actor.Require` after the user and returns `ErrForbidden` |
| `domain/errors.go` | `ErrForbidden`, mapped to 403 `forbidden` in `internal/app/module_<names>.go` |
| `internal/app/module_<names>.go` | `<names>Permissions`, like org-scoped resources |
| `internal/app/permissions.go` | `orb gen resource --scope user` adds `<names>Permissions,` after `//orb:anchor user-permissions` in `userResourcePermissions`; `declarePermissions` gives them to the `user` role (`authusecase.RoleUser`), which every user holds, so sessions are unaffected |
| Tests | `TestRequiresPermission` (use cases: read-only actor reads, gets `ErrForbidden` on writes; no permissions, `ErrForbidden` on reads); the end-to-end test creates a read-scoped API key and checks 200 on reads and 403 `forbidden` on create, update and delete (both scopes) |

`TestResourceMatchesGoldenApp` reproduces both golden apps' projects modules and the permissions line for each scope; `TestGeneratedResourcesPass` generates a user-scoped resource into both golden apps, so a multi-tenant app's user role is exercised too.

# Architecture

This app follows the gorbital layered module structure. The rules below are checked by `internal/app/architecture_test.go`, so `go test ./...` fails when they are broken.

## Layout

```text
cmd/api/                 entry point: config → app → run ("api openapi" exports the spec)
cmd/migrate/             applies db/migrations, then the job queue's migrations
cmd/seed/                development seed data: an administrator, their personal workspace and example projects
db/migrations/           one ordered goose history, including gorbital module tables
internal/app/            composition root: builds, wires, runs and shuts down the app
  config.go              boot configuration: secrets and infrastructure from environment variables
  settings.go            runtime settings: tunables edited through /ops/settings
  jobs.go                one line per background job (//orb:anchor jobs)
  job_<name>.go          declares one job and its default configuration
  app.go                 construction order and lifecycle
  routes.go              API, health, docs and the middleware chain
  permissions.go         platform roles (platform_admin, ops_viewer), org roles (owner, admin, member) and one line per org-scoped resource (//orb:anchor org-permissions)
  admin.go               grant-role, revoke-role and roles commands (cmd/api)
  admin_mfa.go           reset-mfa and rotate-auth-keys commands (cmd/api)
  commands.go            database, audit, auth and orgs wiring shared by commands
  orgs_hooks.go          account hooks: personal workspaces and ownership checks on account deletion
  keys.go                AUTH_ENCRYPTION_KEYS: the keyring for two-factor authentication secrets
  passkeys.go            WEBAUTHN_*: the passkey relying party and the /.well-known files for apps
  social.go              GOOGLE_*, APPLE_*, APP_PUBLIC_URL: Google and Apple sign-in providers
  providers.go           sign-in method status: printed at start, auth-providers, /ops/auth/providers
  rate_limits.go         rate limits every instance shares (ratelimitpg), read from runtime settings
  idempotency.go         Idempotency-Key on POST and PATCH: stored responses replayed per caller (modules/idempotency)
  seed.go                development seed data (cmd/seed)
  mail.go                email delivery: Mailpit in development or the provider
  infra_mail.go          the email provider's configuration (replaced by `orb add mail`)
  infra_mail_test.go     the email provider's tests and fixtures (replaced by `orb add mail`)
  modules.go             one line per business module (//orb:anchor modules)
  module_<name>.go       wires one module: its operations and error codes
internal/jobs/<name>/    background job arguments and worker
internal/modules/<name>/ one bounded context per directory
  module.go              wires the module's layers
  domain/                business rules and errors (standard library only)
  usecase/               application logic; ports.go holds the interfaces it needs
  repository/            storage adapters implementing ports (hand-written SQL)
  delivery/              HTTP adapter: Huma operations ↔ use cases
internal/modules/auth/   sign-up, sign-in, sessions, passwords and roles: domain, usecase, repository (SQL), delivery
internal/modules/ops/    admin APIs for runtime settings, jobs, the audit log and email
internal/modules/orgs/   organisations, members, invitations and personal workspaces: domain, usecase, repository (SQL), delivery
internal/modules/projects/ example business resource that belongs to an organisation: copy it for your own
api/openapi.json         exported API contract (committed; review changes in pull requests)
api/surface.json         error codes, audit actions, permissions, settings and jobs: public names that may only grow
api/openapi.baseline.json the released /ops contract that TestOpsAPICompatible checks against
compose.yaml             PostgreSQL and Mailpit for development and tests
```

## Request flow

```text
HTTP → middleware (recover, trusted proxies, request ID, tracing, access log, security headers, CORS,
       cross-origin protection, body limit, session authentication, shared auth rate limit, idempotency keys) → delivery → usecase → domain
     ← domain errors mapped to problem+json in internal/app/module_<name>.go
```

## Configuration

- **Environment** (`config.go`): secrets and infrastructure. Changing them needs a restart.
- **Runtime settings** (`settings.go`): non-secret tunables stored in PostgreSQL, changed with `PUT /ops/settings/{key}`, applied on every instance within moments. Modules receive them as `config.Value[T]` and call `Get` each time.
- **Job definitions** (`job_<name>.go`): each job's code defaults (enabled, schedule, timeout, retries, queue), overridable with `PUT /ops/jobs/definitions/{name}`. Changing a job's code still needs a deploy.

A value is never in more than one layer, and secrets are never runtime settings.

## Background jobs

Jobs run in the API process on PostgreSQL (River). A job carries the request ID, trace and actor that enqueued it, but never their permissions; it runs as the `jobs` system actor. Add one with `orb gen job <Name>`, or copy `internal/jobs/heartbeat` and `internal/app/job_heartbeat.go` and add a line in `jobs.go`.

## Authentication

`internal/modules/auth` owns sign-up, email codes, sign-in, sessions, password reset and change, account deletion and platform roles, with all four layers: its use cases hold every flow and its repository holds the SQL for `auth_users`, `auth_sessions`, `auth_codes` and `auth_user_roles`. The gorbital auth library supplies password hashing, tokens, codes, cookies, the permission catalog and the middleware that puts the signed-in user's actor (with the permissions of their roles) in each request's context. Use cases check `actor.Can(permission)`; declare permissions and roles in `internal/app/permissions.go`.

## Organisations

`internal/modules/orgs` owns organisations (`orgs`), their members with one role each (`org_members`) and invitations (`org_invitations`), with all four layers. The gorbital orgs library supplies the organisation ID type, invitation emails and `orgs.RequireMember`.

Every use case on an organisation's data starts with `orgs.RequireMember(ctx, memberships, catalog, orgID, permission)`. It reads the caller's membership (so removing someone takes effect on their next request), answers `orgs.ErrOrgNotFound` to non-members, and returns a context whose actor has `OrgID` set and the permissions of their org role instead of their platform roles. Audit events recorded in that context carry the organisation.

The two catalogs stay apart: platform roles (`declarePermissions`) grant `/ops` permissions, org roles (`declareOrgPermissions`) grant permissions inside one organisation. The auth module doesn't import the orgs module: `orgs_hooks.go` connects them through the auth use cases' `AccountHooks`, so new accounts get a personal workspace and account deletion checks ownership. Hooks don't share a transaction; creating the personal workspace is idempotent and runs again when someone lists their organisations.

Deleted organisations are soft deleted, restorable until `orgs.deleted_org_retention` ends, then removed by the `orgs_purge` job; foreign keys with `ON DELETE CASCADE` remove every org-scoped row.

## Business resources

`internal/modules/projects` is exactly what `orb gen resource Project name:string:unique description:text 'status:enum(active,archived)' --scope org` creates. Generate your own resources the same way, or copy it. All of it is your code: change any rule, query or response.

- **Ownership:** every project has an `org_id` and a `created_by`. Every use case calls `orgs.RequireMember` with `projects.project.read` or `.write`, every repository method takes the organisation ID, and a project of another organisation returns 404 `project_not_found`, so IDs can't be probed. `UNIQUE (org_id, id)` lets other org-scoped tables reference a project only within the same organisation. Purging an organisation deletes its projects.
- **Lists:** `GET /v1/orgs/{orgId}/projects` uses keyset pagination through `gorbital.dev/page`: `limit`, an opaque `cursor`, and `sort` by one allowlisted field, with one fixed query per sort in `repository/select_projects.go`.
- **Updates:** `PATCH` sends the `version` it read; a stale version returns 409 `project_version_conflict` instead of overwriting someone else's change.
- **Audit:** `projects.project.created`, `.updated` (changed field names only) and `.deleted`.
- **Tests:** domain rules, repository methods on real PostgreSQL, use cases including non-members, roles without the permission and cross-organisation access, and an end-to-end HTTP test in `internal/app/projects_test.go`.

## Email

Modules send email through `mailer`, a `mail.Sender` built in `app.go`: it fills the sender from the `mail.*` runtime settings and queues the message; the mail worker delivers it with retries and idempotency. `mail.go` sends to Mailpit in development (`MAIL_DELIVERY`) or to the provider in `infra_mail.go`. The provider's secrets are environment variables in the `# orb:begin mail` block of `.env.example`. `orb add mail` replaces `infra_mail.go`, `infra_mail_test.go` and that block to switch between Resend and SMTP; don't edit them by hand.

## Audit log

Modules record who did what through `audit.Recorder`; `app.go` passes the `auditpg` store, which writes the append-only `audit_events` table and redacts sensitive metadata keys. `GET /ops/audit` lists events. To commit an event with the change it describes, call `RecordTx` with the same transaction.

## Rules

1. `domain/` imports only the standard library. Domain types have no struct tags.
2. `usecase/` never imports `delivery/`, `repository/` or HTTP packages.
3. `delivery/` calls use cases, never repositories. Only `delivery/`, `module.go` and `internal/app` import the HTTP framework (Huma).
4. Modules never import other modules. Cross-module needs go through interfaces wired in `internal/app`.
5. Only `internal/app` reads environment variables.
6. Handlers return domain errors unchanged; each `module_<name>.go` maps them to an HTTP status and a stable error code.
7. Request body types tolerate unknown fields (`additionalProperties:"true"`), so older servers accept newer clients.
8. Migrations never run at startup; run `cmd/migrate` before starting a new version.

## Adding an endpoint

1. Add the rule and its error to `domain/`.
2. Add the use case to `usecase/`.
3. Add input/output types and a `huma.Register` call in `delivery/`.
4. Map new domain errors in `internal/app/module_<name>.go`.
5. Run `go test ./...`, then `go run ./cmd/api openapi --dir api`.

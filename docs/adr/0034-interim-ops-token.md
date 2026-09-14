# ADR-0034: Interim ops token

**Status:** Superseded by ADR-0038 (2026-09-15): `OPS_TOKEN` is removed; `/ops/*` uses signed-in sessions and platform roles · **Amends:** ADR-0026

## Context

ADR-0026 protects `/ops/*` with platform roles (`ops.*` permissions) and enrolled 2FA. The runtime settings (ADR-0031) and jobs (ADR-0033) admin APIs were built before `modules/auth`, users and roles exist. Without protection they can't be exposed at all; waiting for authentication would delay the admin panel the Full preset promises.

## Options

1. Build authentication first, then the admin APIs.
2. Serve `/ops/*` only on a loopback listener, with no credentials.
3. Protect `/ops/*` with a single bearer token from the environment until authentication exists.

## Decision

Option 3, in `examples/full-single` and the Full preset it defines.

| Topic | Decision |
|---|---|
| Configuration | `OPS_TOKEN` (or `OPS_TOKEN_FILE`) loaded as `config.Secret`; at least 32 characters, otherwise startup fails without printing the value |
| Disabled | Without `OPS_TOKEN`, every `/ops/*` request returns 404 `not_found`, so the routes are invisible |
| Request | `Authorization: Bearer <token>`, compared in constant time |
| Failure | 401 `unauthenticated` with `WWW-Authenticate: Bearer realm="ops"`; public routes are unaffected |
| Identity | A valid token acts as actor `{kind: service, id: "ops-token"}` holding every `ops.*` permission, so settings and job history, audit events and job metadata record the change |
| Authorisation | Use cases still check `ops.settings.read`, `ops.settings.write`, `ops.jobs.read`, `ops.jobs.write` and `ops.jobs.run`; replacing the token changes only the middleware |
| Documentation | The OpenAPI document declares a bearer security scheme on every ops operation |
| Location | `internal/app/ops_auth.go` (middleware), `internal/modules/ops/domain/permissions.go` (permissions) |

## Why

- The admin APIs can be used and tested end to end now.
- Permission checks already sit where they will stay, so the switch to real authentication is small and reviewable.

## Trade-offs

- One shared secret: changes are attributed to `ops-token`, not a person. Reasons are required for risky changes (disabling or rescheduling jobs, security-relevant settings) to compensate.
- No 2FA and no per-person revocation; rotating the token needs a restart.
- Operators should additionally restrict `/ops/*` at the network edge in production.

## Consequences

- Threat model row 18 is mitigated only partially until authentication ships.
- When `modules/auth` lands: `opsAuth` is replaced by session authentication plus a permission check per operation, `OPS_TOKEN` is removed with an upgrade note, and this ADR is marked Superseded.

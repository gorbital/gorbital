# Changelog

Notable changes to the gorbital library, the `orb` CLI and generated apps. The library modules and `orb` are versioned together. What existing apps must do for each release is in the [upgrade notes](docs/guides/upgrade-notes.md); why things changed is in the [decision records](docs/adr/README.md).

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Everything is v0 until 1.0 ([ADR-0015](docs/adr/0015-public-api-and-stability-tiers.md)).

## Unreleased (v1.0)

### Added

- Rate limits shared by every instance, counted in PostgreSQL by `modules/ratelimitpg`, with an in-memory fallback; sign-in limits are runtime settings ([ADR-0052](docs/adr/0052-shared-rate-limits.md)).
- `APP_TRUSTED_PROXIES` and `httpx.TrustedProxies`: client IPs from `X-Forwarded-For` only through configured proxies.
- Apple tokens revoked by the `auth_revoke_tokens` job, with retries.
- Governance, contribution guide, code owners and issue and pull request templates ([ADR-0055](docs/adr/0055-governance-and-contribution.md)).
- API freeze checks ([ADR-0054](docs/adr/0054-api-freeze-and-scaffold-compatibility.md)): `Stability:` markers on every package, API listings in `api/*.txt` checked by `internal/tools/apicheck`, `api/surface.json` and `api/openapi.baseline.json` with their tests in Full apps, `openapi.CheckCompatible`, `gorelease` on library tags, and the scaffold compatibility check.
- `"schemaVersion": 1` in every `orb --json` output; `orb version --json`.
- `settings.(*Registry).Keys` and `jobs.(*Definitions).Names`.

### Fixed

- `orb`'s template generator no longer copies a golden app's local `.env` file into templates.

## v0.5.0 (2026-09-15)

### Added

- `orb upgrade`: 3-way merges of template changes on a branch, with the merge base rebuilt from the recorded release and proven against `gorbital.lock` v2 ([ADR-0050](docs/adr/0050-upgrades-and-adding-features.md)).
- `orb add orgs`: turns a single-tenant Full app multi-tenant, with migrations that move existing data.
- `orb doctor`.
- Ops endpoints: `GET /ops/system`, `/ops/audit/stats`, `/ops/jobs/overview`, `/ops/retention`; retention settings and the daily `retention` job; maintenance mode ([ADR-0051](docs/adr/0051-operations-v0-5.md)).
- `api/postman_collection.json` and `api/llms.txt`, generated with the OpenAPI document.

### Changed

- Audit events older than 365 days are deleted by default.

## v0.4.0 (2026-09-15)

### Added

- Multi-tenant organisations: `modules/orgs`, `orb new --tenancy multi`, personal workspaces, memberships and org roles, invitations, soft delete, restore and purge ([ADR-0048](docs/adr/0048-organisations-v0-4.md)).
- `orb gen resource --scope org` with generated cross-organisation denial tests.

## v0.3.0 (2026-09-15)

### Added

- Two-factor authentication with authenticator apps and recovery codes; 2FA required for ops roles ([ADR-0043](docs/adr/0043-two-factor-authentication.md)).
- Passkeys, for passwordless sign-in and as a second factor ([ADR-0044](docs/adr/0044-passkeys.md)).
- Google and Apple sign-in, web and native ([ADR-0046](docs/adr/0046-google-and-apple-sign-in.md)); sign-in provider setup and `AUTH_PROVIDERS.md` ([ADR-0045](docs/adr/0045-sign-in-provider-setup.md)).

## v0.2.0 (2026-09-15)

### Added

- The Full preset: PostgreSQL, runtime settings, background jobs on River, email through Resend or SMTP, audit log, email and password authentication, platform roles and permissions, ops APIs, seed data.
- `orb dev` with Docker services, `orb gen resource`, `orb gen job`, `orb gen migration`, `orb add mail`.

## v0.1.0 (2026-09-14)

### Added

- Core packages, OpenAPI with Huma, OpenTelemetry, the Minimal preset, `orb new`, `orb dev`, `orb version`, project CI and the signed release workflow.

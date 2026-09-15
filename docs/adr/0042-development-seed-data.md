# ADR-0042: Development seed data

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0028

## Context

ADR-0028 says a Full app's seed creates a default administrator on first run, with credentials "printed once and stored in `.env`". Nothing implements seed yet: today a new developer registers, reads a code in Mailpit, verifies, runs `go run ./cmd/api grant-role`, then logs in, before `/ops/*` answers. `aps dev` with Docker (ADR-0028) is meant to make the first run work without those steps.

Storing the password in `.env` has costs:

| Problem | Why it matters |
|---|---|
| A login password in plain text on disk | `.env` is copied into shells (`set -a; . ./.env`), containers and backups, and read by every tool that loads it |
| It isn't configuration | `.env` holds the app's secrets and infrastructure (ADR-0031); an account's password isn't read by the app |
| It goes stale | After a password change or reset, `.env` shows a password that no longer works |

## Options

| | 1. Password stored in `.env` (ADR-0028 as written) | 2. Random password printed once, never stored | 3. No administrator |
|---|---|---|---|
| First login | Read `.env` | Copy from the first `aps dev` output | Register, verify, `grant-role` |
| Password on disk | Yes | No | No |
| Lost password | Read `.env` (if not stale) | Reset through Mailpit, or reset the database | n/a |
| Example data | Possible | Possible | Possible, owned by nobody |

## Decision

Option 2.

| Topic | Decision |
|---|---|
| Command | `cmd/seed` in the Full preset, calling `app.Seed` in `internal/app/seed.go`; `go run ./cmd/seed [-email address]` |
| When it runs | `aps dev` runs it after migrations on every start; by hand after `cmd/migrate` |
| Production | Refuses when `APP_ENV=production` |
| Idempotent | When the administrator's email already exists, nothing changes and no password is printed |
| Administrator | `admin@example.com` by default, verified, with `platform_admin`; created through the auth use cases, so the password policy, argon2id hashing and audit events apply, with the system actor `seed` |
| Password | `crypto/rand.Text()`: 26 characters, 130 bits; printed once to standard output; never written to a file, the database in plain text or the logs |
| Lost password | `POST /v1/auth/password/forgot`, then the code from Mailpit; or `docker compose down -v` and start again |
| Example data | Three projects owned by the administrator (two active, one archived), created through the projects use cases, only when the administrator is created, so deleted examples stay deleted |
| Partial failure | Seed stops with the error; a later run sees the administrator and doesn't retry the examples. Reset the database to start clean |
| Ownership | `seed.go` is app code: change the administrator, add data for new modules, or delete it |

Command-line wiring shared with `grant-role` (database, audit store, auth use cases) lives in `internal/app/commands.go`.

## Why

- The first `aps dev` ends with a working administrator: `/docs`, `/ops/*` and the example resource can be tried at once.
- No login password is left on disk, and nothing in `.env` goes stale.
- Going through the use cases keeps seed data valid and audited, like data created through the API.

## Trade-offs

- A password missed in the output means a reset through Mailpit or a database reset.
- `admin@example.com` is a known address in every development database; it only exists where seed ran, never in production.
- Seed runs on every `aps dev` start; with the administrator present it is one query.

## Consequences

- ADR-0028's "Default admin" row now reads: created by seed on first run; the random password is printed once and never stored.
- `aps dev` (ADR-0028) runs migrations and seed before starting the app.
- The Full preset gains `cmd/seed`, `internal/app/seed.go`, `internal/app/commands.go` and a seed test; `aps new` no longer tells developers to grant a role before using `/ops/*`.

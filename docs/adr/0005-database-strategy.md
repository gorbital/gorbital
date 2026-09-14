# ADR-005: Database strategy

**Status:** Accepted (2026-09-14) · **Amended by:** ADR-0032 (hand-written SQL replaces sqlc)

**Context:** Modules need persistence and migrations; apps need productive data access.

**Options:** Multi-DB abstraction + ORM (GORM/Ent); Postgres-only with sqlc; declarative schema (Atlas).

**Decision:** PostgreSQL only; pgx with hand-written SQL in repositories (ADR-0032; originally sqlc); goose; module migrations copied into the app's single history; table prefixes per module.

**Why:** Enables transactional jobs and audit, reviewable schema changes, no ORM lock-in for apps, no commercial-tier dependency.

**Tradeoffs:** No MySQL/SQLite users in v1; dynamic queries need hand-written SQL.

**Consequences:** Released migrations are immutable forever; module schema changes are forward-only.

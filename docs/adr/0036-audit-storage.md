# ADR-0036: Audit storage

**Status:** Accepted (2026-09-14) · **Amends:** ADR-0026, ADR-0029

## Context

Core `audit` defines `audit.Event` and `audit.Recorder` (ADR-0019). Settings and jobs already record events (`settings.value.changed`, `jobs.definition.changed`, …), but until now `examples/full-single` only logged them with `audit.LogRecorder`, so v0.2's definition of done ("a setting changed through `/ops/settings` … appears in history and the audit log") couldn't be met. Authentication, next in v0.2, records security events (logins, sessions, password changes) that must be queryable, bounded in what they store, and hard to alter.

Questions to settle:

- Table shape, indexes and immutability.
- Whether events commit with the change they describe.
- What happens to sensitive data in metadata (threat model row 19).
- Which query APIs ship now, and which stay in v0.5.

## Options for sensitive metadata

1. Trust callers: store metadata as given.
2. A per-action allowlist of metadata keys, declared by each module.
3. Store what callers give, with a recorder-side safety net: redact values under sensitive key names, strip characters PostgreSQL rejects, and bound size.

Option 3. Metadata keys are chosen in module code and reviewed there; a registry of allowed keys per action adds ceremony to every new event without catching a secret put under an innocent key. The safety net catches the common mistakes (a `password`, `token` or `code` field copied from a request) without silently dropping events.

## Decision

`gorbital.dev/modules/auditpg` stores events in PostgreSQL. `*auditpg.Store` implements `audit.Recorder`, and apps pass it to every module that records events.

### Table

| Topic | Decision |
|---|---|
| Table | `audit_events`: `id` (identity), `occurred_at`, `recorded_at`, `actor_kind`, `actor_id`, `actor_label`, `action`, `resource_type`, `resource_id`, `outcome`, nullable `org_id`, `request_id`, `trace_id`, `ip` (`inet`), `user_agent`, `metadata` (`jsonb`) |
| Constraints | `action` matches the dotted action pattern; `outcome` is `success`, `failure` or `denied` |
| Immutability | A `BEFORE UPDATE` trigger rejects every update. Deletes stay possible for retention (v0.5), which removes whole rows by age |
| Indexes | `occurred_at`; `(action, id DESC)`; partial `(actor_id, id DESC)`, `(resource_type, resource_id, id DESC)`, `(org_id, id DESC)`, `(request_id)` |
| Partitioning | Not in v0.2. Time partitioning is reconsidered with retention if volumes need it |
| Migrations | `auditpg.Migrations`, copied into the app's `db/migrations` like other modules (ADR-0005) |

### Recording

| Topic | Decision |
|---|---|
| `Record(ctx, e)` | Fills empty actor, org, request and trace fields from `ctx` (`audit.FromContext`), validates, sets `OccurredAt` to now when zero, and inserts |
| `RecordTx(ctx, db, e)` | Same, through a `postgres.DBTX`, so an event commits or rolls back with its change. Modules that own a transaction (auth) use it; settings and jobs record after commit and log a failed write, as ADR-0031 and ADR-0033 decided |
| Invalid events | Rejected with an error: malformed action, unknown outcome, action over 200 characters |
| Text | Invalid UTF-8 replaced, NUL bytes removed, lengths bounded (IDs 200, user agent 512, request ID 128, trace ID 64); invalid IP addresses stored as NULL |
| Metadata redaction | Values under keys matching `password`, `passwd`, `passphrase`, `secret`, `token`, `cookie`, `authorization`, `api_key`, `apikey`, `private_key`, `otp`, `credential(s)`, `recovery_code`, `verification_code` become `"[REDACTED]"`, at any depth, including struct fields. Keys match as whole snake_case segments (`refresh_token`, `accessToken`, not `tokenizer`). `WithRedactedKeys` adds names |
| Metadata bounds | Above 16 KiB of JSON after redaction (`WithMaxMetadataBytes`), metadata is replaced with `{"metadata_dropped": "too_large"}`; unencodable metadata with `{"metadata_dropped": "not_json"}`. The event is still recorded |
| IP and user agent | Stored when the caller sets them. No core middleware puts them in the context yet; authentication fills them for its events |

### Queries and ops APIs

| Topic | Decision |
|---|---|
| `List(ctx, Filter)` | Newest first by `id`. Filters: actor kind and ID, exact action or action prefix, resource type and ID, org, outcome, request ID, `occurred_at` range. Limit 1–100 (default 50); opaque cursor; `ErrInvalidFilter`, `ErrInvalidCursor` |
| `Get(ctx, id)` | One event, or `ErrEventNotFound` |
| SQL | Hand-written (ADR-0032); `List` appends one fixed condition per set filter so each query uses its index |
| `GET /ops/audit`, `GET /ops/audit/{id}` | **Moved from v0.5 to v0.2** in the generated `ops` module, with permission `ops.audit.read`, so operators can see the events settings and jobs changes already record |
| Still v0.5 | `GET /ops/audit/stats`, retention policies and purge jobs, 2FA on ops routes |
| Error codes | `audit_event_not_found` (404), `invalid_audit_filter` (422), and the existing `invalid_cursor` (400) |

## Why

- Events stored next to the data they describe can commit with it, and need no extra infrastructure.
- An append-only table with a trigger makes accidental or casual tampering fail loudly; database superusers remain trusted, which is recorded as residual risk.
- Redaction in the recorder protects every module, including community modules, from the most common leak.

## Trade-offs

- Audit writes add load to the primary database; high-volume apps may later need partitioning or export.
- Name-based redaction can miss secrets under unrelated keys, and can redact harmless values under matching keys (a `token_count`).
- The trigger doesn't stop `TRUNCATE` or a superuser; tamper evidence (hash chains, external export) is not in scope.
- IP addresses and user agents are personal data; they are kept because security investigations need them, and retention (v0.5) bounds how long.

## Consequences

- `examples/full-single` records every audit event in `audit_events` instead of logging it, and serves `/ops/audit`.
- Audit action names, permission `ops.audit.read`, and the new error codes are public API (ADR-0015).
- Threat model row 19 gains the audit metadata controls.

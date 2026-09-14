# ADR-0031: Runtime settings

**Status:** Accepted (2026-09-14) · **Amends:** ADR-0020, ADR-0026

## Context

ADR-0020 made environment variables the only configuration source, so changing a verification-code expiry, a rate limit or an email sender name means editing env and redeploying. The maintainer's previous production API split configuration into two layers: secrets and infrastructure in `.env`, and non-secret tunables in a PostgreSQL `system_settings` table edited through dashboard APIs, with an audit trail and an in-memory cache. Operators changed behaviour in seconds without touching env. ADR-0026 planned a similar configuration center for v1.1; apps need it from the first release that has a database.

Lessons from that implementation:

- Catalog metadata (type, validation rules, description) was copied into every database row and seeded, so changing a rule needed a migration and rows drifted from code.
- Settings were read by string key (`GetInt(ctx, "otp_ttl_minutes")`), so a typo compiled and returned zero.
- A change updated only the instance that received the request; other instances served stale values until a manual reload.

## Options

1. Environment only (ADR-0020 as written).
2. Copy the previous design: catalog rows seeded into the database, string-key getters.
3. Settings declared in Go as typed handles; the database stores only changed values; changes propagate to every instance; edited through `/ops/settings`.

## Decision

Option 3, shipped in v0.2 as `apistock.dev/modules/settings`.

### Two layers

| Layer | Holds | Changed by | Takes effect |
|---|---|---|---|
| **Environment** (ADR-0020) | Secrets, credentials, database URL, listen addresses, anything needed before the database connects | Env or `*_FILE`, then redeploy | Restart |
| **Runtime settings** (this ADR) | Non-secret tunables: expiries, limits, sender names, frontend URLs, retention days, maintenance mode | `PUT /ops/settings/{key}` | Immediately on every instance (or at restart, if the setting says so) |

Rule of thumb: if it is secret or infrastructure, it goes in env; if an operator should change it without a deploy, it is a runtime setting. A setting is never also read from env.

### Declaring settings

Settings are declared in Go, in the wiring file of the feature that uses them (`internal/app/module_auth.go`). The declaration is the whole catalog: key, type, default, bounds, description and flags. Nothing is seeded.

```go
// Shape only.
codeTTL := settings.Duration(reg, "auth.verification_code_ttl", 15*time.Minute,
    settings.Describe("How long email verification codes stay valid."),
    settings.Range(5*time.Minute, time.Hour),
    settings.ReasonRequired(),
)

authService, err := auth.New(db, mailer, recorder, auth.WithVerificationCodeTTL(codeTTL))
```

| Topic | Decision |
|---|---|
| Keys | Dotted lowercase, namespaced by feature (`auth.verification_code_ttl`), same pattern as audit actions |
| Types | `Bool`, `Int`, `Float`, `String`, `Duration`, `StringList`, `Enum`; no free-form JSON |
| Validation | Declared in code (`Range`, `OneOf`, `MaxLen`, `URL`, `Email`, `Validate(func)`); checked on write and on load |
| Flags | `ReasonRequired` (change needs a reason), `RestartRequired` (read once at startup), `Group` (for listing) |
| Secrets | Not representable: there is no secret type, and `config.Secret` is rejected at declaration |
| Duplicates | Declaring a key twice panics at startup |

### Reading settings

- Each handle implements a new core contract, `config.Value[T]` (`Get(ctx) T`), plus `config.Static(v)` for fixed values. Library modules accept `config.Value[T]` for options documented as live, and plain values for everything else. Modules never import `modules/settings` (ADR-0019).
- `Get` reads an in-memory snapshot (atomic pointer swap): no database call, no lock contention.
- Value resolution: stored valid value → declared default. A stored value that no longer passes validation is ignored, logged once, and flagged in the API.
- Library modules still validate live values against their own safe limits and fall back to their default if a value is out of range.

### Storage and propagation

| Topic | Decision |
|---|---|
| Tables | `settings_values` (key, value `jsonb`, version, updated_at, updated_by, nullable `org_id` reserved for per-org settings) and `settings_history` (key, old value, new value, actor, reason, request ID, time) |
| Rows | Only changed settings have rows; resetting to the default stores a NULL value, so rows are never deleted and versions never repeat (a stale version can't match after a reset) |
| Write | One transaction: version check (optimistic concurrency), upsert or delete, history row, `pg_notify('apistock_settings', key)`. The audit event `settings.value.changed` is recorded through `audit.Recorder` |
| Propagation | `settings.Store` is an `app.Runner` holding a `LISTEN` connection; on notify it reloads that key; on reconnect it reloads everything; a full resync every 5 minutes covers missed notifications |
| Startup | `settings.NewStore` loads all values before the app serves traffic; a load failure fails startup |
| Unknown keys | Rows for keys no longer declared are kept, ignored, and listed by `aps doctor` |

### API (generated `internal/modules/settings`, Full preset)

| Endpoint | Purpose |
|---|---|
| `GET /ops/settings` | List declared settings with value, default, `modified`, `applies` (`live` or `restart`), `reason_required`, validation summary; filter by group, text, modified |
| `GET /ops/settings/{key}` | One setting |
| `PUT /ops/settings/{key}` | `{value, reason, version}`; 409 on a stale version, 422 on invalid value or missing reason |
| `DELETE /ops/settings/{key}` | Reset to the default (`reason` in the body when required) |
| `GET /ops/settings/{key}/history` | Change history, cursor pagination |

Permissions `ops.settings.read` and `ops.settings.write`. The ops 2FA requirement (ADR-0026) applies from v0.3, when TOTP ships. There is still no admin web UI: a team's dashboard calls these endpoints.

### Initial settings (v0.2)

Each module's design fixes its exact list; expected candidates are auth code and reset expiries, session idle and absolute timeouts, auth rate limits, mail sender name and address, frontend base URL for email links, and (from v0.5) retention days and maintenance mode.

### Not included

Per-org settings (column reserved), feature flags and percentage rollouts (v1.1), env overrides that lock a setting, bulk import/export, secrets of any kind.

## Why

- Operators change behaviour without redeploying, and every change has an actor, a reason and history.
- Typed handles make a wrong key or type a compile error, and "go to definition" shows the default and bounds.
- Declarations in code mean no seeding, no metadata drift and no migration when a rule changes.
- Every instance converges within milliseconds, without an extra service.

## Trade-offs

- A second configuration source to explain; the two-layer rule must be documented in every generated app.
- A `LISTEN` connection per instance, which does not survive transaction-mode connection poolers (PgBouncer); the 5-minute resync is the fallback.
- Validation rules live in app code, so an app can loosen a bound; library modules keep their own hard limits.

## Consequences

- ADR-0020's "runtime-mutable configuration is not supported" is replaced by this ADR; env remains the only source for secrets and infrastructure.
- ADR-0026's v1.1 configuration center moves to v0.2; maintenance mode becomes a runtime setting instead of an env flag.
- `config.Value[T]` enters core, consumed by `auth`, `jobs`, `auditpg` and `mail` providers.
- Setting keys are public API, additive only (ADR-0015).
- Generated apps include a "Configuration" section in `ARCHITECTURE.md` with the two-layer rule.
- The Full preset includes settings; in Custom it is a checkbox that requires PostgreSQL.

## v0.2 implementation notes (2026-09-14)

- Library: `settings.NewRegistry`; declarations `Bool`, `Int`, `Float`, `String`, `Enum`, `Duration`, `StringList`; options `Describe`, `Group`, `ReasonRequired`, `RestartRequired`, `Range`, `OneOf`, `MaxLen`, `MaxItems`, `URL`, `Email`, `Validate`. Invalid declarations panic at startup.
- `settings.NewStore(ctx, pool, reg, recorder)` loads values and freezes the registry; `Store.Run` is the listener runner; `Store.List`, `Get`, `Set`, `Reset`, `History` and `UnknownKeys` back the `/ops/settings` endpoints.
- Changes require an authenticated actor in the context (`ErrActorRequired`); every change is one `settings.value.changed` audit event with `version`, `reset` and `reason` metadata, recorded after commit (a failed audit write is logged, not returned).
- Setting a value equal to the current one, or resetting a setting already at its default, changes nothing.
- The listener uses a dedicated connection, reloads everything after each (re)connect, and reconnects with backoff up to 30 seconds.
- Migrations ship embedded as `settings.Migrations`. Tests use `modules/postgres/pgtest` (a test-only dependency).
- The `/ops/settings` HTTP endpoints are implemented in `examples/full-single` (`internal/modules/ops`), protected by the interim ops token (ADR-0034). Error codes: `setting_not_found` (404), `setting_version_conflict` (409), `setting_reason_required` (422), `invalid_setting_value` (422). See [the runtime settings guide](../guides/runtime-settings.md) and [the ops API reference](../guides/ops-api.md).

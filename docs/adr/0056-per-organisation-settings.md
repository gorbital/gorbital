# ADR-0056: Per-organisation settings

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0031, ADR-0048

## Context

v1.1 item 1 (roadmap): `modules/settings` uses its reserved `org_id`, settings are declared as org-overridable, `Setting.Get` resolves the organisation from the context, organisation admins change their values under `/v1/orgs/{orgId}/settings`, and `/ops/settings` stays compatible. Today:

| Area | Today | Evidence |
|---|---|---|
| Storage | `settings_values` and `settings_history` have a nullable `org_id` "reserved for per-organisation settings"; unique indexes already separate platform rows (`key WHERE org_id IS NULL`) from organisation rows (`(org_id, key) WHERE org_id IS NOT NULL`) | `modules/settings/migrations/00001_settings.sql` |
| Queries | Every statement hard-codes `org_id IS NULL`; inserts never set it | `select_value.go`, `upsert_value.go`, `history.go` |
| Reads | `Setting.Get(ctx)` ignores ctx and reads an atomic snapshot: no lock, no database, no error; called on hot paths (every sign-in, every invitation, every ping) | `settings.go` |
| Propagation | `NOTIFY gorbital_settings` with the key as payload; listeners reload that key; full reload after reconnect and every 5 minutes | `listen.go`, `store.go` |
| Organisation in the context | `orgs.RequireMember` (through `Authorize`) returns a context whose actor has `OrgID` set; every org-scoped use case already reads settings after it | `modules/orgs/member.go`, `internal/modules/orgs/usecase/invitations.go` |
| Audit | `audit.FromContext` fills the event's `OrgID` from the actor when the event has none | `audit/audit.go` |
| Organisation purge | `DELETE FROM orgs` removes org-scoped rows through `ON DELETE CASCADE` foreign keys; the library tables have no foreign key (a library can't reference an app's table) | `internal/modules/orgs/repository/delete_org.go`, ADR-0048 |
| Candidate settings | 26 settings in full-multi; all but `example.ping_message`, `maintenance.message` and `maintenance.retry_after` are security-relevant or per user; `orgs.invitation_ttl` is read inside organisation use cases after `RequireMember` | `internal/app/settings.go` |
| `orb add orgs` | Copies only the organisations migration and a conversion; every other multi-tenant-only migration is dropped because an existing database ran the single-tenant ones | `cli/internal/cli/add_orgs.go` |

Constraints: modules never import each other and `modules/settings` can't know about organisations (ADR-0019); setting keys, permissions and error codes are additive public API (ADR-0015, ADR-0054); `/ops` changes must pass the baseline check; organisations get the four isolation layers of ADR-0048.

## Options

| Question | Option | Verdict |
|---|---|---|
| Which settings accept organisation values | Every setting | Rejected: an organisation admin could loosen sign-in, rate limits, retention or email senders for their members |
| | **Opt-in per declaration: `settings.OrgOverridable()`** | **Chosen**: the declaration stays the whole catalog (ADR-0031); apps guard the choice with a test over the registry |
| Where `Get` finds organisation values | A database read on a miss, cached in an LRU | Rejected: `Get` has no error to report a failed read, it runs inside transactions and hot paths, and "`Get` never touches the database" is a documented property. A miss storm (many organisations, first request each) would become a query storm |
| | A copy-on-write map of every organisation, swapped whole | Rejected: each change copies an entry per organisation on every instance |
| | **Every organisation value in memory, one immutable short slice per organisation in a `sync.Map`, replaced per change** | **Chosen**: `Get` stays lock-free and allocation-free; memory is bounded by rows that exist (organisations × overridable settings they changed), measured below |
| Notification payload | A new channel for organisation values | Rejected: a second `LISTEN` per instance |
| | **Same channel, payload `key` or `key org_id`** | **Chosen**: an instance running the previous release looks up `"key org_id"` as a key, finds nothing and ignores it, so rolling deploys are safe |
| Store API | An organisation handle, `store.Org(id).Set(…)` | Rejected: consumer-owned interfaces in apps would need a second type |
| | **Flat methods: `ListForOrg`, `GetForOrg`, `SetForOrg`, `ResetForOrg`, `HistoryForOrg`, `Overrides`** | **Chosen**: additive, and an app's port lists exactly what it uses |
| Removing values of purged organisations | A library `DeleteOrg` called by the purge | Rejected: not in the purge's transaction, so a crash leaves orphans nothing retries |
| | **A multi-tenant app migration adding `ON DELETE CASCADE` foreign keys to `orgs`** | **Chosen**: the same mechanism as every org-scoped table; a full reload then drops the values from memory |
| Who changes organisation values | Operators through `/ops` | Rejected as the only path: operators already have platform values; the roadmap gives organisation admins the endpoints |
| | **Owners and admins (`orgs.settings.write`), read by every member (`orgs.settings.read`); operators list them read-only (`GET /ops/settings/{key}/overrides`)** | **Chosen** |
| Where the endpoints live | A new app module | Rejected: it would need the organisation catalog and memberships wired like a resource, for five routes about the organisation itself |
| | **The generated `orgs` module** | **Chosen**: its permissions are already `orgs.*`, and every file under it may differ between the golden apps |
| Example setting | `example.ping_message` | Rejected: `GET /v1/ping` has no organisation, so an override would never be read without a new org route |
| | **`orgs.invitation_ttl`** | **Chosen**: already read after `RequireMember`, so the override works with no use-case change; an organisation choosing shorter or longer links within the platform's 1–30 days is a real preference, not a security bypass (the platform keeps the bounds and reason requirement) |

## Decision

### Library (`modules/settings`)

| Topic | Decision |
|---|---|
| Declaration | `settings.OrgOverridable()`; combining it with `RestartRequired` panics (organisation values are live) |
| Resolution | `Setting.Get(ctx)` for an overridable setting: when `actor.From(ctx)` has an `OrgID` and that organisation has a valid value, that value; otherwise the platform value as before. Other settings don't look at ctx |
| Validation | The declared validation applies to organisation values; a stored value that fails it is ignored (platform value applies), logged, and flagged `InvalidStoredValue` |
| Writes | `SetForOrg(ctx, orgID, key, value, change)` and `ResetForOrg`: same transaction as platform writes (row lock, version check, history row with `org_id`, `NOTIFY`), versions counted per organisation, `ReasonRequired` honoured, actor required. Audit event `settings.value.changed` with `OrgID` set and `org_id` in metadata |
| Reads | `ListForOrg(orgID)` and `GetForOrg(orgID, key)` from memory; `View` gains `OrgOverridable`, `OrgID` and `PlatformValue`; `HistoryForOrg`; `Overrides(ctx, key, after, limit)` pages the organisations with their own value from the database |
| Errors | `ErrNotOrgOverridable`, `ErrInvalidOrgID` (empty, over 100 bytes, or not visible ASCII, since a space separates the notification payload) |
| Loading | `Reload` loads platform values, then organisation values of overridable keys only (`key = ANY($1)`). A full reload keeps newer versions already applied and drops values whose rows disappeared, unless they were applied while it ran (a per-registry sequence number) |
| Migration | `00002_settings_org_values.sql`: indexes `(key, org_id)` for `Overrides` and `(org_id, key, id DESC)` for organisation history |
| Checking membership | Not the library's job: callers check that the caller may act in the organisation (`orgs.RequireMember`) |

### Apps

| App | Change |
|---|---|
| Both Full apps | Library migration copied as `20260918000001_settings_org_values.sql`; `/ops/settings` responses gain `org_overridable`; `GET /ops/settings/{key}/overrides` (`ops.settings.read`); `TestSecuritySettingsArentOrgOverridable` fails when a setting in groups `rate_limits`, `retention`, `maintenance`, `mail`, with keys under `auth.`, `audit.`, `mail.`, `maintenance.`, `ops.`, `releases.`, or `orgs.invitation_url`, `orgs.max_owned`, `orgs.deleted_org_retention`, `orgs.user_invitations_per_hour`, is declared overridable |
| full-multi | `20260918000002_settings_org_purge.sql` (cascading foreign keys); `orgs.invitation_ttl` declared `OrgOverridable`; `GET /v1/orgs/{orgId}/settings`, `GET|PUT|DELETE /v1/orgs/{orgId}/settings/{key}`, `GET /v1/orgs/{orgId}/settings/{key}/history` in the orgs module; org permissions `orgs.settings.read` (every role) and `orgs.settings.write` (owner, admin); `settings.ErrNotOrgOverridable` maps to 404 `setting_not_found`; the other settings error codes are reused |
| full-single | Library and `/ops` changes only; no setting is overridable, so `overrides` lists are empty |
| `orb add orgs` | Copies later organisation-only migrations (`recipes.OrgsLaterMigrationPaths`) after the conversion |

Non-members get 404 `org_not_found` on every organisation settings route, like every other organisation route.

## Why

- Opt-in declarations keep security-relevant settings platform-wide by construction, with a test that names the setting if someone marks one.
- Keeping every value in memory preserves `Get`'s contract (no database, no error, no lock), which every module relies on.
- One channel with a backward-compatible payload keeps one listener per instance and makes rolling deploys safe.
- Cascading foreign keys make purging an organisation's settings part of the same transaction as every other org-scoped row.

## Trade-offs

- Memory grows with organisation values: about 350 bytes per value (100 000 values: 33 MiB of heap), and a full reload reads them all every 5 minutes (100 000 rows: about 0.3 s on a laptop). Apps with millions of organisations changing settings should revisit this (an LRU with a warm-up, or `Get` returning the platform value while loading).
- `Get` of an overridable setting inside an organisation costs about 24 ns instead of 3 ns (a context lookup and a `sync.Map` read).
- Values of an organisation purged while an instance missed notifications stay in that instance's memory until its next full reload (at most 5 minutes); they are never read, since the organisation can't be reached.
- Organisation admins see the platform value (`platform_value`) of overridable settings; don't mark a setting overridable if its platform value is confidential.
- The deny list in the app test is maintained by hand; a new security-relevant group needs adding there.
- Organisation IDs are limited to visible ASCII of at most 100 bytes (gorbital's are 30).

## Consequences

- ADR-0031's "Per-org settings (column reserved)" under Not included is now built; `Get(ctx)` may depend on ctx for overridable settings.
- ADR-0048's organisations gain settings of their own; purging an organisation removes them.
- New public names: org permissions `orgs.settings.read` and `orgs.settings.write`; `/ops` operation `ops-setting-overrides` and response field `org_overridable` (additive; the baseline check passes). No new error codes, audit actions, settings or jobs.
- Existing apps receive it through `orb upgrade`: one migration in single-tenant apps, two in multi-tenant apps, and the files above.

## Implementation notes (2026-09-16)

- `modules/settings`: `org.go` (organisation memory, `ListForOrg`, `GetForOrg`, `SetForOrg`, `ResetForOrg`, `HistoryForOrg`, `Overrides`), `select_org_value.go`, `select_overrides.go`; `write` takes the organisation; the notification payload carries it; `stored.raw`, never read, was removed.
- Memory layout: the first version kept a `map[string]stored` per organisation and used 126 MiB for 100 000 values (a map bucket per organisation). A short slice per organisation brought it to 33 MiB.
- full-multi: `internal/modules/orgs/usecase/settings.go` (use cases after `RequireMember`), `delivery/settings.go`, `SettingsStore` port and `Config.Settings` (optional: without it organisations have no settings).

| Check | Result |
|---|---|
| Library `TestOrgValuesOverridePlatformValues` | Organisation value applies only in that organisation; platform changes still reach other organisations; versions per organisation; history and audit events carry the organisation; `Overrides` pages by organisation and skips reset values |
| Library `TestOrgValuesRejectInvalidChanges` | Not overridable, unknown key, invalid value, missing reason, no actor and malformed organisation IDs are refused, with no rows or audit events |
| Library `TestOrgValuesLoadAtStartupAndLeaveWithTheirRows` | Values load at startup; invalid ones are flagged and ignored; rows of non-overridable settings are ignored; deleted rows leave memory on `Reload` |
| Library `TestOrgValuesConvergeThroughNotifications` | A second instance sees an organisation value and its reset through `NOTIFY` only |
| Library `TestConcurrentOrgChangesOneWins` | Six concurrent first changes: exactly one succeeds |
| `BenchmarkSettingGet` | Platform 2.7 ns, overridable in an organisation 24 ns, no allocations |
| full-multi `TestOrganisationSettingsEndToEnd` | Members read, members can't write (403), reason and bounds enforced, platform-only and unknown keys 404 `setting_not_found`; invitations in the organisation expire after its 48 h, in another organisation after 7 days; history; `/ops/settings/{key}/overrides` and the audit event name the organisation; purging the organisation deletes its values and history |
| full-multi `TestOrganisationsCantReachEachOthersSettings` | Another organisation's member gets 404 `org_not_found` on list, get, set, reset and history, and doesn't see the value |
| Both apps `TestSecuritySettingsArentOrgOverridable` | Passes; marking `mail.from_name` overridable fails naming it |
| `TestAddOrgs` | The settings purge migration follows the conversion |

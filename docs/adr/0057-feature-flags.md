# ADR-0057: Feature flags

**Status:** Accepted (2026-09-16)

## Context

v1.1 item 2 (roadmap): a new `modules/flags` with flags declared in code, on/off with per-organisation and per-user targeting and a stable percentage rollout, stored in PostgreSQL and reloaded on every instance, and `/ops/flags` with reasons, history and audit events. Runtime settings (ADR-0031) deliberately left flags and percentage rollouts out. Today:

| Area | Today | Evidence |
|---|---|---|
| Turning features on | Only runtime settings: one value for everyone, or per organisation for settings declared `OrgOverridable` (ADR-0056). No user targeting, no gradual rollout | `modules/settings`, `internal/app/settings.go` |
| Live configuration pattern | Typed handles declared in Go; only changed values stored (`settings_values`, `settings_history`); a transaction with a version check, history row and `pg_notify`; an `app.Runner` listening on a dedicated connection with a 5-minute full reload; `Get` reads an atomic snapshot and never touches the database | ADR-0031, `modules/settings/store.go`, `listen.go` |
| Who is asking | `actor.From(ctx)`: `Kind` (user, service, system, anonymous), `ID`, and `OrgID` once `orgs.RequireMember` has checked membership | `actor/actor.go`, `modules/orgs/member.go` |
| Signed-in clients | Bearer or cookie sessions set the actor in `auth.Middleware`; `GET /v1/ping` accepts anonymous callers too | `internal/app/routes.go` |
| Ops APIs | `internal/modules/ops`: every use case calls `authorize` first (enforced by `TestEveryOperationAuthorizesFirst`); `platform_admin` gets every `ops.*` permission, `ops_viewer` the read ones; both need 2FA | `internal/modules/ops/usecase`, `internal/app/permissions.go` |
| History retention | The `retention` job deletes `settings_history` and `job_definition_history` after `ops.history_retention`, listed in `/ops/retention` | ADR-0051, `internal/app/app.go` |
| Public names | Error codes, audit actions, permissions, settings and jobs are recorded in `api/surface.json`; `/ops` changes must be additive | ADR-0054 |

Constraints: modules never import each other, so `modules/flags` can't use `modules/settings` or know about organisations beyond `actor.Actor.OrgID` (ADR-0019); PostgreSQL is the only required service (ADR-0014).

## Options

| Question | Option | Verdict |
|---|---|---|
| Where flags live | Settings of a new `KindFlag` in `modules/settings` | Rejected: a flag's state (lists, percentage) isn't a bounded scalar, evaluation needs the actor, and settings' API (`Get` returns a value, `OrgOverridable`) would grow a second model |
| | A hosted flag service (LaunchDarkly, Unleash, Flagsmith) | Rejected as the default: another service to run or pay for, and user IDs leave the platform. Apps can still use one |
| | **New `modules/flags`, following the settings store's patterns without importing it** | **Chosen**: same operator experience (version, reason, history, audit, `NOTIFY`), its own tables and channel |
| Declaration | Flags created in the database through the API | Rejected: typos compile, keys drift from code, nothing tells a dashboard what a flag does (ADR-0031's lesson) |
| | **`flags.Bool(reg, key, opts...)` with `Describe`, `Group`, `Client`, `DefaultOn`** | **Chosen**: the code is the catalog; a flag is off until turned on, unless declared `DefaultOn` (kill switches of released features) |
| State model | A list of arbitrary rules with attributes (country, plan, email domain) | Rejected for v1.1: needs an attribute contract per app and a rule language; organisation and user IDs cover betas and customer rollouts |
| | **Fixed state: `enabled`, `default`, `orgs {allow, deny}`, `users {allow, deny}`, `percentage` (nil = none)** | **Chosen**: one precedence everyone can learn, validated with hard bounds |
| Precedence | Allow lists before deny lists | Rejected: a deny must be able to exclude someone from a broad allow |
| | **`enabled` → organisation deny, allow → user deny, allow → percentage → default** | **Chosen**: the organisation acting decides before its members' personal lists, so an organisation-wide beta is consistent for its members; deny beats allow within a level |
| Rollout subject | Always the user | Rejected: members of one organisation would see a feature flicker between colleagues |
| | **The organisation when the actor acts in one, else the actor's ID; anonymous callers only follow 0 and 100** | **Chosen**, as the roadmap asks. Anonymous requests have no stable identity worth hashing (IPs change and are shared) |
| Bucketing | FNV or xxhash of the subject | Rejected: not easily reproduced by clients and pipelines in other languages; cheaper doesn't matter at ~140 ns |
| | **First 8 bytes of SHA-256(key ∥ 0x00 ∥ subject), big-endian, mod 100; on when below the percentage** | **Chosen**: stable across instances, releases and languages; raising the percentage only adds subjects; the key in the hash makes flags independent. Pinned by a test |
| Where evaluation reads state | Database per check | Rejected: flags sit on hot paths, like settings' `Get` |
| | **Every flag's compiled state in memory (atomic snapshot, sets for lists)** | **Chosen**: lists are bounded, flags are few |
| Reasons | Optional, like most settings | Rejected: every flag change alters what users get in production |
| | **Always required, for changes and resets** | **Chosen** |
| Client exposure | Every flag listed to clients | Rejected: keys of server-side or unreleased features leak |
| | **Only flags declared `Client()`: `GET /v1/flags`; in multi-tenant apps also `GET /v1/orgs/{orgId}/flags` after `RequireMember`** | **Chosen**. Answers only (key → bool), no rules or reasons, `Cache-Control: private, no-store` |
| Where the org route lives | The new flags app module, taking a membership port | Rejected: the module would differ between the golden apps, for one route about the organisation |
| | **The generated `orgs` module, with the existing `orgs.org.read` permission** | **Chosen**: like organisation settings (ADR-0056); every member reads flags, no new org permission |
| Audit metadata | The whole state | Rejected: up to 4,000 IDs per event |
| | **A summary (enabled, default, percentage, organisation and user target counts, version, reason)** | **Chosen**: the full old and new states are in the flag's history, deleted by retention |

## Decision

### Library (`modules/flags`)

| Topic | Decision |
|---|---|
| Declaration | `flags.NewRegistry()`, `flags.Bool(reg, key, Describe, Group, Client, DefaultOn)`; bad keys, duplicates and declarations after `NewStore` panic |
| Evaluation | `(*Flag).Enabled(ctx)`, `Get(ctx)` (so a flag is a `config.Value[bool]`), `Evaluate(ctx)` returning `Evaluation{Key, Enabled, Reason}`; `flags.Bucket(key, subject)`; memory only |
| State | `flags.State{Enabled, Default, Orgs, Users Targets{Allow, Deny}, Percentage *int}`; lists at most `MaxTargets` (1,000) IDs of 1–`MaxIDLen` (100) visible ASCII bytes, no ID in both lists of a level, percentage 0–100; lists stored sorted without duplicates |
| Store | `NewStore(ctx, pool, reg, recorder, WithLogger, WithResyncInterval)`; `Run` (LISTEN `gorbital_flags`, reload after connect, 5-minute resync); `Reload`, `List`, `Get`, `Set`, `Reset`, `History`, `ClientFlags`, `UnknownKeys`, `DeleteHistoryBefore`, `OldestHistory` |
| Writes | One transaction: row lock, version check, no-op for an equal state, insert or update, history row, `pg_notify`; the change applies locally at once; audit `flags.flag.changed` or `flags.flag.reset` after commit (a failed audit write is logged) |
| Errors | `ErrUnknownFlag`, `ErrVersionConflict`, `ErrReasonRequired`, `ErrActorRequired`, `ErrInvalidState` (`*InvalidStateError`, reasons never contain IDs) |
| Loading | Rows of undeclared keys kept and ignored; a stored state that fails validation is ignored (declared state applies), logged and flagged `InvalidStoredValue`; unknown JSON fields ignored so older instances read newer states |
| Tables | `migrations/00001_flags.sql`: `flags_states` (key, state jsonb, version, updated_at, updated_by) and `flags_history` |

### Apps (both Full apps unless noted)

| Change | Detail |
|---|---|
| Migration | `20260918000010_flags.sql` |
| Wiring | `internal/app/flags.go` (`declareFlags`), store built after settings in `app.go`, listed in `Workers()` |
| Example | `example.ping_time` (client flag, off): `GET /v1/ping` adds `server_time` when it is on for the caller; the ping module takes it as a `config.Value[bool]` |
| Ops | `GET /ops/flags?group=`, `GET|PUT|DELETE /ops/flags/{key}`, `GET /ops/flags/{key}/history` in the ops module; permissions `ops.flags.read` (`platform_admin`, `ops_viewer`) and `ops.flags.write` (`platform_admin`) |
| Clients | New app module `internal/modules/flags`: `GET /v1/flags` (signed in; 401 `unauthenticated` otherwise) |
| Organisations (full-multi) | `GET /v1/orgs/{orgId}/flags` in the orgs module after `RequireMember(orgs.org.read)`; non-members 404 `org_not_found` |
| Error codes | `flag_not_found` (404), `flag_version_conflict` (409), `flag_reason_required` (422), `invalid_flag_state` (422); schema bounds (percentage range, list length) answer `validation_failed` |
| Retention | `flags_history` deleted by the `retention` job after `ops.history_retention`, listed in `/ops/retention` |
| Surface | `api/surface.json` gains a `flags` list, checked by `TestPublicSurface` |

## Why

- Operators get one familiar model for live changes: declared in code, versioned, reasoned, audited, applied everywhere within moments, with no new service.
- A fixed, ordered state makes every answer explainable (`Evaluate` returns the rule), and bounded lists keep evaluation lock-free and allocation-free.
- Hashing organisation or user with the flag key gives stable, independent, reproducible rollouts; organisations as the subject keep colleagues consistent.
- Exposing only `Client()` flags, and only their answers, keeps rules, IDs and unreleased feature names on the server.

## Trade-offs

- Flags are booleans with ID lists: no variants, attributes, schedules or experiments with metrics. Apps needing those use a flag service.
- A change replaces the whole state: two operators editing different lists conflict (409) rather than merge.
- IDs in lists and history outlive deleted accounts and organisations until edited and until history retention; they are opaque IDs, not emails.
- Anonymous callers can't be rolled out gradually; a feature for logged-out pages goes 0% or 100%, or uses the default.
- Every instance holds every flag's state in memory (at most 4,000 IDs per flag) and reloads all flags every 5 minutes; the `LISTEN` connection has the same PgBouncer caveat as settings.
- Flags aren't access control: a client can observe its client flags, and the ops API edits lists. Guides say so.

## Consequences

- ADR-0031's "feature flags and percentage rollouts (v1.1)" are built as a separate module; runtime settings are unchanged.
- New module `modules/flags` (stable, ADR-0054) in CI's test, lint and govulncheck lists and `api/modules-flags.txt`. Not added to CODEOWNERS' security-sensitive paths, like `modules/settings`: flags don't authenticate or authorise.
- New public names: flag key `example.ping_time`; permissions `ops.flags.read`, `ops.flags.write`; audit actions `flags.flag.changed`, `flags.flag.reset`; the error codes above; `/ops` operations `ops-list-flags`, `ops-get-flag`, `ops-set-flag`, `ops-reset-flag`, `ops-flag-history` (additive; the baseline check passes); `GET /v1/flags`, and `GET /v1/orgs/{orgId}/flags` in multi-tenant apps.
- Existing apps receive it through `orb upgrade`: one migration, the new files and wiring; `api/surface.json` gains `flags`.

## Implementation notes (2026-09-16)

- Library files: `flags.go` (registry, handles, evaluation), `state.go` (bounds, normalisation, storage form), `options.go`, `store.go`, `listen.go`, one statement per repository file (`select_states.go`, `select_state.go`, `insert_state.go`, `update_state.go`, `insert_history.go`, `select_history.go`, `delete_history.go`, `notify.go`).
- `BenchmarkEnabled` (state with lists and a 50% rollout, actor in an organisation): 142 ns, 0 allocations on an M-series laptop; almost all of it SHA-256.
- The `/ops/flags` request schema bounds `percentage` (0–100) and list lengths (1,000), so those answer `validation_failed` before the library checks them again; `orgs`, `users` and their lists may be omitted (empty).
- The ping response keeps its `MessageResponse` schema and gains an optional `server_time`, so generated clients stay compatible.

| Check | Result |
|---|---|
| `TestBucketIsStable` | Pinned bucket values for four subjects |
| `TestRulePrecedence` | Disabled beats an allowed organisation; organisation deny beats user allow; organisation allow beats user deny; user lists beat the rollout; organisation lists ignore callers outside organisations; service accounts are targeted by ID; anonymous actors and a missing actor aren't users; default on and off |
| `TestRolloutIsDeterministic` | 200 users, repeated calls and a second registry with the same state give the same answers, matching `Bucket` |
| `TestRolloutDistribution` | Over 10,000 subjects, 0/1/10/25/50/75/90/99/100% turn the flag on within 1.5 points; raising the percentage never removes a subject; two 50% flags overlap on 25% ± 1.5 |
| `TestOrganisationIsTheSubject` | In an organisation every member gets its bucket's answer; outside, each user their own |
| `TestAnonymousCallersGetOnlyWholeRollouts` | 0% and 100% apply; 50% falls to the default; user lists don't apply |
| `TestStateBounds`, `TestDeclarationsPanic`, `TestDeclaredDefaults` | 1,000 IDs and 100-byte IDs accepted; 1,001 IDs, percentages −1 and 101, empty, spaced and 101-byte IDs, and an ID in both lists refused without echoing IDs; bad and duplicate keys panic |
| `TestSetResetHistoryAndAudit` (PostgreSQL) | Set normalises lists, applies at once, views are copies; stale version conflicts; reset restores the declared state and a second reset is a no-op; history pages with old and new states, actor and request ID; audit events with summaries and no IDs |
| `TestSetRejectsInvalidChanges` | Invalid states, missing or blank reasons (set and reset), no actor, anonymous, unknown flags: no rows, no events |
| `TestSettingTheSameStateIsANoOp`, `TestClientFlags` | Reordered lists are the same state; `ClientFlags` lists only `Client()` flags, evaluated per actor |
| `TestStoredStatesLoadAtStartup` | States load; an out-of-bounds stored state is flagged and ignored; unknown JSON fields ignored; unknown keys listed |
| `TestInstancesConvergeThroughNotifications`, `TestPeriodicResyncCatchesChangesWithoutNotifications`, `TestConcurrentFirstChangesOneWins`, `TestDeleteHistoryBefore` | A second store applies a change and a reset through `NOTIFY` only; a direct SQL change arrives by resync; six concurrent first changes: one wins; history deletion in batches and `OldestHistory` |
| Apps `TestFeatureFlagsEndToEnd` (both) | 401 without a session, 403 for users and for viewers changing; viewers read; `GET /v1/flags` lists exactly the flags `/ops/flags` marks `client`, with `no-store`; reason, bounds, overlap, unknown flag and stale version refused; a user allow list with 0% gives only that user `server_time`; a 50% rollout matches each user's bucket on every call, and anonymous callers get the default; history and `flags.flag.changed`/`flags.flag.reset` audit events without targeted IDs |
| Apps `TestFlagChangeReachesAnotherInstance` (both) | A change and a reset on one instance reach a second through its listener |
| full-multi `TestOrganisationFlagsEndToEnd` | With the organisation allowed and a member denied, both members get true in the organisation and false outside; at 50% both members get the organisation's bucket, and outside each gets their own |
| full-multi `TestOrganisationsCantReachEachOthersFlags` | Another user gets 404 `org_not_found` for the organisation's flags (and a missing one), and the organisation's allow list doesn't reach their own |
| `TestOpsRetention`, `TestPublicSurface`, `TestOpsAPICompatible`, `TestOpenAPIUpToDate`, `TestEveryOperationAuthorizesFirst`, `TestGoldenAppsDontDrift`, `apicheck` | `flags_history` policy listed; names recorded; `/ops` additive; API files regenerated; the five new ops use cases authorise first; `org_flags_test.go` is the only multi-tenant difference; `api/modules-flags.txt` recorded |

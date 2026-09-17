# Internal admin tool

An internal back-office API. Staff publish announcements that customers see, such as a maintenance window, and operators tune the app while it runs: how many announcements can be active, how long expired ones are kept, who hit a rate limit. Nothing here needs a redeploy.

The app is in `examples/apps/admin-tool/`. It has one module, `announcements`, laid out like Shelfie's books module ([1. A books module](../shelfie/01-books-module.md)): `domain/`, `usecase/`, `repository/` and `delivery/`, one file per operation. Part 1 is the operations side; [part 2](#part-2-sign-in-and-reviewing-changes) is sign-in, the audit log as a workflow, and the layers above an address list.

| Route | Who | Does |
|---|---|---|
| `POST /v1/announcements` | Staff (`announcements.announcement.write`) | Publishes an announcement from now until `ends_at` |
| `POST /v1/announcements/{id}/withdraw` | Staff, signed in within the last 10 minutes | Deletes an announcement customers can still see, with a reason the audit log keeps |
| `GET /v1/announcements` | Anyone | Lists the active announcements, newest first |
| `GET /v1/flags` | Signed-in customers | Says whether apps show the banner |
| `/v1/auth/*` | Anyone with an account | Signing in, second factors, passkeys, API keys. There is no sign-up |
| `/ops/*` | Operators (`platform_admin`, `ops_viewer`) | Settings, flags, retention, rate limits, jobs, audit log |

## main.go

<!-- include examples/apps/admin-tool/cmd/api/main.go#main -->

| Module | Adds |
|---|---|
| `opshttp.Module()` | The operations API under `/ops/`, and the roles `platform_admin` (every `ops.*` permission) and `ops_viewer` (reading) |
| `flagshttp.Module()` | `GET /v1/flags`, the client flags a signed-in caller sees |
| `modules.All()` | The app's own modules: here, `announcements` |

`gorbital.WithAuth` takes the authenticator. `authhttp` serves `/v1/auth/`, authenticates every request, and grants `platform_admin` and `ops_viewer` only to sessions signed in with a second factor; what `signInOptions` changes is [part 2](#sign-in-for-a-tool-nobody-signs-up-to).

Details: [Your main.go](../../guides/main-go.md), [Ops API reference](../../guides/ops-api.md#adding-it-to-an-app), [opshttp](../../methods/gorbital-opshttp.md), [flagshttp](../../methods/gorbital-flagshttp.md).

## Staff and operators

Staff and operators are signed-in users with a role. The module gives its write permission to `platform_admin`, so the people who operate the app also publish, and to `announcements_editor`, the role of staff who publish and do nothing else ([part 2](#three-roles)):

<!-- include examples/apps/admin-tool/internal/modules/announcements/module.go#permissions -->

Two shortcuts save you an account while you work on the app:

- Locally, `orb dev` prints a dev console token. On `/ops/` it acts as an operator holding `platform_admin`'s permissions, from loopback only and never in production ([the dev console's operator](../../guides/ops-api.md#the-dev-consoles-operator)). It doesn't open `/v1/` routes, so `POST /v1/announcements` answers 401 with it.
- The tests say who calls with `gorbitaltest`: a staff member is a user holding `announcements.announcement.write`, an operator a user holding `ops.*` permissions. `cmd/api/signin_test.go` uses accounts that really signed in instead.

The route table puts the permission, the rate limit and the public read on the routes:

<!-- include examples/apps/admin-tool/internal/modules/announcements/delivery/routes.go#routes -->

## A runtime setting

`announcements.max_active` caps how many announcements are active at once, so the banner stays readable. It is declared in the module, with its range and a required reason:

<!-- include examples/apps/admin-tool/internal/modules/announcements/module.go#settings-and-flags -->

The use case reads it on every publish, so a change applies on the next request, on every instance:

<!-- include examples/apps/admin-tool/internal/modules/announcements/usecase/publish_announcement.go#check-active -->

Change it with `PUT /ops/settings/{key}` (`ops.settings.write`). Send the `version` you last read (0 for a setting never changed) and a `reason`:

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/announcements.max_active \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":1,"version":0,"reason":"one banner at a time"}'
```

```text
{"key":"announcements.max_active","kind":"int","group":"announcements","value":1,"default":3,
 "modified":true,"version":1,"updated_by":"usr_ops","reason_required":true,"constraints":{"max":20,"min":1},…}
```

The next publish over the limit answers:

```text
409 {"code":"too_many_announcements","detail":"as many announcements are active as allowed; wait for one to end",…}
```

| Mistake | Answer |
|---|---|
| No `reason` | 422 `setting_reason_required` |
| A value outside 1 to 20 | 422 `invalid_setting_value` |
| A `version` someone else already changed | 409 `setting_version_conflict`: read again and retry |

Every change is kept in `GET /ops/settings/announcements.max_active/history` and the audit log. More: [runtime settings](../../guides/runtime-settings.md), [Ops API reference](../../guides/ops-api.md#runtime-settings).

## A client flag

`announcements.banner`, in the same block above, is declared with `flags.Client()`. The server doesn't read it: the customer apps do, from `GET /v1/flags`, and show the banner when it is on.

```bash
curl -H "Authorization: Bearer $CUSTOMER_TOKEN" http://127.0.0.1:8080/v1/flags
```

```text
{"flags":{"announcements.banner":false}}
```

Operators turn it on, for everyone or some users, with `PUT /ops/flags/announcements.banner` ([feature flags](../../guides/feature-flags.md), [Ops API reference](../../guides/ops-api.md#feature-flags)).

## Retention

An announcement that ended is useless after a while. `Module.Retention` says how long it is kept and what deletes it:

<!-- include examples/apps/admin-tool/internal/modules/announcements/module.go#retention -->

| Field | Here |
|---|---|
| `Data` | `expired_announcements`: the name in `/ops/retention`, logs and `retention.purged` audit events |
| `Setting` | `announcements.retention`: 90 days by default, 1 day to 3 years, reason required |
| `Delete` | Called daily by the built-in `retention` job with the time the setting's value ago, until a batch is short |
| `Oldest` | The earliest end among expired announcements, shown as `oldest_at` |

`Delete` removes one batch per call. PostgreSQL's `DELETE` has no `LIMIT`, so a subquery picks the rows:

<!-- include examples/apps/admin-tool/internal/modules/announcements/repository/delete_expired.go#delete-expired -->

`GET /ops/retention` (`ops.settings.read`) lists it after what gorbital keeps:

```text
{"policies":[
  {"data":"audit_events","setting":"audit.retention","retention":"8760h0m0s","retention_seconds":31536000,"job":"retention",…},
  …
  {"data":"expired_announcements","setting":"announcements.retention","retention":"2160h0m0s","retention_seconds":7776000,
   "job":"retention","next_run_at":"2026-09-18T04:15:00Z"}]}
```

Shortening the retention is a setting change like any other: `PUT /ops/settings/announcements.retention` with `{"value":"720h","version":0,"reason":"…"}`. More: [Ops API reference](../../guides/ops-api.md#retention), [modules and routes](../../guides/modules-and-routes.md).

## A named rate limiter

`guard.RateLimit(20, time.Hour, guard.Named("announcements_publish"))` on the publish route (in the route table above) allows each staff member 20 publishes an hour. The name is what operators see in `GET /ops/auth/rate-limits` (`ops.auth.read`):

```text
{"limiters":[
  {"name":"auth_ip","keys":"client IP address",…},
  {"name":"ops_test_email","keys":"actor ID",…},
  {"name":"announcements_publish","keys":"actor kind and ID, or client address for requests without an actor",
   "description":"guard.RateLimit on POST /v1/announcements"}]}
```

Without `guard.Named`, the limiter is named after the operation ID. To give someone their budget back, reset their key:

```bash
curl -X POST http://127.0.0.1:8080/ops/auth/rate-limits/reset \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"announcements_publish","key":"user:usr_mfrggzdfmztwq2lk"}'
```

Listing needs `ops.auth.read` and the reset `ops.auth.write`, which are sign-in's permissions: `authhttp` declares them for `platform_admin` (and `ops_viewer` for reading). The list holds sign-in's own limiters too, such as `auth_login`. A reset is audited as `ops.rate_limit.reset` ([guards](../../methods/gorbital-guard.md), [Ops API reference](../../guides/ops-api.md#accounts)).

## Restricting /ops to your network

An admin tool shouldn't answer operators' requests from anywhere. Set `OPS_ALLOWED_IPS` to your office's or VPN's ranges:

<!-- include examples/apps/admin-tool/.env.example#ops-network -->

| Situation | Answer |
|---|---|
| `OPS_ALLOWED_IPS` empty | `/ops/` answers every address, and relies on sign-in and permissions |
| Client address in a listed range | The request goes on to the sign-in and permission checks |
| Client address outside every range | 403 `ip_not_allowed`, before the sign-in check, guards and input parsing; the address isn't echoed |
| Any route outside `/ops/` | Not affected: `GET /v1/announcements` stays public |

The address is the client's after `APP_TRUSTED_PROXIES`. Behind a load balancer, list the balancer there, or every request has the balancer's address and is allowed or refused as one. An invalid range stops the app at start. More: [security layers](../../guides/security-layers.md#ip-filter), [environment variables](../../guides/environment-variables.md#operations).

## Tests

Run them with PostgreSQL up (`orb dev`, or `docker compose up -d --wait`):

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://admin_tool:admin_tool@127.0.0.1:5432/admin_tool?sslmode=disable'
export GORBITAL_REQUIRE_DB=1
go test ./...
```

Staff publish, anyone reads, customers and anonymous callers can't publish:

<!-- include examples/apps/admin-tool/internal/modules/announcements/announcements_test.go#publish-and-read -->

An operator lowers the setting, and the next publish is refused:

<!-- include examples/apps/admin-tool/internal/modules/announcements/announcements_test.go#max-active -->

Workers don't run in tests, so the retention job never calls `Delete`. The test calls it itself, through the module value the app was built with (its settings are declared by then):

<!-- include examples/apps/admin-tool/internal/modules/announcements/announcements_test.go#retention-delete -->

`cmd/api/operations_test.go` builds the app with `main.go`'s options and checks what operators see:

<!-- include examples/apps/admin-tool/cmd/api/operations_test.go#retention -->

<!-- include examples/apps/admin-tool/cmd/api/operations_test.go#rate-limits -->

| Test | Checks |
|---|---|
| `TestBannerFlag` (`cmd/api/operations_test.go`) | `announcements.banner` is in `GET /v1/flags`, off |
| `TestOpenAPIIsCurrent` (`cmd/api/main_test.go`) | `api/openapi.json` matches the code; after changing a route, run `go run ./cmd/api openapi --dir api` |
| `internal/modules/architecture_test.go` | The layers import only what they may |
| `internal/modules/announcements/domain/announcement_test.go` | The rules of `NewAnnouncement` |

More: [Testing with gorbitaltest](../../guides/testing-with-gorbitaltest.md), [4. Tests](../shelfie/04-tests.md).

## Part 2: sign-in and reviewing changes

Part 1 left two questions open: who the signed-in people are, and how you find out afterwards what one of them changed. Sign-in answers the first and the audit log the second. The last section answers a third: what stands between the tool and someone who is not who they say.

### Sign-in for a tool nobody signs up to

<!-- include examples/apps/admin-tool/cmd/api/main.go#sign-in-options -->

| Option | What it does here | Why an internal tool wants it |
|---|---|---|
| `WithoutRegistration()` | `POST /v1/auth/register` isn't served | Accounts belong to people who already work here. Sign-up on a back-office API is a door for anyone who finds the host name |
| `MinPasswordLength(16)` | 16 characters instead of the library's 12 | Every account here can reach `/ops`, the banner customers see, or both. Staff keep passwords in a manager, so length costs them nothing |
| `APIKeyMaxTTL(30 * 24 * time.Hour)` | `auth.api_key_max_ttl` accepts at most 30 days, and defaults to it | Keys belong to scripts, and a script that still runs is renewed. A key nobody renews is a credential nobody is watching |
| `RequireMFA(announcements.RoleEditor)` | The role's permissions reach only sessions with a second factor | Sign-in already does this for `platform_admin` and `ops_viewer`. The app's own role changes what every customer sees, so it shouldn't be the weak one |

Nothing else is changed: passwords, passkeys, authenticator apps, recovery codes, sessions and API keys stay as the library ships them, and a bad option value stops the app at start ([configuring sign-in](../../guides/configuring-sign-in.md)).

Closed sign-up shows in the document as well as at the door, so generated clients don't offer a button that answers 404:

<!-- include examples/apps/admin-tool/cmd/api/signin_test.go#no-sign-up -->

With a Google, Apple or GitHub sign-in configured — this app configures none — a first sign-in of an address without an account is refused with 403 `registration_closed`, before anything is written ([closing sign-up](../../guides/configuring-sign-in.md#closing-sign-up)). The API key cap is the runtime setting operators already know, narrowed:

<!-- include examples/apps/admin-tool/cmd/api/signin_test.go#api-key-ttl -->

### Three roles

| Role | Declared by | May | Second factor |
|---|---|---|---|
| `platform_admin` | `opshttp`, `authhttp` | Every `ops.*` permission: settings, flags, jobs, accounts, rate limits, the audit log. Publishes and withdraws announcements | Always, by sign-in |
| `ops_viewer` | `opshttp` | The reading `ops.*` permissions, including `ops.audit.read`. Changes nothing | Always, by sign-in |
| `announcements_editor` | this module | `announcements.announcement.write`, and nothing else | `RequireMFA` |

<!-- include examples/apps/admin-tool/internal/modules/announcements/module.go#role -->

An operator creates the account and grants the role. Until that account enrols a second factor and signs in with it, the role grants nothing:

<!-- include examples/apps/admin-tool/cmd/api/signin_test.go#operator-creates-staff -->

Because the role needs a second factor, it can't be a service account's either, so no API key ever publishes:

<!-- include examples/apps/admin-tool/cmd/api/signin_test.go#no-robot-editors -->

### Reviewing a change

`/ops` records every change as an audit event, and so does this module. Reading them needs `ops.audit.read`, which both `ops_viewer` and `platform_admin` hold; reading the log is not itself audited. Filters combine:

| Question | Request |
|---|---|
| Who changed this setting, and why? | `GET /ops/audit?action=settings.value.changed&resource_id=announcements.max_active` |
| What has this person done this month? | `GET /ops/audit?actor_id=usr_…&from=2026-09-01T00:00:00Z` |
| What else did that one request do? | `GET /ops/audit?request_id=req_…`, the ID that also names the access log line and the trace |
| What happened to this announcement? | `GET /ops/audit?action_prefix=announcements.&resource_id=ann_…` |
| Which changes are failing? | `GET /ops/audit/stats?group_by=action&outcome=failure` |

`announcements.max_active` is declared with `settings.ReasonRequired()` (part 1), so there is always a why to find:

<!-- include examples/apps/admin-tool/cmd/api/operations_test.go#audit-setting-change -->

```bash
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8080/ops/audit?action=settings.value.changed&resource_id=announcements.max_active'
```

```text
{"events":[{"id":41,"occurred_at":"2026-09-17T09:12:44.081Z","recorded_at":"2026-09-17T09:12:44.083Z",
  "actor_kind":"user","actor_id":"usr_mfrggzdfmztwq2lk","action":"settings.value.changed",
  "resource_type":"setting","resource_id":"announcements.max_active","outcome":"success",
  "request_id":"req_99c4a38756b2eb8f","metadata":{"reason":"one banner at a time","reset":false,"version":1}}]}
```

A flag change is `flags.flag.changed` with the same `reason`, and a rate-limit reset is `ops.rate_limit.reset`, which names the limiter and never the key. Every action a gorbital app records, with its metadata: [audit actions](../../reference/audit-actions.md). Filters on `actor_id`, `action`, `resource_type`/`resource_id` and `request_id` use an index, while `outcome`, `actor_kind` or `action_prefix` alone scan newest first: on a large table, pair them with an indexed filter or a `from`/`to` window. Events can't be changed, and the `retention` job deletes them after `audit.retention`, 365 days by default ([Ops API reference](../../guides/ops-api.md#audit-log)).

### Withdrawing an announcement

A wrong announcement has to go before it ends. Withdrawing deletes the row, so the audit event is all that survives it — which is why the use case insists on a reason:

<!-- include examples/apps/admin-tool/internal/modules/announcements/usecase/withdraw_announcement.go#withdraw -->

<!-- include examples/apps/admin-tool/cmd/api/operations_test.go#audit-withdrawal -->

It is the tool's one destructive route, so `guard.RecentReauth()` sits beside `guard.Permission` on it in the route table above: a session that hasn't signed in or verified a second factor in the last 10 minutes is sent back to sign in, and an API key never gets there at all.

<!-- include examples/apps/admin-tool/cmd/api/operations_test.go#withdraw-guards -->

| Caller | Answer |
|---|---|
| Not signed in | 401 `unauthenticated` |
| Signed in without `announcements.announcement.write` | 403 `forbidden` |
| An API key, however it is scoped | 403 `session_required` |
| A session that last signed in or verified a second factor over 10 minutes ago | 403 `reauthentication_required` |
| A reason of nothing but spaces | 422 `withdrawal_reason_required` |
| An ID that isn't there, or that someone withdrew first | 404 `announcement_not_found` |

### Restricting the tool further than an address list

`OPS_ALLOWED_IPS` (part 1) is the outermost layer, and the one most often mistaken for the whole answer. Each layer stops something different:

| Layer | Stops | Doesn't stop |
|---|---|---|
| `OPS_ALLOWED_IPS` | `/ops/` requests from outside your office or VPN, before the sign-in check, the guards and input parsing | Anything under `/v1/`, and an attacker already inside an allowed network. It isn't authentication |
| Sign-in (`authhttp`) | Requests with no, expired or revoked credentials; with sign-up closed, accounts nobody created | A session token stolen from a signed-in operator, until it expires or someone revokes it |
| A second factor for the roles | A leaked password on its own reaching `/ops` or the banner; those roles reaching service accounts and API keys at all | A session that already passed its second factor |
| `guard.Permission` on each route | An `ops_viewer` changing what they can read | The right person doing the wrong thing. That is what the audit log is for |
| `guard.RecentReauth()` on the withdrawal | An old stolen session deleting announcements | Routes that don't carry it, and a session whose thief signs in again |
| `guard.RateLimit` (part 1) | One staff member publishing more than 20 an hour | The same person with a second account |
| `APP_REQUEST_TIMEOUT` | A slow query or a stuck dependency holding a connection and a database slot until the client gives up | A handler that ignores its context: its goroutine runs on until it returns |

<!-- include examples/apps/admin-tool/.env.example#request-timeout -->

`gorbital.Timeout(d)` on a group or a route shortens the app's timeout further, and can only shorten it: a route that needs longer raises `APP_REQUEST_TIMEOUT`, or moves the slow work into a background job. Second factors need one more variable, which production refuses to start without:

<!-- include examples/apps/admin-tool/.env.example#auth-encryption-keys -->

### The tests of part 2

`cmd/api/signin_test.go` builds the app with `signInOptions`, so its accounts, roles and second factors are the real ones; `cmd/api/operations_test.go` keeps using `gorbitaltest` principals, which is enough for the guards and the audit log.

<!-- include examples/apps/admin-tool/cmd/api/signin_test.go#new-signed-in-app -->

| Test | Checks |
|---|---|
| `TestNobodySignsUp` | `POST /v1/auth/register` answers 404 and has left the OpenAPI document; signing in hasn't |
| `TestOperatorsCreateStaffAccounts` | The 16-character minimum on `POST /ops/auth/users`, granting `announcements_editor`, and 403 `mfa_required` from a session without a second factor |
| `TestNoRobotPublishes` | 422 `invalid_service_account_role` for a service account holding the role |
| `TestAPIKeysExpireWithinTheMonth` | `auth.api_key_max_ttl` defaults to 720h, and 422 `invalid_setting_value` above the cap |
| `TestSettingChangesSayWhy` | 422 `setting_reason_required` without a reason, and the reason in the `settings.value.changed` event |
| `TestWithdrawalsAreAudited` | The withdrawal, its reason and its actor in `GET /ops/audit`, and the `request_id` filter |
| `TestWithdrawingNeedsARecentSession` | The six answers of the table above |

More: [security layers](../../guides/security-layers.md), [configuring sign-in](../../guides/configuring-sign-in.md), [authentication](../../guides/authentication.md), [Ops API reference](../../guides/ops-api.md#audit-log), [Shelfie, chapter 6](../shelfie/06-accounts.md).

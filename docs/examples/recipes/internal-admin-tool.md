# Internal admin tool, part 1: operations

An internal back-office API. Staff publish announcements that customers see, such as a maintenance window, and operators tune the app while it runs: how many announcements can be active, how long expired ones are kept, who hit a rate limit. Nothing here needs a redeploy.

The app is in `examples/apps/admin-tool/`. It has one module, `announcements`, laid out like Shelfie's books module ([1. A books module](../shelfie/01-books-module.md)): `domain/`, `usecase/`, `repository/` and `delivery/`, one file per operation.

| Route | Who | Does |
|---|---|---|
| `POST /v1/announcements` | Staff (`announcements.announcement.write`) | Publishes an announcement from now until `ends_at` |
| `GET /v1/announcements` | Anyone | Lists the active announcements, newest first |
| `GET /v1/flags` | Signed-in customers | Says whether apps show the banner |
| `/ops/*` | Operators (`platform_admin`, `ops_viewer`) | Settings, flags, retention, rate limits, jobs, audit log |

## main.go

<!-- include examples/apps/admin-tool/cmd/api/main.go#main -->

| Module | Adds |
|---|---|
| `opshttp.Module()` | The operations API under `/ops/`, and the roles `platform_admin` (every `ops.*` permission) and `ops_viewer` (reading) |
| `flagshttp.Module()` | `GET /v1/flags`, the client flags a signed-in caller sees |
| `modules.All()` | The app's own modules: here, `announcements` |

Details: [Your main.go](../../guides/main-go.md), [Ops API reference](../../guides/ops-api.md#adding-it-to-an-app), [opshttp](../../methods/gorbital-opshttp.md), [flagshttp](../../methods/gorbital-flagshttp.md).

## Staff and operators

Staff and operators are signed-in users with a role. The module gives its write permission to `platform_admin`, so the people who operate the app also publish:

<!-- include examples/apps/admin-tool/internal/modules/announcements/module.go#permissions -->

Sign-in, which is what grants those roles, arrives in Phase 5. Until then:

- Locally, `orb dev` prints a dev console token. On `/ops/` it acts as an operator holding `platform_admin`'s permissions, from loopback only and never in production ([the dev console's operator](../../guides/ops-api.md#the-dev-consoles-operator)). It doesn't open `/v1/` routes, so `POST /v1/announcements` answers 401 when you run the app.
- The tests say who calls with `gorbitaltest`: a staff member is a user holding `announcements.announcement.write`, an operator a user holding `ops.*` permissions.

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

The reset needs `ops.auth.write`, which is sign-in's permission: no role holds it until sign-in's module arrives in Phase 5, so today the call answers 403 `forbidden`. It is audited as `ops.rate_limit.reset` ([guards](../../methods/gorbital-guard.md), [Ops API reference](../../guides/ops-api.md#accounts)).

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

## Part 2

Part 2 arrives with a later phase, once sign-in and the security layers are in place. It covers reviewing the audit log as a workflow (who changed a setting, a flag or a rate limit, and why) and restricting the tool further than an address list.

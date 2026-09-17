# 5. Operations

Shelfie runs in production now, and the people who run it need to change things without a deploy: how many books a shelf holds, whether the apps show a new feature, which background jobs run when. This chapter adds the operations API under `/ops/` and the flags the apps read from `/v1/flags`, both built into gorbital, and gives the books module a runtime setting and a feature flag operators control.

> [!NOTE]
> Operators are accounts holding `platform_admin` or `ops_viewer`, given with `go run ./cmd/api grant-role <email> platform_admin`. Both roles require a second factor, so an operator turns on an authenticator app (`POST /v1/auth/mfa/totp`) and signs in with it before `/ops/` answers; until then it answers 403 `mfa_required`. In development `orb dev`'s Dev Portal operates it with the dev console's token, and the tests say who is calling.

## 1. Add the built-in modules

One line in `main.go`:

<!-- include examples/apps/shelfie/cmd/api/main.go#main -->

| Module | Adds |
|---|---|
| `opshttp.Module()` | `/ops/`: runtime settings, feature flags, jobs, the audit log, releases, email, file storage, system health, retention, rate limits, live request metrics and incidents ([Ops API](../../guides/ops-api.md), [Methods](../../methods/gorbital-opshttp.md)) |
| `flagshttp.Module()` | `GET /v1/flags`: whether each client flag is on for the signed-in reader ([feature flags](../../guides/feature-flags.md), [Methods](../../methods/gorbital-flagshttp.md)) |

They are modules like `books`: their routes, permissions and roles come with them. `opshttp` declares `platform_admin`, who holds every `ops.*` permission, and `ops_viewer`, who reads; sign-in (`authhttp`, in `main.go` since chapter 0) declares the accounts permissions and requires a second factor for both roles. The endpoints, error codes and permissions are those of a v0.1 app's generated `internal/modules/ops`, so tools written for v0.1, such as the Dev Portal, work unchanged.

What `/ops` shows comes from the whole app. Without writing anything else, operators already see gorbital's and sign-in's settings (`mail.*`, `maintenance.*`, `auth.*`, retention), their jobs (`retention`, `auth_cleanup`, …), the audit events `books` and sign-in record, which sign-in methods are configured in `/ops/auth/providers`, sign-in's rate limiters and the `guard.RateLimit` on `POST /v1/books` in `/ops/auth/rate-limits`, and the retention of deleted and unverified accounts in `/ops/retention`.

## 2. A setting and a flag in the books module

A module declares its runtime settings and flags in `Settings` and `Flags`, and keeps what they return for its routes:

<!-- include examples/apps/shelfie/internal/modules/books/module.go#settings-and-flags -->

- `books.shelf_limit` is how many books a reader's shelf holds. Its value lives in PostgreSQL, so a change reaches every instance within a second, without a restart. `ReasonRequired` makes operators say why they change it; the reason is in the setting's history and the audit log.
- `books.reading_goals` is a client flag: the web and mobile apps read it from `GET /v1/flags` and show reading goals when it's on. The server doesn't branch on it.

The use case reads the setting on every new book:

<!-- include examples/apps/shelfie/internal/modules/books/usecase/create_book.go#check-shelf -->

and `module.go` maps `ErrShelfFull` to 409 `shelf_full`.

## 3. Change it without a deploy

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/books.shelf_limit \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value": 500, "version": 0, "reason": "storage costs"}'
```

`version` is the one you read last: if someone changed the setting in between, you get 409 `setting_version_conflict` instead of overwriting their change. Turning the flag on for everyone:

```bash
curl -X PUT http://127.0.0.1:8080/ops/flags/books.reading_goals \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"version": 0, "reason": "launch", "state": {"enabled": true, "default": true, "orgs": {"allow": [], "deny": []}, "users": {"allow": [], "deny": []}}}'
```

A flag can also roll out to a percentage of readers, or to named users first ([feature flags](../../guides/feature-flags.md)).

## 4. Test it

An operator lowers the limit, and the next book is refused:

<!-- include examples/apps/shelfie/internal/modules/books/shelf_limit_test.go#shelf-limit -->

`cmd/api/operations_test.go` builds the app with the options of `main.go` and checks who can do what. A reader holding only the books permissions gets 403 `forbidden` on `/ops/`, a viewer reads but can't change:

<!-- include examples/apps/shelfie/cmd/api/operations_test.go#ops-permissions -->

and the flag reaches the apps once an operator turns it on:

<!-- include examples/apps/shelfie/cmd/api/operations_test.go#flags -->

`gorbitaltest.User` holds exactly the permissions you name, so the tests don't depend on how roles are granted.

## 5. Keep /ops to your network

Operators usually reach `/ops/` over a VPN. Set `OPS_ALLOWED_IPS` and every other address gets 403 `ip_not_allowed` before the request is even authenticated:

<!-- include examples/apps/shelfie/.env.example#ops-allowed-ips -->

The address is the client's after `APP_TRUSTED_PROXIES`: behind a load balancer, list the balancer there, or every request seems to come from it. Chapter 10 hardens the rest of the API ([security layers](../../guides/security-layers.md#ip-filter)).

## Next

[6. Accounts](06-accounts.md): registration fields, a shelf for every new reader, and suspended readers, with sign-in's options and hooks.

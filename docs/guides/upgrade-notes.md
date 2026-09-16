# Upgrade notes

What changes for existing apps in each release, and what to do that `orb upgrade` can't do for you. How upgrades work: [ADR-0016](../adr/0016-scaffold-compatibility-and-upgrades.md) and [ADR-0050](../adr/0050-upgrades-and-adding-features.md); commands: [CLI reference](cli.md).

## Unreleased

| Change | What to do |
|---|---|
| **Apple tokens are revoked by a job.** Unlinking Apple or deleting an account queues the token in `auth_token_revocations`; the new `auth_revoke_tokens` job (every minute) revokes it and retries failures ([ADR-0046](../adr/0046-google-and-apple-sign-in.md)) | Nothing: `orb upgrade` adds the job and migration. Account deletion no longer waits on Apple |
| **Rate limits are shared by every instance** ([ADR-0052](../adr/0052-shared-rate-limits.md)). Sign-in, per-IP and two-factor limits are counted in PostgreSQL (`modules/ratelimitpg`), so an app running several instances no longer allows several times each limit. They became runtime settings: `auth.login_attempts` (10), `auth.login_window` (15 min), `auth.ip_requests_per_minute` (60), `auth.mfa_change_attempts` (10) | Nothing for one instance. With several, effective limits get stricter: raise the settings if your traffic needs it |
| **`APP_TRUSTED_PROXIES`** | Behind a load balancer or reverse proxy, **set it before deploying** to their CIDR ranges (such as `10.0.0.0/8`). Otherwise every client shares the balancer's per-IP budget, and logs and audit events show the balancer's address |
| New job `ratelimit_cleanup` (hourly) | Nothing |
| Database | Two new migrations, `20260917000001_auth_token_revocations.sql` and `20260917000002_ratelimit_buckets.sql` (an unlogged table); run migrations as usual |
| **Public names are recorded and checked** ([ADR-0054](../adr/0054-api-freeze-and-scaffold-compatibility.md)). Full apps gain `api/surface.json` (error codes, audit actions, permissions and roles, setting keys, job names), `api/openapi.baseline.json` (the `/ops` contract), and the tests `TestPublicSurface` and `TestOpsAPICompatible` in `internal/app`; `permissions.go` gains `permissionCatalogs` | Nothing: `orb upgrade` adds the files and records `api/surface.json` from your code, including your own resources and jobs; review it in the upgrade commit. From then on, after `orb gen resource`, `orb gen job` or adding a code, action, permission, setting or job by hand, run `go test ./internal/app -run TestPublicSurface -update` and commit the file. A test failure for a recorded name means you removed something clients may use. [Stability and compatibility](stability.md) |
| **`orb --json` output starts with `"schemaVersion": 1`**; `orb version --json` is new | Scripts that read `--json` keep working (a field was added). Check `schemaVersion` from now on |
| Library | Additions only: `settings.(*Registry).Keys`, `jobs.(*Definitions).Names`, `openapi.CheckCompatible`. Package docs say `Stability: stable` |

## v0.5

```bash
orb upgrade --from v0.4.0     # apps created before v0.5 name the release that created them
go test ./...                 # on branch orb-upgrade/<version>
orb doctor                    # then merge the branch and deploy
```

| Change | What to do |
|---|---|
| **Audit events older than 365 days are deleted.** The new daily `retention` job applies `audit.retention` (365 days) and `ops.history_retention` (365 days, setting and job configuration history) ([ADR-0051](../adr/0051-operations-v0-5.md)) | If you must keep them longer, raise the settings **before deploying**: `PUT /ops/settings/audit.retention` with a value up to `87600h` (10 years) and a reason. Or disable the job first: `PUT /ops/jobs/definitions/retention` with `{"enabled": false}` |
| `gorbital.lock` moves to format v2 | The upgrade writes it; commit it. Later upgrades need no `--from` |
| `api/postman_collection.json` and `api/llms.txt` are generated with `api/openapi.json` | The upgrade writes them. From now on, regenerate with `go run ./cmd/api openapi --dir api`: the old `> api/openapi.json` form leaves the new files stale, and `TestOpenAPIUpToDate` fails |
| New endpoints: `GET /ops/system`, `/ops/audit/stats`, `/ops/jobs/overview`, `/ops/retention`; permission `ops.system.read` | Nothing: `platform_admin` and `ops_viewer` get the new permission. Custom roles that should see system health need `ops.system.read` |
| Maintenance mode: settings `maintenance.enabled`, `maintenance.message`, `maintenance.retry_after`; command `go run ./cmd/api maintenance on\|off` | Nothing: it's off. Clients should treat 503 with code `maintenance` and `Retry-After` as temporary |
| Release instance retention is the `releases.instance_retention` setting | Nothing: the default stays 90 days |
| `go run ./cmd/migrate --status [--json]` | Nothing: `orb doctor` uses it |
| Database | No new migrations |
| Library | `go get` is part of the upgrade: new `auditpg`, `jobs`, `settings`, `releases` and `openapi/reference` functions only; nothing removed |

Known issue: apps that ran `orb add mail --provider smtp` have three tests in `internal/app` that assume Resend (`TestLoadConfigReportsAllErrors`, `TestEmailConfiguration`, `TestWebAuthnConfiguration`). They failed before the upgrade too; a fix is in progress.

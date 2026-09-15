# Upgrade notes

What changes for existing apps in each release, and what to do that `aps upgrade` can't do for you. How upgrades work: [ADR-0016](../adr/0016-scaffold-compatibility-and-upgrades.md) and [ADR-0050](../adr/0050-upgrades-and-adding-features.md); commands: [CLI reference](cli.md).

## v0.5

```bash
aps upgrade --from v0.4.0     # apps created before v0.5 name the release that created them
go test ./...                 # on branch aps-upgrade/<version>
aps doctor                    # then merge the branch and deploy
```

| Change | What to do |
|---|---|
| **Audit events older than 365 days are deleted.** The new daily `retention` job applies `audit.retention` (365 days) and `ops.history_retention` (365 days, setting and job configuration history) ([ADR-0051](../adr/0051-operations-v0-5.md)) | If you must keep them longer, raise the settings **before deploying**: `PUT /ops/settings/audit.retention` with a value up to `87600h` (10 years) and a reason. Or disable the job first: `PUT /ops/jobs/definitions/retention` with `{"enabled": false}` |
| `apistock.lock` moves to format v2 | The upgrade writes it; commit it. Later upgrades need no `--from` |
| `api/postman_collection.json` and `api/llms.txt` are generated with `api/openapi.json` | The upgrade writes them. From now on, regenerate with `go run ./cmd/api openapi --dir api`: the old `> api/openapi.json` form leaves the new files stale, and `TestOpenAPIUpToDate` fails |
| New endpoints: `GET /ops/system`, `/ops/audit/stats`, `/ops/jobs/overview`, `/ops/retention`; permission `ops.system.read` | Nothing: `platform_admin` and `ops_viewer` get the new permission. Custom roles that should see system health need `ops.system.read` |
| Maintenance mode: settings `maintenance.enabled`, `maintenance.message`, `maintenance.retry_after`; command `go run ./cmd/api maintenance on\|off` | Nothing: it's off. Clients should treat 503 with code `maintenance` and `Retry-After` as temporary |
| Release instance retention is the `releases.instance_retention` setting | Nothing: the default stays 90 days |
| `go run ./cmd/migrate --status [--json]` | Nothing: `aps doctor` uses it |
| Database | No new migrations |
| Library | `go get` is part of the upgrade: new `auditpg`, `jobs`, `settings`, `releases` and `openapi/reference` functions only; nothing removed |

Known issue: apps that ran `aps add mail --provider smtp` have three tests in `internal/app` that assume Resend (`TestLoadConfigReportsAllErrors`, `TestEmailConfiguration`, `TestWebAuthnConfiguration`). They failed before the upgrade too; a fix is in progress.

# Jobs

<!-- Generated from examples/full-multi and examples/full-single by `go run -C internal/tools/refdocs . -write`. Don't edit: change the code, or internal/tools/refdocs/descriptions.json. -->

Background jobs run on River in PostgreSQL ([Background jobs](../guides/background-jobs.md)). Each job definition's schedule, whether it is enabled, its timeout, attempts, queue and priority can be changed at runtime through `/ops/jobs/definitions/{name}` ([Ops API](../guides/ops-api.md)); the values below are the defaults in code. Schedules are evaluated in UTC. Job names are public API: overrides, history and queued jobs refer to them ([Stability](../guides/stability.md)).

| Job | Default schedule | Enabled | Timeout | Attempts | Queue | Priority | What it does |
|---|---|---|---|---|---|---|---|
| `heartbeat` | `@every 1h` (every hour) | yes | 1 minute | 3 | `default` | 1 | Logs a heartbeat. An example job: change its schedule in /ops/jobs. |
| `auth_cleanup` | `30 3 * * *` (daily at 03:30) | yes | 5 minutes | 3 | `default` | 2 | Removes ended sessions, old email codes and accounts deleted longer ago than auth.deleted_account_retention. |
| `auth_revoke_tokens` | `@every 1m` (every minute) | yes | 5 minutes | 1 | `default` | 2 | Revokes the Apple refresh tokens of unlinked identities and deleted accounts, retrying failures with backoff. |
| `orgs_purge` | `45 3 * * *` (daily at 03:45) | yes | 10 minutes | 3 | `default` | 2 | Removes organisations deleted longer ago than orgs.deleted_org_retention, with their members, invitations and data. *Multi-tenant apps only.* |
| `ratelimit_cleanup` | `@every 1h` (every hour) | yes | 5 minutes | 3 | `default` | 3 | Deletes shared rate limit buckets whose keys are back to a full budget. |
| `idempotency_cleanup` | `@every 1h` (every hour) | yes | 5 minutes | 3 | `default` | 3 | Deletes idempotency keys and their stored responses once they are older than idempotency.retention. |
| `retention` | `15 4 * * *` (daily at 04:15) | yes | 30 minutes | 3 | `default` | 3 | Deletes audit events older than audit.retention and setting and job configuration history older than ops.history_retention. |
| `gorbital.mail.send` | none | always | | | | | Delivers queued email through the configured provider: up to 8 attempts of 30 seconds each on the `default` queue, and a message the provider rejects is cancelled instead of retried. The app enqueues one for every email it sends; it has no schedule and isn't a job definition, so `/ops/jobs/definitions` doesn't list it, but its runs appear in `/ops/jobs/runs`. |

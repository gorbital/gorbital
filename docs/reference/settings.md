# Runtime settings

<!-- Generated from examples/full-multi and examples/full-single by `go run -C internal/tools/refdocs . -write`. Don't edit: change the code, or internal/tools/refdocs/descriptions.json. -->

Runtime settings are non-secret values operators change without a redeploy: `PUT /ops/settings/{key}` stores the value in PostgreSQL and every instance applies it within seconds ([Runtime settings](../guides/runtime-settings.md), [Ops API](../guides/ops-api.md)). Secrets and infrastructure are environment variables instead ([Environment variables](../guides/environment-variables.md)). Setting keys are public API: stored values and history refer to them ([Stability](../guides/stability.md)).

**Reason required**: a change must say why (`reason`), and the reason is kept in the history and the audit event. **Restart required**: the value takes effect when an instance starts. Durations are written as Go durations in the API, such as `"720h"`.

## Example

| Key | Type | Default | Allowed | Reason required | Restart required | Description |
|---|---|---|---|---|---|---|
| `example.ping_message` | string | `"pong"` | at most 100 characters | no | no | Reply of GET /v1/ping. An example runtime setting: change it with PUT /ops/settings/example.ping_message. |

## Email

| Key | Type | Default | Allowed | Reason required | Restart required | Description |
|---|---|---|---|---|---|---|
| `mail.from_name` | string | the app's name | at most 100 characters | yes | no | Sender name on every email, such as Acme. |
| `mail.from_email` | string | `"no-reply@example.com"` | an email address | yes | no | Sender address on every email. With Resend, its domain must be verified in your Resend account. |
| `mail.reply_to` | string | empty | at most 254 characters | yes | no | Address replies go to. Empty: replies go to the sender address. |

## Authentication

| Key | Type | Default | Allowed | Reason required | Restart required | Description |
|---|---|---|---|---|---|---|
| `auth.session_idle_ttl` | duration | 14 days | 1 hour to 90 days | yes | no | How long a signed-in session lasts without being used. |
| `auth.session_absolute_ttl` | duration | 90 days | 1 day to 1 year | yes | no | The longest a session lasts, however often it is used; then the user signs in again. |
| `auth.verification_code_ttl` | duration | 15 minutes | 5 minutes to 1 hour | yes | no | How long email verification codes stay valid. |
| `auth.reset_code_ttl` | duration | 30 minutes | 10 minutes to 2 hours | yes | no | How long password reset codes stay valid. |
| `auth.deleted_account_retention` | duration | 30 days | 1 day to 1 year | yes | no | How long deleted accounts are kept before the auth_cleanup job removes them. |
| `auth.unverified_account_ttl` | duration | 7 days | 1 hour to 90 days | yes | no | How long an account whose email address was never verified stays before the auth_cleanup job deletes it, freeing the address. Accounts with a Google or Apple sign-in are kept. |

## Rate limits

| Key | Type | Default | Allowed | Reason required | Restart required | Description |
|---|---|---|---|---|---|---|
| `auth.ip_requests_per_minute` | int | `60` | 10 to 10000 | yes | no | Requests to /v1/auth/ allowed per client IP address per minute, across all instances. Behind a load balancer, set APP_TRUSTED_PROXIES so each client has its own budget. |
| `auth.login_attempts` | int | `10` | 3 to 100 | yes | no | Sign-in attempts allowed per email address from one client network (an IPv4 address or IPv6 /64) within auth.login_window, second factors included. |
| `auth.login_address_attempts` | int | `50` | 10 to 1000 | yes | no | Sign-in attempts allowed per email address from all networks together within auth.login_window. Keep it well above auth.login_attempts: reaching it blocks the owner's sign-ins too. |
| `auth.login_window` | duration | 15 minutes | 1 minute to 1 day | yes | no | The window of auth.login_attempts, auth.login_address_attempts, auth.mfa_change_attempts and auth.reauth_attempts. |
| `auth.mfa_change_attempts` | int | `10` | 3 to 100 | yes | no | Changes to two-factor authentication (confirming, turning off, replacing recovery codes) allowed per user within auth.login_window. |
| `auth.reauth_attempts` | int | `10` | 3 to 100 | yes | no | Changes that check the password or a second factor of a signed-in user (changing the password, setting up or turning off two-factor authentication, adding or removing passkeys, linking or unlinking Google or Apple, deleting the account) allowed per user within auth.login_window. |
| `auth.code_attempts` | int | `20` | 5 to 100 | yes | no | Email verification and password reset code checks allowed per email address within auth.code_window, across every code sent. Reaching it blocks the owner's codes too until the window passes. |
| `auth.code_window` | duration | 1 day | 1 hour to 7 days | yes | no | The window of auth.code_attempts. |
| `orgs.user_invitations_per_hour` | int | `50` | 1 to 10000 | yes | no | Invitations one user may send or resend per hour across all their organisations, on top of each organisation's 20 an hour. *Multi-tenant apps only.* |

## Retention

| Key | Type | Default | Allowed | Reason required | Restart required | Description |
|---|---|---|---|---|---|---|
| `audit.retention` | duration | 1 year | 30 days to 10 years | yes | no | How long audit events are kept before the retention job deletes them. Each deletion leaves a retention.purged audit event. |
| `ops.history_retention` | duration | 1 year | 30 days to 10 years | yes | no | How long the history of runtime setting and job configuration changes is kept before the retention job deletes it. |
| `releases.instance_retention` | duration | 90 days | 1 day to 3 years | no | no | How long instances are listed in /ops/releases after they were last seen. Applied when an instance starts. |
| `idempotency.retention` | duration | 1 day | 1 hour to 7 days | yes | no | How long responses to requests with an Idempotency-Key are kept for retries before the idempotency_cleanup job deletes them. They can hold personal data. Shortening it applies to stored responses at once. |
| `observability.retention` | duration | 1 day | 1 hour to 7 days | yes | no | How long request counts per minute, which /ops/observability and incident reports read, are kept before the observability_cleanup job deletes them. |

## incidents

| Key | Type | Default | Allowed | Reason required | Restart required | Description |
|---|---|---|---|---|---|---|
| `incidents.detection_window` | duration | 5 minutes | 1 minute to 1 hour | yes | no | How far back incidents_detect counts requests, in whole minutes up to the current one. |
| `incidents.error_rate_threshold` | float | `5` | 0.1 to 100 | yes | no | The percentage of requests answered with a server error (5xx), over incidents.detection_window, above which an automatic incident opens. |
| `incidents.min_requests` | int | `100` | 1 to 1e+06 | yes | no | How many requests incidents.detection_window needs before its error rate opens an incident or counts as recovered. |

## Maintenance mode

| Key | Type | Default | Allowed | Reason required | Restart required | Description |
|---|---|---|---|---|---|---|
| `maintenance.enabled` | bool | `false` | any | yes | no | Answer 503 to every request except health checks, docs, sign-in and /ops. |
| `maintenance.message` | string | empty | at most 500 characters | no | no | What clients see while maintenance mode is on. Empty: a generic message. |
| `maintenance.retry_after` | duration | 5 minutes | 1 minute to 1 day | no | no | The Retry-After clients get while maintenance mode is on. |

## Organisations

*Multi-tenant apps only.*

| Key | Type | Default | Allowed | Reason required | Restart required | Description |
|---|---|---|---|---|---|---|
| `orgs.invitation_url` | string | `"http://localhost:3000/invitations"` | at most 500 characters | yes | no | The frontend page invitation emails link to. The link adds #token=… for the page to POST to /v1/invitations/accept. |
| `orgs.invitation_ttl` | duration | 7 days | 1 day to 30 days | yes | no | How long an invitation link stays valid. Owners and admins may set their organisation's own value, within the same range, with PUT /v1/orgs/{orgId}/settings/orgs.invitation_ttl. |
| `orgs.deleted_org_retention` | duration | 30 days | 1 day to 1 year | yes | no | How long a deleted organisation can be restored before the orgs_purge job removes it with its data. |
| `orgs.max_owned` | int | `20` | 1 to 10000 | yes | no | How many organisations, personal workspaces aside, one user may own. Checked when they create or restore one. |

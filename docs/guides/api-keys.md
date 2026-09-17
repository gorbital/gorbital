# API keys and service accounts

Programs call a Full app's API with API keys: a script or CI job acting as a person, or a **service account**, a non-human principal with its own roles. Keys are shown once, stored only as hashes, always expire, can be limited to some permissions, and are sent as bearer tokens. Decision: [ADR-0058](../adr/0058-api-keys-and-service-accounts.md).

## Quick start

A personal key for a script, from a signed-in session (a bearer token or the browser's cookie):

```bash
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"your password","transport":"bearer"}' | jq -r .token)

# The key is in the response once: store it in your secret manager now.
KEY=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/api-keys -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Nightly export","expires_at":"2026-12-01T00:00:00Z","password":"your password"}' | jq -r .key)

curl -H "Authorization: Bearer $KEY" http://127.0.0.1:8080/v1/projects   # multi-tenant apps: /v1/orgs/{orgId}/projects
```

A key looks like `gbk_mfrggzdfmztwq2lknnwg23tpob_<52 characters>`. The part before the second underscore is its **prefix**: listed with the key, safe to log, useful to recognise a key. The rest is the secret.

## What a key can do

| | Personal key | Platform service account's key | Organisation service account's key (multi-tenant apps) |
|---|---|---|---|
| Acts as | The user (`actor.KindUser`) | The service account (`actor.KindService`) | The service account, in its organisation only |
| Permissions | The user's current roles and the `user` role every user holds | The service account's platform roles | Its organisation role, through `orgs.RequireMember` |
| Never | Permissions of roles that require two-factor authentication, so never `/ops`; joining or leaving organisations | The same | The same; organisation management (members, invitations, renaming, deleting) |
| Limited to | Its scopes, when it has any | Its scopes | Its scopes |
| Stops working when | Revoked, expired, the account is deleted, or the password is reset | Revoked, expired, the service account is disabled or deleted | The same, or the organisation is deleted |

- **Scopes** are permission names. A key without scopes gets every permission its owner holds (without two-factor authentication), now and later; a key with scopes never gets more than them. Scopes must be permissions the owner holds when the key is created: a user's platform permissions, or any organisation permission (a user's role differs between organisations and is checked on every request); for a service account, what its roles grant.
- **Scopes cover every operation a key can reach.** What any signed-in user may do without a granted role is a permission of the `user` role, which every user holds and sessions always have: a user's own resources (`projects.project.read` and `.write` in a single-tenant app, and every `orb gen resource --scope user` resource), and in multi-tenant apps creating and listing organisations (`orgs.org.create`, `orgs.org.list`). A key scoped to `projects.project.read` lists and reads projects, and gets 403 `forbidden` creating, changing or deleting one, or creating an organisation. Operations a key may never do answer `session_required` whatever its scopes (next point).
- **Keys can't manage accounts or memberships.** A request with a key gets 403 `session_required` from `/v1/auth/me`, sessions, passwords, two-factor authentication, passkeys, linked sign-ins, account deletion, API keys and service accounts, and, in multi-tenant apps, from accepting an invitation and leaving an organisation (also removing yourself). A leaked key can't make itself permanent, take the account over, widen what it reaches by joining organisations, or take the person out of one. Managing *other* members, renaming, deleting and restoring an organisation are organisation permissions, so a key does them only when its owner's role and its scopes allow.
- **Permissions are read on every request**, so removing a role, disabling a service account or revoking a key applies to the next request.
- **Two-factor authentication:** a key can't sign in with a second factor, so a role that requires it grants a key nothing, and such roles can't be given to service accounts. In the Full apps the ops roles require it, so `/ops` stays for people. The reasoning is in ADR-0058.

## Personal keys

| Method and path | Purpose | Success |
|---|---|---|
| `GET /v1/auth/api-keys` | Your keys, newest first, with `status` (`active`, `expired`, `revoked`) and `last_used_at`; expired and revoked ones for 30 days | 200 `{api_keys}` |
| `POST /v1/auth/api-keys` | `{name, expires_at, scopes?, password?}` | 201 `{api_key, key}` with `Cache-Control: no-store` |
| `DELETE /v1/auth/api-keys/{id}` | Revoke a key at once | 204 |

Creating a key needs a session (not a key), a verified email address, and your password, unless the session verified a second factor in the last 10 minutes; with two-factor authentication on, the session must have verified one. It spends the same per-user budget as other changes that check the password (`auth.reauth_attempts`). Each person, and each service account, can have 20 usable keys.

Resetting the password (the recovery path) revokes every key of the account; changing it while signed in doesn't. Proving an address by email revokes the keys of whoever had the account before, like its sessions.

## Service accounts

### Platform service accounts (`/ops/service-accounts`)

For integrations of the whole platform. Permissions `ops.service_accounts.read` (`ops_viewer`, `platform_admin`) and `ops.service_accounts.write` (`platform_admin`), with a session signed in with two-factor authentication like every `/ops` call.

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/service-accounts` | List | 200 `{service_accounts}` |
| `POST /ops/service-accounts` | `{name, description?, roles?}` | 201 |
| `GET /ops/service-accounts/{id}` | Read | 200 |
| `PATCH /ops/service-accounts/{id}` | `{name?, description?, roles?, disabled?}`. Disabling revokes every key at once; enabling again doesn't bring them back | 200 |
| `DELETE /ops/service-accounts/{id}` | Delete with its keys | 204 |
| `GET /ops/service-accounts/{id}/keys` | Its keys | 200 `{api_keys}` |
| `POST /ops/service-accounts/{id}/keys` | `{name, expires_at, scopes?, password?}` | 201 `{api_key, key}` |
| `DELETE /ops/service-accounts/{id}/keys/{keyId}` | Revoke | 204 |

The example apps' platform roles all require two-factor authentication, so a platform service account can't hold them. Declare a role for what the integration does in `internal/app/permissions.go`, without `RequireMFA`:

```go
c.Permission("reports.report.read", "Read reports")
c.Role("report_reader", "Reads reports; can be given to service accounts", "reports.report.read")
```

In an app on [`gorbital.Main`](main-go.md), sign-in is `authhttp` and there is no `permissions.go`: the role is a role name in one of your module's permissions, and it doesn't require a second factor:

```go
Permissions: []gorbital.Permission{
	{Name: "reports.report.read", Description: "Read reports", Roles: []string{"report_reader"}},
},
```

### Organisation service accounts (`/v1/orgs/{orgId}/service-accounts`)

In multi-tenant apps, an organisation's owners and admins (permission `orgs.service_accounts.manage`) manage its service accounts with the same operations under `/v1/orgs/{orgId}/service-accounts`, giving each one `role` instead of `roles`. The role must be one they could give a member, and never `owner` or a role that requires two-factor authentication; an admin can't change, delete or create keys for a service account whose role they couldn't give.

An organisation service account's key acts in its organisation's org-scoped modules with its role, like a member: `GET /v1/orgs/{orgId}/projects` works; other organisations answer 404 `org_not_found`, and so do the organisation's own management operations. Deleting the organisation stops its keys; purging it deletes its service accounts.

## Limits and settings

| Setting | Default | Range | Reason required |
|---|---|---|---|
| `auth.api_key_max_ttl` | 90 days | 1 – 365 days | Yes |
| `auth.api_key_failures_per_minute` (group `rate_limits`) | 30 | 5 – 10 000 | Yes |

- Every key needs an `expires_at` at least an hour away and within `auth.api_key_max_ttl`; the library also caps it at a year.
- Requests with a malformed, unknown or wrong key count toward `auth.api_key_failures_per_minute` per client network (an IPv4 address or IPv6 /64), shared by every instance; then 429 `too_many_attempts` with `Retry-After`. Valid keys aren't limited by it, and neither are revoked or expired ones (they aren't guesses). Behind a load balancer, set `APP_TRUSTED_PROXIES`.
- `last_used_at` is updated at most once a minute per key.
- The `auth_cleanup` job records each key's expiry as `auth.api_key.expired` and deletes keys expired or revoked more than 30 days ago.

## Security properties

| Property | How |
|---|---|
| Secrets never stored | Only `SHA-256(key)` in `auth_api_keys.secret_hash`; the 256-bit secret makes a fast hash enough |
| Looked up without timing leaks | Found by lookup ID (the prefix), then compared with `subtle.ConstantTimeCompare`; an unknown lookup ID does the same comparison |
| Never a session | `auth.Middleware` sends `gbk_` bearer tokens only to the API key authenticator, ignores them in cookies, and session tokens never start with `gbk_` |
| Never logged | Logs and audit events carry the lookup ID, never the key or its hash; the creation response has `Cache-Control: no-store` |
| No escalation | Permissions come from the owner's current roles, without roles requiring 2FA, intersected with the key's scopes, in `auth.Principal.Restrict`, which `auth.WithPrincipal` and `orgs.Authorize` both apply |
| Scoped to one organisation | An organisation service account's principal names its organisation; `orgs.Authorize` refuses any other, and its role is read with the organisation's ID |

## In your own code

A use case can't tell a key from a session unless it asks, and usually shouldn't: check permissions with `actor.Require`. **Every operation a signed-in user can call must check a permission**, or a key's scopes don't limit it: for something any user may do, declare a permission, give it to the `user` role in `internal/app/permissions.go` (`authusecase.RoleUser`; in an app on `gorbital.Main`, `Roles: []string{"user"}` in the module's permission), and check it; generated user-scoped resources do this through `userResourcePermissions`. When an operation must need a person, refuse keys explicitly:

```go
if p, ok := authlib.PrincipalFrom(ctx); ok && p.APIKey() {
	return ErrSessionRequired
}
```

An org-scoped module gets service accounts for free by passing `orgs.Service().Memberships()` to `orgs.RequireMember`, as generated resources do. A module that checks `actor.KindUser` itself (such as single-tenant projects) answers 401 to service accounts.

## Error codes

| Code | Status | When |
|---|---|---|
| `session_required` | 403 | An API key used for account, session, key or service account management, or to accept an invitation or leave an organisation |
| `api_key_not_found` | 404 | Revoking a key that isn't yours, or the service account's |
| `invalid_api_key_name` | 422 | Name empty, over 100 characters or on several lines |
| `invalid_api_key_expiry` | 422 | `expires_at` missing, within an hour or beyond `auth.api_key_max_ttl` |
| `invalid_api_key_scopes` | 422 | A scope the owner doesn't hold without two-factor authentication, or more than 50 |
| `api_key_limit_reached` | 409 | 20 usable keys already |
| `service_account_not_found` | 404 | No such service account here (another organisation's, or the platform's, included) |
| `invalid_service_account` | 422 | Name or description invalid |
| `invalid_service_account_role` | 422 | Undeclared role, a role that requires 2FA, `owner`, a role above yours, or not exactly one organisation role |
| `service_account_limit_reached` | 409 | 100 service accounts already (per organisation, or on the platform) |
| `service_account_disabled` | 409 | Creating a key for a disabled service account |
| `too_many_attempts` | 429 | Too many wrong keys from this network |
| `forbidden` | 403 | A key used for an operation outside its scopes |
| `forbidden`, `mfa_required` | 403 | Managing platform service accounts without the permission, or without two-factor authentication |

## Audit events

`auth.api_key.created`, `auth.api_key.revoked` (metadata `reason`: `revoked_by_owner`, `revoked_by_admin`, `service_account_disabled`, `account_deleted`, `password_reset`; bulk revocations carry `count`), `auth.api_key.expired` (recorded by `auth_cleanup`), `auth.service_account.created`, `.updated`, `.disabled` and `.deleted`. Key events carry the owner, lookup ID, scopes and expiry; organisation events carry `org_id`. Requests made with a key are recorded with the key's user or service account as the actor.

## Troubleshooting

| Symptom | Fix |
|---|---|
| 401 with a key | It's revoked, expired, its owner is deleted or disabled, or it was copied wrongly. `GET /v1/auth/api-keys` shows its status; logs show `API key refused` with the lookup ID and reason |
| 403 `session_required` | That operation needs a person signed in; use a session token |
| 403 `forbidden` on `/ops` with a key | By design: ops roles require two-factor authentication |
| 422 `invalid_service_account_role` for `platform_admin` | Declare a role without `RequireMFA` for the integration |
| 429 `too_many_attempts` from CI | Something sends a wrong key repeatedly from your network; fix it, or raise `auth.api_key_failures_per_minute` |

# Permissions and roles

<!-- Generated from examples/full-multi and examples/full-single by `go run -C internal/tools/refdocs . -write`. Don't edit: change the code, or internal/tools/refdocs/descriptions.json. -->

Access is denied by default: a user holds only the permissions of their roles. Permission and role names are public API ([Stability](../guides/stability.md)); they are declared in `internal/app/permissions.go`. How checks work: [Authentication](../guides/authentication.md).

A role that requires two-factor authentication grants its permissions only to sessions signed in with a second factor; other sessions get `403 mfa_required`.

## Platform roles

Platform roles are held across the whole app; the ops roles grant access to `/ops`. Every user holds the `user` role without a grant: it covers what they do with their own data and outside organisation roles, so an API key's scopes limit that too ([API keys](../guides/api-keys.md)). Give and take the other roles with `go run ./cmd/api grant-role <email> <role>` and `revoke-role`; list them with `go run ./cmd/api roles`. `orb gen resource --scope user` adds `<resource>.<resource>.read` and `.write` permissions, granted to the `user` role.

| Role | Description | Requires two-factor authentication |
|---|---|---|
| `user` | Held by every signed-in user without a grant; by API keys only within their scopes. | no |
| `platform_admin` | Operates the platform: every /ops permission. | yes |
| `ops_viewer` | Reads operational data without changing anything. | yes |

## Platform permissions

| Permission | Description | `user` | `platform_admin` | `ops_viewer` |
|---|---|---|---|---|
| `ops.settings.read` | Read runtime settings and their history. | | yes | yes |
| `ops.settings.write` | Change and reset runtime settings. | | yes | |
| `ops.flags.read` | Read feature flags and their history. | | yes | yes |
| `ops.flags.write` | Change and reset feature flags. | | yes | |
| `ops.jobs.read` | Read job definitions, runs and queues. | | yes | yes |
| `ops.jobs.write` | Change job configuration; pause and resume queues. | | yes | |
| `ops.jobs.run` | Run, retry and cancel jobs. | | yes | |
| `ops.audit.read` | Read the audit log. | | yes | yes |
| `ops.releases.read` | Read releases and the instances running them. | | yes | yes |
| `ops.mail.read` | See how the app sends email. | | yes | yes |
| `ops.mail.test` | Send a test email. | | yes | |
| `ops.mail.write` | Remove addresses from the email suppression list. | | yes | |
| `ops.auth.read` | See which sign-in methods are configured. | | yes | yes |
| `ops.system.read` | See an instance's health checks, database pool, migrations and runtime. | | yes | yes |
| `ops.service_accounts.read` | See service accounts and their API keys. | | yes | yes |
| `ops.service_accounts.write` | Create, change and delete service accounts and their API keys. | | yes | |
| `orgs.org.create` | Create organisations. *Multi-tenant apps only.* | yes | | |
| `orgs.org.list` | See the organisations you belong to. *Multi-tenant apps only.* | yes | | |
| `flags.flag.read` | Read the feature flags shown to clients. | yes | | |
| `projects.project.read` | See your projects. *Single-tenant apps only.* | yes | | |
| `projects.project.write` | Create, change and delete your projects. *Single-tenant apps only.* | yes | | |

## Organisation roles

*Multi-tenant apps only.* Every member of an organisation has exactly one of these roles in it, and it grants permissions only in that organisation. Platform roles never grant them. `orb gen resource --scope org` adds `<resource>.<resource>.read` and `.write` permissions, granted to every role.

| Role | Description | Requires two-factor authentication |
|---|---|---|
| `owner` | Everything, including deleting the organisation and managing owners. | no |
| `admin` | Manages the organisation and its members, except owners. | no |
| `member` | Works in the organisation. | no |

## Organisation permissions

| Permission | Description | `owner` | `admin` | `member` |
|---|---|---|---|---|
| `orgs.org.read` | See the organisation. | yes | yes | yes |
| `orgs.org.update` | Rename the organisation. | yes | yes | |
| `orgs.org.delete` | Delete and restore the organisation. | yes | | |
| `orgs.members.read` | See the members. | yes | yes | yes |
| `orgs.members.manage` | Invite people, change roles and remove members, up to your own role. | yes | yes | |
| `orgs.settings.read` | See the organisation's settings and their history. | yes | yes | yes |
| `orgs.settings.write` | Change the organisation's settings. | yes | yes | |
| `orgs.service_accounts.manage` | Create and manage service accounts and their API keys, up to your own role. | yes | yes | |
| `projects.project.read` | See projects. | yes | yes | yes |
| `projects.project.write` | Create, change and delete projects. | yes | yes | yes |

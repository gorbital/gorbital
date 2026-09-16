# Audit actions

<!-- Generated from examples/full-multi and examples/full-single by `go run -C internal/tools/refdocs . -write`. Don't edit: change the code, or internal/tools/refdocs/descriptions.json. -->

Security-relevant changes are recorded as audit events in PostgreSQL and read through `GET /ops/audit` ([Ops API](../guides/ops-api.md#audit-log)). Action names are public API: filters, alerts and dashboards depend on them, so they are added, never renamed or removed ([Stability](../guides/stability.md)).

Every event has `occurred_at`, `actor_kind` (`user`, `service`, `system` for jobs and commands, or `anonymous`) and `actor_id`, `action`, `outcome`, `resource_type` and `resource_id`, `org_id` in multi-tenant apps, `request_id`, `trace_id`, `ip` and `user_agent` when there is a request, and `metadata`. The metadata keys below are those the code sets; the recorder redacts sensitive keys and bounds the size, and events never hold passwords, tokens or secrets.

## Authentication

| Action | Recorded when | Metadata |
|---|---|---|
| `auth.account.deleted` | A user deleted their account (`DELETE /v1/auth/me`). The account is kept for `auth.deleted_account_retention`, then purged. |  |
| `auth.accounts.purged` | The `auth_cleanup` job removed accounts deleted longer ago than `auth.deleted_account_retention`. | `count` |
| `auth.accounts.unverified_expired` | The `auth_cleanup` job deleted accounts whose email address was never verified within `auth.unverified_account_ttl`. | `count` |
| `auth.api_key.created` | A user created a personal API key, or an operator or organisation admin created a key for a service account. Records the key's ID, owner and scopes, never the key. |  |
| `auth.api_key.expired` | The `auth_cleanup` job found API keys past their expiry. Records the count. |  |
| `auth.api_key.revoked` | An API key was revoked: by its owner, by an operator or organisation admin, or because its user reset their password, deleted their account or its service account was disabled. | `count`, `reason` |
| `auth.email.verified` | A user verified their email address with the emailed code. |  |
| `auth.identity.apple_notification` | Apple sent a server-to-server notification about a linked Apple account, such as consent revoked or the account deleted. | `before_link`, `known`, `replayed`, `sessions_ended`, `type` |
| `auth.identity.linked` | A Google or Apple account was linked: at sign-up through the provider, or by a signed-in user. | `identity_id`, `new_account`, `provider`, `signed_in` |
| `auth.identity.revocation_abandoned` | The `auth_revoke_tokens` job gave up revoking a provider's tokens after its attempts. | `attempts`, `provider`, `revocation_id` |
| `auth.identity.unlinked` | A user unlinked a Google or Apple account. | `identity_id`, `provider` |
| `auth.keys.rotated` | `rotate-auth-keys` re-encrypted two-factor secrets with the first `AUTH_ENCRYPTION_KEYS` key. | `key_id`, `secrets` |
| `auth.login.failed` | A sign-in failed: wrong password, unknown account, unverified provider email, invalid passkey and so on. `reason` says which. | `provider`, `reason` |
| `auth.login.succeeded` | A user signed in and a session was created. | `method`, `mfa_method`, `session_id` |
| `auth.mfa.challenge_failed` | A second factor at sign-in was wrong, used or expired. | `reason` |
| `auth.mfa.challenge_succeeded` | A user passed the second-factor step of sign-in. | `method` |
| `auth.mfa.recovery_code_used` | A recovery code was used as the second factor; the user is emailed. | `remaining` |
| `auth.mfa.recovery_codes_regenerated` | A user replaced their recovery codes. |  |
| `auth.mfa.reset` | An operator turned off a user's two-factor authentication with `reset-mfa`. |  |
| `auth.mfa.totp_disabled` | A user turned off authenticator app two-factor authentication. |  |
| `auth.mfa.totp_enabled` | A user confirmed authenticator app setup; two-factor authentication is on. | `by_operator` |
| `auth.mfa.totp_enrollment_started` | A user started authenticator app setup. |  |
| `auth.passkey.clone_warning` | A passkey's signature counter didn't increase, which suggests a cloned authenticator; the ceremony was refused. | `passkey_id` |
| `auth.passkey.registered` | A user added a passkey. | `backed_up`, `passkey_id` |
| `auth.passkey.removed` | A user removed a passkey. | `passkey_id` |
| `auth.passkey.renamed` | A user renamed a passkey. | `passkey_id` |
| `auth.password.changed` | A signed-in user changed their password. |  |
| `auth.password.reset` | A user set a new password with a reset code; their sessions end. |  |
| `auth.password.reset_requested` | A password reset code was requested for an existing account. |  |
| `auth.reauth.failed` | A signed-in user's password or second factor was wrong when confirming a sensitive change. | `reason` |
| `auth.role.granted` | An operator gave a user a platform role with `grant-role`. | `role` |
| `auth.role.revoked` | An operator took a platform role away with `revoke-role`. | `role` |
| `auth.service_account.created` | An operator (platform) or organisation owner or admin (organisation) created a service account. | `roles` |
| `auth.service_account.deleted` | A service account was deleted; its keys stop working. |  |
| `auth.service_account.disabled` | A service account was disabled; its keys are revoked permanently. | `revoked_keys` |
| `auth.service_account.updated` | A service account's name, description or roles changed. | `changed`, `roles` |
| `auth.session.revoked` | One session ended: logout, or the user revoked it from their session list. | `reason`, `user_id` |
| `auth.sessions.revoked` | A user signed out everywhere (logout-all). | `count`, `reason` |
| `auth.user.created` | An operator or seed data created an account (`CreateUser`), not through sign-up. | `email_verified` |
| `auth.user.registered` | Someone signed up, with a password or through Google or Apple. | `provider` |

## flags

| Action | Recorded when | Metadata |
|---|---|---|
| `flags.flag.changed` | An operator changed a feature flag's state through `PUT /ops/flags/{key}`, with a reason. The metadata summarises the new state (enabled, default, percentage, how many organisations and users are targeted); the IDs themselves are in the flag's history. | `default`, `enabled`, `org_targets`, `percentage`, `reason`, `user_targets`, `version` |
| `flags.flag.reset` | An operator reset a feature flag to the state declared in code through `DELETE /ops/flags/{key}`, with a reason. | `reason`, `version` |

## Background jobs

| Action | Recorded when | Metadata |
|---|---|---|
| `jobs.definition.changed` | An operator changed or reset a job's configuration through `/ops/jobs/definitions/{name}`. | `action`, `reason`, `version` |
| `jobs.definition.run_requested` | An operator ran a job now through `/ops/jobs/definitions/{name}/run`. | `job_id` |
| `jobs.queue.paused` | An operator paused a queue. |  |
| `jobs.queue.resumed` | An operator resumed a queue. |  |
| `jobs.run.cancelled` | An operator cancelled a job run. | `kind`, `state` |
| `jobs.run.retried` | An operator retried a job run. | `kind`, `state` |

## Email

| Action | Recorded when | Metadata |
|---|---|---|
| `mail.suppression.added` | A signed Resend webhook reported a hard bounce or complaint, and the address was added to the suppression list. The event records the reason and suppression ID, never the address. | `delivery_id`, `detail`, `reason`, `source` |
| `mail.suppression.removed` | An operator removed an address from the suppression list through `DELETE /ops/mail/suppressions/{id}`, with a reason. | `reason`, `source`, `suppression_reason` |
| `mail.test.requested` | An operator sent a test email through `POST /ops/mail/test`. | `delivery`, `provider` |

## Organisations

*Multi-tenant apps only.*

| Action | Recorded when | Metadata |
|---|---|---|
| `orgs.invitation.accepted` | An invited person accepted an invitation. |  |
| `orgs.invitation.created` | A member invited someone to the organisation. | `role` |
| `orgs.invitation.resent` | A member resent an invitation, with a new link. |  |
| `orgs.invitation.revoked` | A member revoked an invitation. |  |
| `orgs.member.added` | Someone joined the organisation, by accepting an invitation. | `invitation_id`, `role` |
| `orgs.member.left` | A member left the organisation. | `role` |
| `orgs.member.removed` | A member was removed, by another member or because their account was deleted. | `reason`, `role` |
| `orgs.member.role_changed` | A member's role changed. | `from`, `reason`, `to` |
| `orgs.org.created` | An organisation was created, including each user's personal workspace (`personal`). | `personal` |
| `orgs.org.deleted` | An organisation was deleted; it can be restored until `purge_after`. | `purge_after` |
| `orgs.org.purged` | The `orgs_purge` job removed an organisation deleted longer ago than `orgs.deleted_org_retention`, with its data. |  |
| `orgs.org.renamed` | An organisation was renamed. |  |
| `orgs.org.restored` | A deleted organisation was restored. |  |

## Projects (example resource)

| Action | Recorded when | Metadata |
|---|---|---|
| `projects.project.created` | A project was created. The example resource: resources made with `orb gen resource` record the same three actions. |  |
| `projects.project.deleted` | A project was deleted. |  |
| `projects.project.updated` | A project was changed; `fields` lists which. | `fields` |

## Retention

| Action | Recorded when | Metadata |
|---|---|---|
| `retention.purged` | The `retention` job deleted data older than its retention setting, one event per kind of data. | `before`, `rows` |

## Runtime settings

| Action | Recorded when | Metadata |
|---|---|---|
| `settings.value.changed` | An operator changed or reset a runtime setting through `/ops/settings/{key}`; `reason` is required for security-relevant settings. | `org_id`, `reason`, `reset`, `version` |

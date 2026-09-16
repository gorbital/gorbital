# Error codes

<!-- Generated from examples/full-multi and examples/full-single by `go run -C internal/tools/refdocs . -write`. Don't edit: change the code, or internal/tools/refdocs/descriptions.json. -->

Every error response is `application/problem+json` with a stable `code` clients can branch on; the `detail` text may change. Codes are public API: they are added, never renamed or removed ([Stability](../guides/stability.md)). How errors become responses, and how to add one: [Error handling](../guides/error-handling.md).

These are the codes of a Full app as generated, including the example `projects` resource and the generic codes any status without its own code gets. Resources you add with `orb gen resource` add `<resource>_not_found`, `<resource>_version_conflict` and, for unique fields, `<resource>_<field>_taken`.

| Code | HTTP status | Meaning | Where |
|---|---|---|---|
| `already_invited` | 409 | This address already has an open invitation; resend it instead. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `already_member` | 409 | This person is already a member. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `api_key_limit_reached` | 409 | At most 20 usable API keys each; revoke one first. | `/v1/auth` |
| `api_key_not_found` | 404 | No API key here has this ID. | `/v1/auth` |
| `audit_event_not_found` | 404 | No audit event has this ID. | `/ops` |
| `audit_query_timeout` | 503 | The audit query took too long; narrow the filters or the time range. | `/ops` |
| `auth_unavailable` | 503 | Authentication is temporarily unavailable, for example when every password hashing slot is busy; try again shortly. | Any endpoint |
| `bad_request` | 400 | The request is malformed, such as a body that isn't valid JSON. | Any endpoint |
| `conflict` | 409 | The request conflicts with the current state, when no more specific code applies. | Any endpoint |
| `cross_origin_request_denied` | 403 | A cookie-authenticated, state-changing request came from an origin that isn't allowed (cross-site request forgery protection). | Any endpoint |
| `email_not_verified` | 403 | The account's email address must be verified first: to sign in, or to create organisations and send invitations. | `/v1/auth`; `/v1/orgs`, `/v1/invitations` |
| `error` | any other 4xx | Any other 4xx status without its own code. | Any endpoint |
| `forbidden` | 403 | The caller is signed in but lacks the permission: a platform role for `/ops`, or an organisation role for org-scoped endpoints. | Any endpoint |
| `idempotency_in_progress` | 409 | A request with this idempotency key is still in progress; retry later. | Any endpoint |
| `idempotency_key_reused` | 422 | This idempotency key was used for a different request; use a new key for a new request. | Any endpoint |
| `identity_in_use` | 409 | This Google, Apple or GitHub account is linked to another account. | `/v1/auth` |
| `identity_not_found` | 404 | No linked account of yours has this ID. | `/v1/auth` |
| `internal_error` | 500, any other 5xx | An unexpected error. The detail never includes the cause; it is logged once with the request ID. | Any endpoint |
| `invalid_api_key_expiry` | 422 | expires_at must be at least an hour away and within auth.api_key_max_ttl. | `/v1/auth` |
| `invalid_api_key_name` | 422 | An API key name must be 1 to 100 characters on one line. | `/v1/auth` |
| `invalid_api_key_scopes` | 422 | Scopes must be at most 50 permissions the key's owner holds without two-factor authentication. | `/v1/auth` |
| `invalid_audit_filter` | 422 | The audit filter is not valid. | `/ops` |
| `invalid_code` | 422 | The code is wrong, used or expired. | `/v1/auth` |
| `invalid_credentials` | 401 | The email address or password is wrong. | `/v1/auth` |
| `invalid_cursor` | 400 | The cursor is not valid. | List endpoints |
| `invalid_email` | 422 | The email address is not valid. | `/v1/auth` |
| `invalid_idempotency_key` | 400 | Send one Idempotency-Key header of 1 to 255 visible ASCII characters. | Any endpoint |
| `invalid_job_config` | 422 | The job configuration is outside the allowed bounds, such as a schedule more often than once a minute or a timeout that isn't a duration. | `/ops` |
| `invalid_job_state` | 422 | State must be one of available, cancelled, completed, discarded, pending, retryable, running, scheduled. | `/ops` |
| `invalid_limit` | 400 | Limit must be between 1 and 100. | List endpoints |
| `invalid_mfa` | 401 | The second factor is wrong or already used, or the sign-in expired. | `/v1/auth` |
| `invalid_org_name` | 422 | An organisation name must be 1 to 100 characters on one line. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `invalid_passkey` | 401 | The passkey couldn't be verified, or the ceremony was used or expired. | `/v1/auth` |
| `invalid_passkey_name` | 422 | A passkey name must be 1 to 100 characters. | `/v1/auth` |
| `invalid_recipient` | 422 | The recipient is not an email address. | `/ops` |
| `invalid_return_to` | 422 | return_to must be an absolute URL on the API's origin or APP_CORS_ORIGINS. | `/v1/auth` |
| `invalid_service_account` | 422 | A service account name must be 1 to 100 characters on one line, and its description at most 500. | `/v1/auth` |
| `invalid_service_account_role` | 422 | Service accounts can't hold roles that require two-factor authentication or an organisation's owner role, nor a role above your own. | `/v1/auth` |
| `invalid_setting_value` | 422 | The value isn't valid JSON, or fails the setting's type or constraints. | `/ops`; `/v1/orgs`, `/v1/invitations` |
| `invalid_social_token` | 401 | The sign-in with Google, Apple or GitHub couldn't be verified; start again. | `/v1/auth` |
| `invalid_sort` | 400 | Sort by one allowed field, with - for descending order. | List endpoints |
| `invalid_state` | 401 | The sign-in expired or was started in another browser; start again. | `/v1/auth` |
| `invalid_webhook_payload` | 400 | The webhook body is not an event. | `mailevents` module |
| `invalid_webhook_signature` | 401 | The webhook signature is missing, invalid or too old. | `mailevents` module |
| `invitation_for_another_email` | 403 | The invitation was sent to another address; sign in with the invited, verified address. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `invitation_not_found` | 404 | The invitation doesn't exist, was used or revoked, or expired. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `job_definition_disabled` | 409 | The job is disabled. | `/ops` |
| `job_definition_not_found` | 404 | No job definition has this name. | `/ops` |
| `job_definition_version_conflict` | 409 | The job definition changed since it was read; read it again. | `/ops` |
| `job_not_found` | 404 | No job has this ID. | `/ops` |
| `job_not_retryable` | 409 | Only runs waiting to retry, discarded or cancelled can be retried. | `/ops` |
| `job_reason_required` | 422 | A reason is required to disable or reschedule a job, change its timeout, attempts or queue, or pause a queue. | `/ops` |
| `job_run_limited` | 429 | The job is queued or running, or ran less than a minute ago. | `/ops` |
| `last_owner` | 409 | An organisation needs at least one owner; make another member an owner first. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `last_sign_in_method` | 409 | This is the account's last way to sign in; set a password or add a passkey first. | `/v1/auth` |
| `mail_suppression_not_found` | 404 | No suppression has this ID. | `/ops` |
| `mail_suppression_reason_required` | 422 | A reason is required to remove a suppressed address. | `/ops` |
| `maintenance` | 503 | Maintenance mode is on (`maintenance.enabled`). The detail is `maintenance.message`; `Retry-After` is `maintenance.retry_after`. | Any endpoint |
| `member_not_found` | 404 | The organisation has no member with this ID. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `message_required` | 422 | The example echo endpoint got a blank message. | `/v1/ping`, `/v1/echo` |
| `method_not_allowed` | 405 | The path exists but not with this HTTP method. | Any endpoint |
| `mfa_already_enabled` | 409 | Two-factor authentication is already on. | `/v1/auth` |
| `mfa_not_enabled` | 409 | Two-factor authentication isn't on, or its setup wasn't started. | `/v1/auth` |
| `mfa_required` | 403 | The permission needs a session signed in with a second factor: turn on two-factor authentication and sign in again. | `/v1/auth`; `/ops`; `/v1/orgs`, `/v1/invitations` |
| `mfa_required_by_role` | 409 | A role of this account requires two-factor authentication. | `/v1/auth` |
| `mfa_unavailable` | 503 | Two-factor authentication isn't configured on this server. | `/v1/auth` |
| `not_found` | 404 | No route matches the method and path, or a resource wasn't found and no more specific code applies. | Any endpoint |
| `org_not_found` | 404 | You aren't a member of an organisation with this ID. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `org_version_conflict` | 409 | The organisation changed since you read it; get it again and retry. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `passkey_limit_reached` | 409 | An account can have at most 10 passkeys. | `/v1/auth` |
| `passkey_not_found` | 404 | No passkey of yours has this ID. | `/v1/auth` |
| `passkeys_unavailable` | 503 | Passkeys aren't configured on this server (WEBAUTHN_RP_ID). | `/v1/auth` |
| `personal_workspace` | 409 | A personal workspace can't be left, deleted or shared; create an organisation instead. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `project_name_taken` | 409 | The organisation already has a project with this name. | `/v1/orgs/{orgId}/projects` (`/v1/projects` in single-tenant apps) |
| `project_not_found` | 404 | The organisation has no project with this ID. | `/v1/orgs/{orgId}/projects` (`/v1/projects` in single-tenant apps) |
| `project_version_conflict` | 409 | The project changed since you read it; get it again and retry. | `/v1/orgs/{orgId}/projects` (`/v1/projects` in single-tenant apps) |
| `queue_not_active` | 422 | No worker runs this queue. | `/ops` |
| `rate_limited` | 429 | Too many requests from this client or for this operation; wait for `Retry-After`. | Any endpoint |
| `request_too_large` | 413 | The request body is larger than `APP_MAX_BODY_BYTES`. | Any endpoint |
| `role_not_allowed` | 403 | You can't give, change or remove a role above your own, and only owners manage owners. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `service_account_disabled` | 409 | The service account is disabled; enable it first. | `/v1/auth` |
| `service_account_limit_reached` | 409 | At most 100 service accounts; delete one first. | `/v1/auth` |
| `service_account_not_found` | 404 | No service account here has this ID. | `/v1/auth` |
| `session_not_found` | 404 | No active session of yours has this ID. | `/v1/auth` |
| `session_required` | 403 | Sign in to do this: an API key can't manage accounts, sessions or API keys. | `/v1/auth` |
| `setting_not_found` | 404 | No setting has this key. Organisations can't set a setting with this key. | `/ops`; `/v1/orgs`, `/v1/invitations` |
| `setting_reason_required` | 422 | A reason is required to change this setting. | `/ops` |
| `setting_version_conflict` | 409 | The setting changed since it was read; read it again. | `/ops` |
| `social_email_unverified` | 403 | The provider hasn't verified this email address. | `/v1/auth` |
| `social_link_required` | 403 | An account with this email address exists; sign in to it and link this provider from the account. | `/v1/auth` |
| `social_unavailable` | 503 | This sign-in provider isn't configured on this server (see AUTH_PROVIDERS.md). | `/v1/auth` |
| `sole_owner` | 409 | You are the only owner of organisations with other members; make another member an owner, or delete them, first. *Multi-tenant apps only.* | `DELETE /v1/auth/me` |
| `too_many_attempts` | 429 | Too many sign-in, code, second-factor or re-authentication attempts within the window; the detail says when to try again. | Any endpoint |
| `too_many_invitations` | 429 | Too many invitations were sent from this organisation, or by you, in the last hour; try again later. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `too_many_orgs` | 409 | You own as many organisations as allowed; delete one, or hand one over, first. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `unauthenticated` | 401 | The endpoint needs a signed-in session (cookie or bearer token). | `/v1/auth`; `/ops`; `/v1/orgs`, `/v1/invitations` |
| `unauthorized` | 401 | Authentication failed, when no more specific code applies. | Any endpoint |
| `unavailable` | 503 | The service or a dependency is temporarily unavailable. | Any endpoint |
| `unknown_role` | 422 | The role isn't one of the organisation roles. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `validation_failed` | 422 | The request doesn't match the operation's schema, or a resource's fields aren't valid. `errors` lists each field. | Any endpoint |
| `weak_password` | 422 | The password doesn't meet the password policy; the detail says why. | `/v1/auth` |
| `webhook_not_found` | 404 | This webhook isn't configured. | `mailevents` module |

# Error codes

<!-- Generated from examples/full-multi and examples/full-single by `go run -C internal/tools/refdocs . -write`. Don't edit: change the code, or internal/tools/refdocs/descriptions.json. -->

Every error response is `application/problem+json` with a stable `code` clients can branch on; the `detail` text may change. Codes are public API: they are added, never renamed or removed ([Stability](../guides/stability.md)). How errors become responses, and how to add one: [Error handling](../guides/error-handling.md).

These are the codes of a Full app as generated, including the example `projects` resource and the generic codes any status without its own code gets. Resources you add with `orb gen resource` add `<resource>_not_found`, `<resource>_version_conflict` and, for unique fields, `<resource>_<field>_taken`.

| Code | HTTP status | Meaning | Where |
|---|---|---|---|
| `account_banned` | 403 | An operator banned the account; it can't sign in until the ban is lifted (ADR-0070). | Every sign-in: password, second factor, passkey, Google, Apple, GitHub, and impersonation. |
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
| `email_taken` | 409 | An account already has this email address. | `/v1/auth` |
| `error` | any other 4xx | Any other 4xx status without its own code. | Any endpoint |
| `flag_not_found` | 404 | No feature flag has this key. | `/ops` |
| `flag_reason_required` | 422 | A reason is required to change a feature flag. | `/ops` |
| `flag_version_conflict` | 409 | The feature flag changed since it was read; read it again. | `/ops` |
| `forbidden` | 403 | The caller is signed in but lacks the permission: a platform role for `/ops`, an organisation role for org-scoped endpoints, or, for an API key, a scope. | Any endpoint |
| `idempotency_in_progress` | 409 | A request with this idempotency key is still in progress; retry later. | Any endpoint |
| `idempotency_key_reused` | 422 | This idempotency key was used for a different request; use a new key for a new request. | Any endpoint |
| `identity_in_use` | 409 | This Google, Apple or GitHub account is linked to another account. | `/v1/auth` |
| `identity_not_found` | 404 | No linked account of yours has this ID. | `/v1/auth` |
| `impersonation_off` | 403 | Impersonation exists only while the app runs with the dev console (`orb dev`), never in production (ADR-0070). | `POST /ops/auth/users/{id}/impersonate` outside development. |
| `incident_not_found` | 404 | No incident has this ID. | `/ops` |
| `incident_resolved` | 409 | The incident is resolved and can't change. | `/ops` |
| `incident_updates_limited` | 409 | The incident has the most updates allowed. | `/ops` |
| `internal_error` | 500, any other 5xx | An unexpected error. The detail never includes the cause; it is logged once with the request ID. | Any endpoint |
| `invalid_address` | 400 | The recipient isn't an email address. | Any endpoint |
| `invalid_api_key_expiry` | 422 | expires_at must be at least an hour away and within auth.api_key_max_ttl. | `/v1/auth` |
| `invalid_api_key_name` | 422 | An API key name must be 1 to 100 characters on one line. | `/v1/auth` |
| `invalid_api_key_scopes` | 422 | Scopes must be at most 50 permissions the key's owner holds without two-factor authentication. | `/v1/auth` |
| `invalid_audit_filter` | 422 | The audit filter is not valid. | `/ops` |
| `invalid_code` | 422 | The code is wrong, used or expired. | `/v1/auth` |
| `invalid_credentials` | 401 | The email address or password is wrong. | `/v1/auth` |
| `invalid_cursor` | 400 | The cursor is not valid. | List endpoints |
| `invalid_email` | 422 | The email address is not valid. | `/v1/auth` |
| `invalid_flag_state` | 422 | The state is not valid for a feature flag. | `/ops` |
| `invalid_idempotency_key` | 400 | Send one Idempotency-Key header of 1 to 255 visible ASCII characters. | Any endpoint |
| `invalid_incident` | 422 | The incident or update is not valid: check the title, severity, status, start time and message. | `/ops` |
| `invalid_job_config` | 422 | The job configuration is outside the allowed bounds, such as a schedule more often than once a minute or a timeout that isn't a duration. | `/ops` |
| `invalid_job_state` | 422 | State must be one of available, cancelled, completed, discarded, pending, retryable, running, scheduled. | `/ops` |
| `invalid_json` | 400 | The body must be a JSON object of at most 64 KiB. | `/_dev/auth/test/` (the dev console, development only) |
| `invalid_limit` | 400 | Limit must be between 1 and 100. | List endpoints |
| `invalid_mfa` | 401 | The second factor is wrong or already used, or the sign-in expired. | `/v1/auth` |
| `invalid_observability_window` | 422 | The window must be whole minutes from 1m to 24h, such as 15m. | `/ops` |
| `invalid_org_name` | 422 | An organisation name must be 1 to 100 characters on one line. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `invalid_passkey` | 401 | The passkey couldn't be verified, or the ceremony was used or expired. | `/v1/auth` |
| `invalid_passkey_name` | 422 | A passkey name must be 1 to 100 characters. | `/v1/auth` |
| `invalid_recipient` | 422 | The recipient is not an email address. | `/ops` |
| `invalid_request` | 422 | A Dev Portal sign-in test got a request it can't use, such as a callback for another provider or a step out of order (development only, ADR-0087). | `/_dev/auth/test/` (the dev console, development only) |
| `invalid_result_url` | 422 | A Dev Portal sign-in test's result page isn't the portal's own address (development only, ADR-0087). | `/_dev/auth/test/` (the dev console, development only) |
| `invalid_return_to` | 422 | return_to must be an absolute URL on the API's origin or APP_CORS_ORIGINS. | `/v1/auth` |
| `invalid_service_account` | 422 | A service account name must be 1 to 100 characters on one line, and its description at most 500. | `/v1/auth` |
| `invalid_service_account_role` | 422 | Service accounts can't hold roles that require two-factor authentication or an organisation's owner role, nor a role above your own. | `/v1/auth` |
| `invalid_setting_value` | 422 | The value isn't valid JSON, or fails the setting's type or constraints. | `/ops`; `/v1/orgs`, `/v1/invitations` |
| `invalid_social_token` | 401 | The sign-in with Google, Apple or GitHub couldn't be verified; start again. | `/v1/auth` |
| `invalid_sort` | 400 | Sort by one allowed field, with - for descending order. | List endpoints |
| `invalid_state` | 401 | The sign-in expired or was started in another browser; start again. | `/v1/auth` |
| `invalid_storage_key` | 422 | Keys are 1 to 1024 characters of path segments without ".", ".." or a leading slash. | `/ops` |
| `invalid_webhook_payload` | 400 | The webhook body is not an event. | `mailevents` module |
| `invalid_webhook_signature` | 401 | The webhook signature is missing, invalid or too old. | `POST /v1/webhooks/resend`, and routes with `guard.Webhook` |
| `invitation_for_another_email` | 403 | The invitation was sent to another address; sign in with the invited, verified address. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `invitation_not_found` | 404 | The invitation doesn't exist, was used or revoked, or expired. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `ip_not_allowed` | 403 | Requests from this network address are not allowed. | Any endpoint |
| `job_definition_disabled` | 409 | The job is disabled. | `/ops` |
| `job_definition_not_found` | 404 | No job definition has this name. | `/ops` |
| `job_definition_version_conflict` | 409 | The job definition changed since it was read; read it again. | `/ops` |
| `job_not_found` | 404 | No job has this ID. | `/ops` |
| `job_not_retryable` | 409 | Only runs waiting to retry, discarded or cancelled can be retried. | `/ops` |
| `job_reason_required` | 422 | A reason is required to disable or reschedule a job, change its timeout, attempts or queue, or pause a queue. | `/ops` |
| `job_run_limited` | 429 | The job is queued or running, or ran less than a minute ago. | `/ops` |
| `last_owner` | 409 | An organisation needs at least one owner; make another member an owner first. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `last_sign_in_method` | 409 | This is the account's last way to sign in; set a password or add a passkey first. | `/v1/auth` |
| `live_test_unavailable` | 409 | A live sign-in test can't run with this configuration; the detail says why (development only, ADR-0087). | `/_dev/auth/test/` (the dev console, development only) |
| `mail_suppression_not_found` | 404 | No suppression has this ID. | `/ops` |
| `mail_suppression_reason_required` | 422 | A reason is required to remove a suppressed address. | `/ops` |
| `maintenance` | 503 | Maintenance mode is on (`maintenance.enabled`). The detail is `maintenance.message`; `Retry-After` is `maintenance.retry_after`. | Any endpoint |
| `member_not_found` | 404 | The organisation has no member with this ID. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `message_required` | 422 | The example echo endpoint got a blank message. | `/v1/ping`, `/v1/echo` |
| `method_not_allowed` | 405 | The path exists but not with this HTTP method. | Any endpoint |
| `mfa_already_enabled` | 409 | Two-factor authentication is already on. | `/v1/auth` |
| `mfa_not_enabled` | 409 | Two-factor authentication isn't on, or its setup wasn't started. | `/v1/auth` |
| `mfa_required` | 403 | The permission needs a session signed in with a second factor: turn on two-factor authentication and sign in again. | Any endpoint |
| `mfa_required_by_role` | 409 | A role of this account requires two-factor authentication. | `/v1/auth` |
| `mfa_unavailable` | 503 | Two-factor authentication isn't configured on this server. | `/v1/auth` |
| `not_configured` | 404 | The sign-in method to test isn't configured (development only, ADR-0087). | `/_dev/auth/test/` (the dev console, development only) |
| `not_found` | 404 | No route matches the method and path, or a resource wasn't found and no more specific code applies. | Any endpoint |
| `observability_query_timeout` | 503 | The request counts took too long to read; try a shorter window. | `/ops` |
| `observability_streams_limited` | 429 | Too many open streams; close one or try another instance. | `/ops` |
| `org_not_found` | 404 | You aren't a member of an organisation with this ID. | Routes under `/v1/orgs/{orgId}/` (`guard.OrgMember`), `/v1/orgs`, `/v1/invitations`. *Multi-tenant apps only.* |
| `org_version_conflict` | 409 | The organisation changed since you read it; get it again and retry. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `passkey_limit_reached` | 409 | An account can have at most 10 passkeys. | `/v1/auth` |
| `passkey_not_found` | 404 | No passkey of yours has this ID. | `/v1/auth` |
| `passkeys_unavailable` | 503 | Passkeys aren't configured on this server (WEBAUTHN_RP_ID). | `/v1/auth` |
| `personal_workspace` | 409 | A personal workspace can't be left, deleted or shared; create an organisation instead. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `preview_not_found` | 404 | No email preview has this name; `GET /_dev/mail/previews` lists them (ADR-0074). | Any endpoint |
| `project_name_taken` | 409 | The organisation already has a project with this name. | `/v1/orgs/{orgId}/projects` (`/v1/projects` in single-tenant apps) |
| `project_not_found` | 404 | The organisation has no project with this ID. | `/v1/orgs/{orgId}/projects` (`/v1/projects` in single-tenant apps) |
| `project_version_conflict` | 409 | The project changed since you read it; get it again and retry. | `/v1/orgs/{orgId}/projects` (`/v1/projects` in single-tenant apps) |
| `queue_not_active` | 422 | No worker runs this queue. | `/ops` |
| `rate_limited` | 429 | Too many requests from this client or for this operation; wait for `Retry-After`. | Any endpoint |
| `rate_limiter_not_found` | 404 | The app has no rate limiter with this name, or the key was empty (ADR-0070). | `POST /ops/auth/rate-limits/reset`. |
| `reauthentication_required` | 403 | Sign in again, or verify your second factor, to continue. | Any endpoint |
| `registration_closed` | 403 | This app doesn't create accounts by signing up; ask for an invitation, or sign in to an existing account. | `/v1/auth` |
| `request_timeout` | 503 | The request took too long; try again later. | Any endpoint |
| `request_too_large` | 413 | The request body is larger than `APP_MAX_BODY_BYTES`. | Any endpoint |
| `role_not_allowed` | 403 | You can't give, change or remove a role above your own, and only owners manage owners. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `service_account_disabled` | 409 | The service account is disabled; enable it first. | `/v1/auth` |
| `service_account_limit_reached` | 409 | At most 100 service accounts; delete one first. | `/v1/auth` |
| `service_account_not_found` | 404 | No service account here has this ID. | `/v1/auth` |
| `session_not_found` | 404 | No active session of yours has this ID. | `/v1/auth` |
| `session_required` | 403 | The operation needs the person's signed-in session, not an API key: managing the account, sessions, API keys and service accounts, and joining or leaving organisations. | Any endpoint |
| `setting_not_found` | 404 | No setting has this key. Organisations can't set a setting with this key. | `/ops`; `/v1/orgs`, `/v1/invitations` |
| `setting_reason_required` | 422 | A reason is required to change this setting. | `/ops`; `/v1/orgs`, `/v1/invitations` |
| `setting_version_conflict` | 409 | The setting changed since it was read; read it again. | `/ops`; `/v1/orgs`, `/v1/invitations` |
| `social_email_unverified` | 403 | The provider hasn't verified this email address. | `/v1/auth` |
| `social_link_required` | 403 | An account with this email address exists; sign in to it and link this provider from the account. | `/v1/auth` |
| `social_unavailable` | 503 | This sign-in provider isn't configured on this server (see AUTH_PROVIDERS.md). | `/v1/auth` |
| `sole_owner` | 409 | You are the only owner of organisations with other members; make another member an owner, or delete them, first. *Multi-tenant apps only.* | `DELETE /v1/auth/me` |
| `storage_object_not_found` | 404 | No object has this key. | `/ops` |
| `storage_off` | 404 | The app has no file storage configured: set STORAGE_DRIVER (ADR-0075). | `/ops` |
| `storage_unavailable` | 503 | The storage service didn't answer: check the endpoint, bucket and keys. | `/ops` |
| `test_not_found` | 404 | No sign-in test has this ID, or it expired (development only, ADR-0087). | `/_dev/auth/test/` (the dev console, development only) |
| `too_many_attempts` | 429 | Too many sign-in, code, second-factor or re-authentication attempts within the window; the detail says when to try again. | Any endpoint |
| `too_many_invitations` | 429 | Too many invitations were sent from this organisation, or by you, in the last hour; try again later. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `too_many_orgs` | 409 | You own as many organisations as allowed; delete one, or hand one over, first. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |
| `too_many_tests` | 429 | Too many sign-in tests at once; wait for `Retry-After` (development only, ADR-0087). | `/_dev/auth/test/` (the dev console, development only) |
| `unauthenticated` | 401 | The endpoint needs a signed-in session (cookie or bearer token). | Any endpoint |
| `unauthorized` | 401 | Authentication failed, when no more specific code applies. | Any endpoint |
| `unavailable` | 503 | The service or a dependency is temporarily unavailable. | Any endpoint |
| `unknown_role` | 422 | No such role in the permission catalog. The role isn't one of the organisation roles. | `/v1/auth`; `/v1/orgs`, `/v1/invitations` |
| `user_not_found` | 404 | No account has this ID; deleted accounts are gone to operators too. | `/ops/auth/users/{id}` and its actions. |
| `validation_failed` | 422 | The request doesn't match the operation's schema, or a resource's fields aren't valid. `errors` lists each field. | Any endpoint |
| `weak_password` | 422 | The password doesn't meet the password policy; the detail says why. | `/v1/auth` |
| `webhook_not_found` | 404 | This webhook isn't configured. | `mailevents` module |

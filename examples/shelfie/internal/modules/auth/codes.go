package authhttp

// builtInCodes are the problem codes of gorbital, its built-in modules and
// sign-in, which an app's refusal can't use: every code of the v0.1.0 Full
// apps but their example modules' (TestRefusalCodesExcludeV010Codes),
// the generic code of each status (httpx.DefaultCode), and the codes added
// since (ip_not_allowed, request_timeout, reauthentication_required,
// invalid_webhook_signature, registration_closed). Add a built-in code here
// when one is added.
var builtInCodes = []string{
	"access_denied", "account_banned", "already_invited", "already_member", "api_key_limit_reached",
	"api_key_not_found", "audit_event_not_found", "audit_query_timeout", "auth_unavailable",
	"bad_request", "conflict", "cross_origin_request_denied", "email_not_verified", "email_taken",
	"error", "flag_not_found", "flag_reason_required", "flag_version_conflict", "forbidden",
	"idempotency_in_progress", "idempotency_key_reused", "identity_in_use", "identity_not_found",
	"impersonation_off", "incident_not_found", "incident_resolved", "incident_updates_limited",
	"internal_error", "invalid_address", "invalid_api_key_expiry", "invalid_api_key_name",
	"invalid_api_key_scopes", "invalid_audit_filter", "invalid_code", "invalid_credentials",
	"invalid_cursor", "invalid_email", "invalid_flag_state", "invalid_idempotency_key",
	"invalid_incident", "invalid_job_config", "invalid_job_state", "invalid_limit", "invalid_mfa",
	"invalid_observability_window", "invalid_org_name", "invalid_passkey", "invalid_passkey_name",
	"invalid_recipient", "invalid_return_to", "invalid_service_account",
	"invalid_service_account_role", "invalid_setting_value", "invalid_social_token", "invalid_sort",
	"invalid_state", "invalid_storage_key", "invalid_webhook_payload", "invalid_webhook_signature",
	"invitation_for_another_email", "invitation_not_found", "ip_not_allowed",
	"job_definition_disabled", "job_definition_not_found", "job_definition_version_conflict",
	"job_not_found", "job_not_retryable", "job_reason_required", "job_run_limited", "last_owner",
	"last_sign_in_method", "mail_suppression_not_found", "mail_suppression_reason_required",
	"maintenance", "member_not_found", "message_required", "method_not_allowed",
	"mfa_already_enabled", "mfa_not_enabled", "mfa_required", "mfa_required_by_role",
	"mfa_unavailable", "not_found", "observability_query_timeout", "observability_streams_limited",
	"org_not_found", "org_version_conflict", "passkey_limit_reached", "passkey_not_found",
	"passkeys_unavailable", "personal_workspace", "preview_not_found", "queue_not_active",
	"rate_limited", "rate_limiter_not_found", "reauthentication_required", "registration_closed",
	"request_timeout", "request_too_large", "role_not_allowed", "server_error",
	"service_account_disabled", "service_account_limit_reached", "service_account_not_found",
	"session_not_found", "session_required", "setting_not_found", "setting_reason_required",
	"setting_version_conflict", "social_email_unverified", "social_link_required",
	"social_unavailable", "sole_owner", "storage_object_not_found", "storage_off",
	"storage_unavailable", "too_many_attempts", "too_many_invitations", "too_many_orgs",
	"unauthenticated", "unauthorized", "unavailable", "unknown_role", "user_not_found",
	"validation_failed", "weak_password", "webhook_not_found",
}

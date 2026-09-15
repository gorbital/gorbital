# ADR-0029: Threat model: framework, CLI and ecosystem

**Status:** Accepted (2026-09-14) · **Amended by:** ADR-0036 (audit metadata controls)

## Context

apistock generates authentication, handles secrets, installs third-party recipes, runs on developer machines and publishes packages under a vanity import path. The threat model must cover the framework itself, not only apps built with it. This is an architecture-level model; specialist review and penetration testing are still required before 1.0.

## Assets

Developer machines (SSH keys, cloud and GitHub credentials), application source code, generated authentication code, end-user data in generated apps, the `apistock.dev` domain and DNS, the GitHub organisation and release pipeline, the module index.

## Trust boundaries

CLI ↔ Go module proxy; recipe → owned code; `apistock.dev` → `go-import` resolution; localhost dev services; CI workflows; app ↔ identity providers; tenant ↔ tenant; public API ↔ ops API.

## Threats and controls

| # | Threat | Control | Milestone |
|---|---|---|---|
| 1 | `apistock.dev` domain or DNS hijack redirects imports for new versions | Registrar lock, DNSSEC, hardware-key 2FA on registrar and DNS accounts, CAA records, scheduled CI check of `go-import` content | Before any public release |
| 2 | Template injection through user input | Names validated with `go/token.IsIdentifier`; field types from an allowlist; generated code parsed before writing | v0.1 |
| 3 | Recipe writes outside the project | All writes through `os.Root`; path validation | v0.1 |
| 4 | Recipe executes code at install time | Declarative operations only; no exec, shell or download operation | v0.1 |
| 5 | Malicious or compromised recipe injects a backdoor | Diff preview, clean git tree, risky new imports highlighted, trust levels for community modules | v0.1 / after 1.0 |
| 6 | Tampered module download | Go module proxy and checksum database; lock file records versions | v0.1 |
| 7 | Compromised CLI release | Releases built only in CI with OIDC; Sigstore signatures and provenance; protected tags; two-person approval for security-sensitive code | v0.1 |
| 8 | Maintainer account takeover | Required 2FA in the GitHub organisation; `CODEOWNERS`; branch protection | Now |
| 9 | GitHub token theft through the CLI | CLI never stores tokens; delegates to `gh`; generated workflows use least-privilege `permissions`, actions pinned by SHA, no `pull_request_target` | v0.1 |
| 10 | Secrets committed | `.env*` git-ignored; gitleaks in generated CI; `.env.example` placeholders only | v0.1 |
| 11 | Localhost services abused from a browser (CSRF, DNS rebinding) | Services bound to 127.0.0.1; custom dev console (v1.1) checks Host header and uses a session token | v0.2 / v1.1 |
| 12 | Account enumeration, credential stuffing | Uniform responses, rate limits, breached-password hook | v0.2 |
| 13 | Session theft | Hashed tokens, `__Host-` cookies, rotation, idle and absolute expiry, revocation | v0.2 |
| 14 | OAuth attacks (CSRF, code injection, token substitution) | PKCE, state, nonce, issuer and audience checks, verified-email-only linking | v0.3 |
| 15 | TOTP secret disclosure or code replay | Encrypted secrets with key rotation, single-use codes, rate limits | v0.3 |
| 16 | Passkey phishing or origin confusion | Relying-party ID and origin allowlist validation | v0.3 |
| 17 | Cross-tenant data access | Four isolation layers (ADR-0023) | v0.4 |
| 18 | Privilege escalation to ops | Platform roles separate from org roles; `/ops/*` requires 2FA; optional internal port | v0.5 |
| 19 | Sensitive data in logs, audit or ops responses | Log IDs not emails; `config.Secret` redaction; audit metadata allowlist | v0.2 |
| 20 | SSRF through outgoing requests (webhooks, avatar fetch) | Safe HTTP client blocking private, link-local and metadata addresses | When the feature ships |
| 21 | Vulnerable dependencies | `govulncheck` in repository CI, generated CI, and module checks | v0.1 |
| 22 | Open-core dependency changes (for example River Pro, Atlas Pro) | Dependencies behind modules; licences recorded; swap path documented | Ongoing |
| 23 | Runtime settings abused to weaken security (for example very long session or code expiry) or to leak secrets | No secret type in settings; bounds declared per setting plus hard limits inside library modules; `ops.settings.write` permission (2FA from v0.3); reason, history and audit event per change | v0.2 |
| 24 | Job controls abused: destructive jobs run on demand, schedules set to overload the database, security jobs (session purge, retention) disabled | `ops.jobs.write` and `ops.jobs.run` permissions (2FA from v0.3); reason required to disable or reschedule; minimum 1-minute interval and bounded timeout and attempts; audit event and history per change; job arguments never returned by ops APIs | v0.2 |

## Residual risk

Owner: project maintainer. Each accepted risk is recorded here with a review date. An external security review of authentication, sessions, tenancy and the generator is required before 1.0.

## Consequences

- `SECURITY.md` process in place before the repository is public.
- Every milestone's definition of done includes its rows from this table.

## v0.1 status (2026-09-14)

| # | Status |
|---|---|
| 1 Domain/DNS hijack | **Open, maintainer action** before any public release: registrar lock, DNSSEC, hardware-key 2FA, CAA records, `go-import` monitoring |
| 2 Template injection | Done: name and module path validation, templates rendered with fixed data, Go output parsed by `go/format` |
| 3 Writes outside project | Done: `os.Root` for app creation and template generation |
| 4 Code execution at install | Done: `aps new` runs only `go mod tidy` and `git init` |
| 5 Malicious recipes | Not yet applicable: no third-party recipes; diff preview arrives with `aps add` |
| 6 Tampered downloads | Done: Go module proxy and checksum database; lock file records hashes |
| 7 Compromised CLI release | Ready: release workflow with keyless Sigstore signing and build provenance; takes effect on the first `cli/v*` tag |
| 8 Maintainer takeover | **Open, maintainer action:** require 2FA in the `apistockhq` organisation, branch protection, `CODEOWNERS` |
| 9 GitHub token theft | Done: CLI stores no tokens; workflows use read-only permissions, SHA-pinned actions, no persisted credentials, no `pull_request_target` |
| 10 Secrets committed | Done: `.env*` ignored in generated apps; gitleaks in CI |
| 19 Sensitive data in logs | Done for v0.1 scope: `config.Secret` redaction, access logs without query strings, validation errors never echo values |
| 21 Vulnerable dependencies | Done: govulncheck in CI for every module |

## v0.2 status (2026-09-15)

| # | Status |
|---|---|
| 11 Localhost services | Done for PostgreSQL, Mailpit and Grafana: `compose.yaml` binds them to 127.0.0.1 in the repository and both presets |
| 12 Account enumeration | Done (ADR-0038): identical responses for register, resend and forgot-password; `invalid_credentials` for unknown addresses and wrong passwords after the same hashing work; 10 login attempts per address per 15 minutes; 5 attempts per code; 60 auth requests per IP per minute; breached-password hook (`PasswordChecker`) |
| 13 Session theft | Done (ADR-0038): 256-bit tokens stored as SHA-256; `__Host-` Secure HttpOnly SameSite=Lax cookies with cross-origin protection; idle (14 d) and absolute (90 d) expiry clamped to hard limits; revocation, logout-all, sessions ended on password change, reset and deletion |
| 18 Privilege escalation to ops | **Partial:** `/ops/*` requires a session whose platform roles grant each operation's permission (deny by default, roles read on every request, grants audited); `OPS_TOKEN` removed (ADR-0038). Required 2FA for ops roles and the optional internal port remain |
| 19 Sensitive data in logs | Done for new modules: database URLs never appear in errors, query spans record SQL text but not arguments, validation errors never echo values, job arguments never appear in ops responses, ops token never printed. Audit metadata (`modules/auditpg`, ADR-0036): values under sensitive keys (`password`, `secret`, `token`, `otp`, `recovery_code`, …) redacted at any depth, size bounded, text sanitised; audit events append-only through a trigger. A per-action metadata allowlist was considered and replaced by recorder-side redaction. Email (ADR-0037): the Resend API key and SMTP password are `config.Secret`s from the environment, never flags, runtime settings or error text; `aps add mail` saves typed secrets only to a git-ignored `.env` (mode 0600) and never prints them; SMTP credentials are sent only over TLS or to a local server; test email audit events omit the recipient; development email goes to Mailpit, never real people |
| 22 Open-core dependencies | River (MPL-2.0) and goose (MIT) used without Pro features; `robfig/cron/v3` (MIT) swap path is River's `PeriodicSchedule` |
| 23 Runtime settings abuse | Done: no secret type, declared bounds, reasons, versions, history and audit events (`modules/settings`) |
| 24 Job controls abuse | Done: permissions per operation, reasons to disable or reschedule, 1-minute minimum interval, bounded timeout and attempts, history and audit events, arguments hidden (`modules/jobs`) |

Reviewed against the code for v0.2's definition of done (rows 12, 13, 19, 23 and 24), 2026-09-15: each mitigation above is in place and tested. Notes from the review:

- Row 12: the breached-password hook is `authusecase.Config.PasswordChecker`. No checker is configured by default; an app chooses one (a local list, or a k-anonymity API) in `internal/app/module_auth.go`.
- Row 19: the auth use cases' log line for an email that couldn't be queued named its attribute `email`, which reads like an address; the value was always the email's kind (`verification_code`), never the address. It is now `email_kind`.
- Row 19: seed data (ADR-0042) prints the administrator's random password once to the developer's terminal, never to logs, files or the audit log, and refuses to run in production.
- Row 24: disabling requires a reason, a disabled job can't be run now (409) and leaves the schedule, and each change is in the job's history and audit log (`TestJobsThroughOps`).

## v0.3 status (in progress, 2026-09-15)

| # | Status |
|---|---|
| 15 TOTP secret disclosure or code replay | Done ([ADR-0043](0043-two-factor-authentication.md)): secrets encrypted with AES-256-GCM under `AUTH_ENCRYPTION_KEYS`, with key IDs, bound to the user ID and re-encrypted by `rotate-auth-keys`; a code is accepted only for a later time step than the last one used, checked and recorded in one statement (safe across instances); sign-in challenges allow 5 attempts in 5 minutes and count toward the per-address login limit; recovery codes are hashed and single-use; secrets, codes and recovery codes never appear in logs or audit metadata |
| 18 Privilege escalation to ops | Required 2FA done ([ADR-0043](0043-two-factor-authentication.md)): `platform_admin` and `ops_viewer` grant their permissions only to sessions verified with a second factor, in every environment; the rule is code, not a runtime setting; other roles' permissions are unaffected. The optional internal port remains (v0.5) |
| 16 Passkey phishing or origin confusion | Done ([ADR-0044](0044-passkeys.md)): the relying party ID and allowed origins come from the environment (not runtime settings), each origin must be https (http only for localhost in development) on the RP ID or a subdomain; every response's origin, RP ID hash, challenge, signature and user-verification flag are checked by `modules/auth/passkey`; ceremonies are single use and bound to their purpose, user and sign-in challenge; counters that don't increase are refused and audited; Android apps are allowed only by configured certificate fingerprints |
| 14 OAuth attacks | Open: Google and Apple sign-in come next with their own ADR |

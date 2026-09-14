# ADR-0029: Threat model: framework, CLI and ecosystem

**Status:** Accepted (2026-09-14)

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

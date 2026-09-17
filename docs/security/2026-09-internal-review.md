# Internal security review, September 2026

Pre-1.0 review of the gorbital library, the `orb` CLI and generator, and the golden apps (roadmap: stability and security review, item 1, [ADR-0053](../adr/0053-internal-security-review.md)). Reviewed and fixed on 2026-09-16 on branch `v1`.

**Result:** 45 finding IDs from six area reviews; two pairs describe the same weakness, so **43 distinct findings**: 2 High, 7 Medium, 28 Low, 6 Info. Every finding is fixed; four smaller parts of three findings are accepted with reasons below. No Critical finding, no cross-tenant data access, no MFA bypass, no SQL injection.

This review was done by the project, not by an independent party. `v1.0.0` still waits for the external review ([limitations](#limitations), [open maintainer actions](#open-maintainer-actions)).

## Scope and method

| Area | Covered | IDs |
|---|---|---|
| Email and password sign-in, sessions, codes, account lifecycle | `modules/auth` (passwords, tokens, middleware, catalogs, emails), `actor`, the Full apps' auth module (use cases, delivery, SQL), admin commands, seed, rate limits | AUTH-S-1 … 8 |
| Strong authentication | TOTP, recovery codes, keyring, passkeys (`modules/auth/passkey`), Google and Apple sign-in (`modules/auth/social`) and their app wiring | AUTH-M-1 … 4 |
| Organisations and resources | `modules/orgs`, `examples/full-multi` orgs and projects modules, user-scoped projects in `full-single`, resource templates, `page` | ORG-1 … 7 |
| Operations | `/ops` module, runtime settings, jobs, audit storage, mail senders, releases, shared rate limits, maintenance mode, API exports | OPS-1 … 10 |
| HTTP core and app wiring | `httpx`, `requestid`, `health`, `config`, `audit`, `mail`, `app`, `buildinfo`, `modules/telemetry`, `modules/postgres`, `modules/openapi` and its reference, app configuration, Dockerfiles, Compose files | HTTP-1 … 8 |
| CLI and supply chain | `orb` commands, template generator, `orb upgrade` and `orb add` merge bases, lock files, release workflows, `CODEOWNERS`, toolchain and dependencies (govulncheck on every module) | CLI-1 … 8 |

How it was done:

1. **Read-only review per area.** Each reviewer read the code against the threat model ([ADR-0029](../adr/0029-threat-model.md)) and the area's ADRs, without changing the repository, and wrote proof-of-concept tests run against Docker PostgreSQL (through `go test -overlay` or a scratch copy) for every finding marked confirmed. Each report also lists what was checked and found sound.
2. **Triage.** The lead set a severity for each finding and decided fix or accept. Severity: **High**, an account or a secret can be taken without special access; **Medium**, a security control can be bypassed or abused at scale; **Low**, a weakness that needs an unusual position or has limited impact; **Info**, hardening or a documentation gap. "Latent" marks code no shipped configuration reaches yet, such as an app-added role.
3. **Fixes in five groups** (authentication; organisations; ops, jobs, audit and mail; HTTP core and telemetry; CLI and supply chain), each on its own branch, merged into `v1`. The rule: every finding is fixed with a regression test that fails without its fix, or accepted in writing. Each group reverted its fix (or ran the reviewers' PoC) to see the test fail.
4. **Documentation.** Each group recorded its design in the ADRs it amended ("Security review fixes (2026-09-16)" sections, listed in [ADR-0053](../adr/0053-internal-security-review.md)); the threat model, [upgrade notes](../guides/upgrade-notes.md) and `CHANGELOG.md` were updated from their notes.

A first fix came before the review: commit `67261e3` stopped the template generator from copying a golden app's local `.env` file (the first half of CLI-1).

Commits on `v1`: `67261e3` (pre-review generator fix), `f13b22a` (CLI and supply chain), `887ad97` (organisations), `258106b` (HTTP core), `69c7790` (ops), `454b735` and `060b33a` (authentication), `25a1fe5` (authentication follow-ups).

## Summary

Distinct findings by area and severity (AUTH-M-2 is counted with AUTH-S-5, OPS-1 with AUTH-S-7):

| Area | High | Medium | Low | Info | Total | Fixed | Part accepted |
|---|---|---|---|---|---|---|---|
| Sign-in, sessions, codes | 1 | 2 | 4 | 1 | 8 | 8 | 0 |
| Strong authentication | 0 | 1 | 2 | 0 | 3 | 3 | 0 |
| Organisations | 0 | 3 | 3 | 1 | 7 | 7 | 0 |
| Operations | 0 | 0 | 7 | 2 | 9 | 9 | 1 (OPS-10) |
| HTTP core | 0 | 1 | 6 | 1 | 8 | 8 | 1 (HTTP-7) |
| CLI and supply chain | 1 | 0 | 6 | 1 | 8 | 8 | 1 (CLI-7) |
| **Total** | **2** | **7** | **28** | **6** | **43** | **43** | **3** |

"Part accepted": the finding's main issue is fixed and a smaller part is accepted ([accepted risks](#accepted-risks)).

## Findings

### Email and password sign-in, sessions, codes

| ID | Severity | Title | Status | Fix | Regression test |
|---|---|---|---|---|---|
| AUTH-S-1 | High | Pre-registration takeover: whoever registered an address before its owner chose the password the owner later verified, and could keep access with credentials added meanwhile | Fixed | Registering again for an unverified account keeps its password only if the same password is given; verifying an address (code, reset, Google or Apple link) ends sessions and removes passkeys, authenticator app, recovery codes and identities added before. Follow-ups: accounts created by Google or Apple for an address the provider isn't authoritative for start unverified; roles are granted only to verified accounts; unverified accounts expire after `auth.unverified_account_ttl` (7 days) | `TestPreRegistrationTakeover`, `TestRegisterAgainKeepsOnlyTheSamePassword`, `TestVerifyingRemovesWhatCameBefore`, `TestNonAuthoritativeSocialAccountIsClaimedByEmail`, `TestRolesGrantPermissions`, `TestCleanupExpiresUnverifiedAccounts` |
| AUTH-S-2 | Medium | Requesting new codes brought new guesses: no limit on code checks across codes | Fixed | Per-address budget across all verification and reset codes, shared by instances: `auth.code_attempts` (20) per `auth.code_window` (24 h) | `TestCodeChecksAreLimitedAcrossCodes` |
| AUTH-S-3 | Low | Unicode case folding in `NormalizeEmail` let a look-alike address (such as one with the Kelvin sign) take another address's account key | Fixed | `auth.NormalizeEmail` refuses addresses whose non-ASCII characters change when lowercased | `TestTokensCodesAndEmails`, `TestRegisterValidatesInput` |
| AUTH-S-4 | Low | Response time showed whether an address has an account (register, resend, forgot password) | Fixed | Those endpoints take at least `MinResponseTime` (300 ms) on every outcome | `TestAnonymousFlowsTakeTheSameTime` |
| AUTH-S-5 | Low | A stolen session could guess the password through account changes, outside the sign-in limit | Fixed | Per-user limit `auth.reauth_attempts` (10 per `auth.login_window`) on every change that checks the password or a second factor; failures audited as `auth.reauth.failed` | `TestChecksBehindASessionAreLimited` |
| AUTH-S-6 | Low | Ten wrong passwords from anywhere locked the owner out of sign-in | Fixed | Sign-in limit keyed by address and client network (IPv4 address or IPv6 /64), plus a looser per-address limit `auth.login_address_attempts` (50) | `TestLoginLimitIsPerNetwork` |
| AUTH-S-7 | Medium | Per-IP limits counted each IPv6 address separately, so a /64 was effectively unlimited (also reported as OPS-1) | Fixed | Core `ratelimit.ClientKey`; `ratelimit.ByRemoteIP` groups IPv6 by /64 | `TestClientKey`, `TestByRemoteIPGroupsIPv6By64`, `TestPerIPLimitGroupsIPv6` |
| AUTH-S-8 | Info | Unauthenticated requests could queue unbounded argon2 work | Fixed | `auth.Hasher` waits at most 5 s for a slot (`ErrHasherBusy`, 503 `auth_unavailable`), honours context cancellation; password reset checks the code before hashing | `TestHasherWaitIsBounded`, `TestResetChecksTheCodeBeforeHashing` |

### Strong authentication

| ID | Severity | Title | Status | Fix | Regression test |
|---|---|---|---|---|---|
| AUTH-M-1 | Medium | Google and Apple sign-in linked an existing account for any provider-verified email, including addresses the provider doesn't manage (a stale provider account could take over an account without 2FA) | Fixed | Automatic linking only when the provider is authoritative for the address (Gmail, the account's Google Workspace domain via `hd`, iCloud, Apple relay); otherwise 403 `social_link_required`, and the signed-in owner links with the new `POST /v1/auth/identities` | `TestAuthoritativeEmail`, `TestSocialLinksOnlyAuthoritativeEmails`, `TestSocialLinkingEndToEnd`, `TestSocialWebSignIn` |
| AUTH-M-2 | Low | No per-account limit on password and second-factor checks behind a session | Fixed with AUTH-S-5 | See AUTH-S-5 | `TestChecksBehindASessionAreLimited` |
| AUTH-M-3 | Low | Apple server-to-server notifications had no freshness or replay check | Fixed | Notifications older than 1 hour (or from the future) refused; each processed once, remembered by ID until it expires | `TestAppleNotificationFreshness`, `TestAppleNotificationReplay`, `TestAppleNotificationReplayEndToEnd` |
| AUTH-M-4 | Low | Recovery codes carried 50 bits under unkeyed SHA-256, so a database dump allowed offline guessing | Fixed (different design) | New codes carry 80 bits (`xxxx-xxxx-xxxx-xxxx`); existing codes keep working | `TestRecoveryCodes` |

### Organisations

No cross-tenant read or write was found: every org-scoped query filters on `org_id` from the authorised path. The findings concern privilege boundaries inside one organisation and abuse limits.

| ID | Severity | Title | Status | Fix | Regression test |
|---|---|---|---|---|---|
| ORG-1 | Medium | A pending invitation kept its role after the inviter was removed or demoted, so a removed admin could get back in | Fixed | Accepting checks, under the organisation lock, that the inviter (or last resender) is still a member allowed to give that role | `TestInvitationsEndWithTheInvitersRole` |
| ORG-2 | Medium (latent) | `canAssign` ranked app-added roles as `member`, so an admin could give a custom role more powerful than admin | Fixed | A role may be given, changed or removed only if its permissions are a subset of the actor's; only owners manage owners | `TestCustomRolesCantExceedTheAssigner` |
| ORG-3 | Medium | Unlimited organisation creation made the per-organisation invitation limit ineffective against email spam and phishing from the app's domain, and could grow a purge backlog | Fixed | Creating organisations and inviting need a verified address; `orgs.max_owned` (20, 409 `too_many_orgs`) and `orgs.user_invitations_per_hour` (50, shared limiter); the purge job clears its backlog each run | `TestOrganisationAndInvitationLimits`, `TestOwnedOrganisationLimitHoldsUnderConcurrency`, `TestPurgeWorksThroughABacklog` |
| ORG-4 | Low | The caller's role was read before the transaction and not re-read under the organisation lock | Fixed | Member-management use cases re-read the caller's membership inside the locked transaction | `TestRoleIsCheckedAgainUnderTheLock` |
| ORG-5 | Low (latent) | Restoring an organisation skipped the org role's two-factor step-up | Fixed | New `orgs.Authorize`, used by `Restore`; 403 `mfa_required` like delete | `TestRestoreNeedsTheSameStepUpAsDelete`, `TestAuthorize` |
| ORG-6 | Low | A deleted account stayed a member, possibly the only owner, of organisations that were deleted at the time | Fixed | Account deletion leaves every organisation; owner counts ignore deleted accounts | `TestDeletedAccountsDontStayOwners` |
| ORG-7 | Info | Accepting and resending the same invitation locked rows in opposite orders and could deadlock | Fixed | Every invitation change locks organisation, then invitation | `TestInvitationChangesLockTheOrganisationFirst` |

### Operations

Every `/ops` operation already checked its permission and two-factor authentication before touching a dependency, and every query was parameterised.

| ID | Severity | Title | Status | Fix | Regression test |
|---|---|---|---|---|---|
| OPS-1 | Medium | Per-IP limit counted each IPv6 address separately | Fixed with AUTH-S-7 | See AUTH-S-7 | See AUTH-S-7 |
| OPS-2 | Low | The reason required to disable a job could be sidestepped with a 1 s timeout, a queue pause or unlimited run-now | Fixed | Reasons required for timeout, attempts and queue changes and for pausing a queue (`PauseQueueWithReason`); run-now refused (429 `job_run_limited`) while a run is queued or running or within a minute of the last | `TestUpdateRequiresReasonForLimits`, `TestRunNowIsLimited`, `TestPauseAndResumeQueue`, `TestJobsThroughOps` |
| OPS-3 | Low | Retry re-ran completed runs (including delivered emails) and runs of disabled jobs | Fixed | Retry only for runs waiting to retry, discarded or cancelled, of enabled jobs (409 `job_not_retryable`, `job_definition_disabled`) | `TestRetryRefusesCompletedJobsAndDisabledDefinitions`, `TestJobsThroughOps` |
| OPS-4 | Low | Audit metadata redaction matched only exact singular key names (`tokens`, `apiKeys`, `recovery_codes` got through) | Fixed | Keys normalised (case style, plural) and matched by segment; more sensitive names added | `TestRecordRedactsAndSanitizes` |
| OPS-5 | Low | Recipient addresses in provider errors reached job errors, `/ops/jobs` and logs | Fixed | Core `mail.RedactAddresses`; SMTP and Resend senders, the mail worker, its error handler and River's logger replace addresses with `[email]` | `TestRedactAddresses`, `TestSendClassifiesRefusals`, `TestSendClassifiesErrors`, `TestMailWorkerCancelsRejectedEmail`, `TestFailedAttemptsAreLoggedOnce` |
| OPS-6 | Low | `GET /ops/audit` queries had no time limit | Fixed | Audit listing and stats stop after 5 s (503 `audit_query_timeout`) | `TestListAndStatsTimeOut` |
| OPS-7 | Low | Audit events for ops changes never recorded the client IP or user agent | Fixed | Core `actor.WithClient`, set by `auth.Middleware` after trusted-proxy handling; `audit.FromContext` fills every event recorded during a request | `TestClientRoundTrip`, `TestFromContextFillsClient`, `TestMiddlewareSetsTheActorClient`, `TestRuntimeSettingsThroughOps` |
| OPS-8 | Low | `POST /ops/mail/test` queued the email before a permission check that could then answer 403; no limit | Fixed | Authorised before queueing; 5 test emails an hour per operator | `TestSendTestEmailNeedsOnlyItsPermissionAndIsLimited`, `TestOpsRoutesRequirePermissions` |
| OPS-9 | Info | Some security-relevant settings changed without a reason | Fixed | Reasons required for `mail.from_name`, `mail.from_email`, `mail.reply_to`, `auth.verification_code_ttl`, `orgs.invitation_url`, `orgs.invitation_ttl` | `TestEmailThroughOps` |
| OPS-10 | Info | Smaller notes: no default item cap on `StringList` settings; host names in `/ops/releases`; audit writes that fail leave some actions without a durable record; threat model wording | Fixed in part, rest accepted | `settings.DefaultMaxItems` (100); threat model row 19 wording corrected; host names and audit durability accepted | `TestInvalidDeclarationsPanic` |

### HTTP core

The core middleware (CORS, cross-origin protection, request ID validation, body limit, recovery, problem mapping) was found sound.

| ID | Severity | Title | Status | Fix | Regression test |
|---|---|---|---|---|---|
| HTTP-1 | Medium | A client's `traceparent` controlled sampling, trace IDs and job and audit correlation; spans took `client.address` from spoofable `X-Forwarded-For` | Fixed | Each request starts a new trace linked to the incoming one; baggage ignored; trace context continued only from `APP_TRUSTED_CALLERS` (`telemetry.WithTraceContextFrom`); `client.address` is the resolved client | `TestHTTPUntrustedTraceContext`, `TestHTTPTrustedTraceContextKeepsSampling` |
| HTTP-2 | Low–Medium | The client's `Host` header became a metric attribute, so varied hosts could exhaust the metric cardinality limit and hide real series | Fixed | HTTP server metrics drop `server.address` and `server.port` | `TestHTTPMetricsIgnoreHost` |
| HTTP-3 | Low | `APP_DOCS_ENABLED=false` still served the OpenAPI document; docs were on in production by default | Fixed | Docs and document switched together (`openapi.WithoutSpecEndpoints`); off by default in production | `TestWithoutSpecEndpoints`, `TestDocs`, `TestLoadConfigSecureDefaults` |
| HTTP-4 | Low | `http://` CORS origins accepted in production, although they're trusted for cross-origin protection and sign-in `return_to` | Fixed | Non-https origins refused in production; `httpx.CORS` refuses origins with user info, path, query or fragment | `TestLoadConfigSecureDefaults`, `TestCORS` |
| HTTP-5 | Low | Client-chosen `X-Request-ID` accepted from any peer and stored in audit events and job metadata | Fixed | `httpx.RequestID` always generates; `httpx.RequestIDFrom` keeps incoming IDs only from `APP_TRUSTED_CALLERS` | `TestRequestID`, `TestRequestIDFromTrustedCallers` |
| HTTP-6 | Low | An unset `APP_ENV` silently meant development, skipping production checks | Fixed | Apps refuse to start without `APP_ENV`; `orb dev` sets `development` | `TestLoadConfigSecureDefaults`, `TestDevSetsAppEnv` |
| HTTP-7 | Low | Unthrottled `/readyz` pinged PostgreSQL on every call; `/version` shows the Go version and commit | Fixed in part, rest accepted | Readiness checks shared by concurrent requests and cached for 1 s; `/version` detail accepted | `TestReadinessSharesChecks` |
| HTTP-8 | Info | The production guide still described hand-written forwarded-header middleware and per-instance limits | Fixed | Guide points to `APP_TRUSTED_PROXIES`, shared limits and `APP_TRUSTED_CALLERS` | Documentation |

### CLI and supply chain

| ID | Severity | Title | Status | Fix | Regression test |
|---|---|---|---|---|---|
| CLI-1 | High | `go generate` copied git-ignored local files, including a golden app's `.env`, into templates, so a locally built `orb` could write a developer's secrets into new apps | Fixed | `67261e3` skipped `.env*` files; the generator now uses git's own file set (`git ls-files --cached --others --exclude-standard`) | `TestSkippedLocalEnvironmentFiles`, `TestRunLeavesGitIgnoredFilesOut` |
| CLI-2 | Low | `orb add mail` saved secrets into an existing world-readable `.env` | Fixed | `.env` made 0600 with a warning | `TestAddMailMakesAnExistingEnvPrivate` |
| CLI-3 | Low | A trailing slash in a `GONOSUMDB` or `GOPRIVATE` pattern slipped past the checksum-policy check for upgrade merge bases | Fixed | Patterns matched as Go matches them; `GOSUMDB` other than sum.golang.org refused | `TestMatchesModulePrefix`, `TestChecksumPolicy` |
| CLI-4 | Low | App name and module path read back from `gorbital.lock` and `gorbital.yaml` were rendered into templates unvalidated | Fixed | The same validators as `orb new` run on every read | `TestReadLockRejects`, `TestReadManifestRejectsInvalidInputs` |
| CLI-5 | Low | An upgrade's merge base could come from a "checkout" inside the app named by its own `replace` directive | Fixed | Earlier releases read only from a gorbital repository outside the app, at a gorbital commit | `TestReleaseCheckoutRefusesCheckoutInsideApp`, `TestReleaseFromCheckoutNeedsAGorbitalCommit` |
| CLI-6 | Low | Release pipeline gaps: build cache in the release job, no approval gate, release-critical files missing from `CODEOWNERS` | Fixed (repository settings pending) | `release` environment, tag commit must be on `main`, no cache, tests before release; more `CODEOWNERS` paths; signature verification documented in the CLI guide | Workflow configuration, not run on GitHub Actions |
| CLI-7 | Low | `go 1.26.0` allows toolchains without the `os.Root` fixes; the Minimal template pinned a vulnerable grpc | Fixed in part, rest accepted | grpc v1.83.2 in Minimal and `modules/telemetry`; `orb version` and `orb doctor` warn when built with Go older than 1.26.5; the `go` directive stays | `TestToolchainWarning`, `TestDoctorOnANewApp` |
| CLI-8 | Info | The threat model claimed CI controls in generated apps that don't exist | Fixed | Threat model rows 7, 9, 10 and 21 corrected | Documentation |

## Accepted risks

| Finding | Accepted | Reason | Review |
|---|---|---|---|
| OPS-10 | `GET /ops/releases/instances` and `/current` return instance host names | Operator-only (`ops.releases.read` with 2FA); host names identify instances during rollouts. Threat 31's promise is about `/ops/system` infrastructure details | Before 2.0 or when `/ops` gains a less trusted role |
| OPS-10 | Run-now, retry, cancel, pause and resume have no history table, so a failed audit write leaves only the logged error | Making audit writes transactional with River operations would let an audit outage block incident response. Settings and job-definition changes keep their history tables | With the external review |
| HTTP-7 | `/version` shows version, Go version, commit and whether the build was modified | Build information is public by design for release tracking; the production guide suggests blocking `/livez`, `/readyz` and `/version` at the load balancer | With the external review |
| CLI-7 | `go 1.26.0` directive kept | ADR-0015 sets the minimum Go version; raising the directive forces every app's toolchain. `orb` warns below 1.26.5, CI and the release job use the latest patch, docs require it | Each Go minor release |

Residual risks noted by the fix groups, not separate findings:

- **AUTH-S-4:** if the database is slower than the 300 ms floor under load, timing differences can reappear.
- **ORG-3:** no per-recipient limit on invitation emails; the per-user limit (50 an hour, verified accounts only) bounds each attacker account.
- **OPS-4:** a bare `code` key isn't redacted, so error codes stay readable; apps add it with `WithRedactedKeys("code")`.
- **OPS-5:** errors that application workers store themselves aren't rewritten; that stays the app's responsibility.
- **OPS-6:** some audit filters (`outcome`, `actor_kind`, prefix only) have no index and rely on the timeout.
- **AUTH-S-1:** accounts verified before the fix with a password set by an earlier registrant can't be told apart; the upgrade notes give a query to review them.

## Deviations from the reviewers' recommendations

| Finding | Recommended | Done instead | Why |
|---|---|---|---|
| AUTH-S-1 | Store the password chosen at registration with the code attempt; verification sets the password from that attempt; registering again starts a new attempt | Registering again keeps the stored password only when the same password is given; a different one removes it. Verification removes everything added before | With "newest attempt wins", an attacker re-registering every minute replaces the owner's pending code with one carrying the attacker's password, and the owner enters the newest code. No contested password survives; the owner sets one with password reset, which proves the mailbox |
| AUTH-S-4 | Send the email through the job queue, or do equal work on both branches | A minimum response time (300 ms) on register, resend and forgot password | A job would put email addresses into job arguments visible through `/ops/jobs`, and registration's account hooks (personal workspace) still run in the request |
| AUTH-M-1 | An explicit link flow for non-authoritative addresses | `POST /v1/auth/identities` with an ID token and a nonce, not a redirect flow | A redirect flow can't bind Apple's cross-site form-post callback to the signed-in user without a third-party cookie or a second single-use token |
| AUTH-M-4 | HMAC recovery codes with a key derived from `AUTH_ENCRYPTION_KEYS` | 80-bit codes under the same SHA-256 storage | Recovery codes can't be re-hashed without the plaintext, so key rotation couldn't migrate them and removing an old key would silently break every user's lockout escape; passkey-only accounts have recovery codes on servers without `AUTH_ENCRYPTION_KEYS` |
| HTTP-1, HTTP-5 | Trust incoming trace context and request IDs from trusted proxies | A separate list, `APP_TRUSTED_CALLERS`, matched on the client address after `APP_TRUSTED_PROXIES` | Load balancers usually pass clients' `traceparent` and `X-Request-ID` through unchanged; trusting them from proxies would reopen both findings in the recommended deployment |
| HTTP-4 | Triage left open an exception for `http://localhost` in production | No exception: every non-https origin refused in production | Matches the `WEBAUTHN_ORIGINS` rule; production frontends on localhost aren't a real case |
| OPS-3 | Retry only discarded and cancelled runs | Runs waiting to retry (`retryable`) may be retried too | Retrying one only brings its next attempt forward; it never re-runs finished work |
| ORG-1 | Check the inviter at acceptance and also revoke a member's open invitations when they're removed or demoted; let admins revoke owner invitations | Acceptance re-checks the inviter (or last resender) under the lock; nothing is revoked on removal; revocation stays "roles you may assign" | The acceptance check covers removal, demotion and account deletion in one place, including paths added later. An owner invitation from a removed owner stops working on its own, so admins don't need power over owner invitations |
| CLI-4 | Also require the lock's module to equal `go.mod`'s | Validation only | Validation alone blocks the injection |

## Open maintainer actions

These can't be done from the repository and block `v1.0.0`:

| Action | Where | Threat model |
|---|---|---|
| External security review of authentication, sessions, tenancy and the generator, and its sign-off | Third party | Residual risk section |
| Create the GitHub environment `release` with maintainers as required reviewers and deployment tags limited to `cli/v*` | Repository settings | Row 7 |
| Tag ruleset restricting creation, update and deletion of `v*`, `cli/v*` and `modules/**/v*` to maintainers | Repository settings | Row 7 |
| Create the `maintainers` and `security` teams named in `CODEOWNERS`, and branch protection requiring code owner review | GitHub organisation | Rows 7, 8 |
| Domain hardening: registrar lock, DNSSEC, hardware-key 2FA, CAA records, `go-import` monitoring; required 2FA in the GitHub organisation | Registrar, DNS, GitHub | Rows 1, 8 |
| Contact address in `CODE_OF_CONDUCT.md` | Repository | Row 8 ([ADR-0055](../adr/0055-governance-and-contribution.md)) |

## Limitations

- **Internal reviewers.** The people who reviewed the code are close to the project; the review is no substitute for the external one `v1.0.0` requires.
- **No load or timing measurement over a network.** Rate limits, the hashing queue, readiness caching and response-time padding are tested in unit and integration tests against local PostgreSQL, not under production-like load.
- **golangci-lint wasn't run locally** (not installed); `gofmt`, `go vet`, the race detector and govulncheck were. GitHub CI was paused, so workflow changes (CLI-6) weren't run on Actions.
- **Scope:** client templates, the gorbital-web site and the dashboards weren't reviewed; nor were third-party services (Resend, Google, Apple) beyond how the apps call them.
- **Found and left open:** in the Full apps, Google and Apple sign-in without a `return_to` send the browser to `APP_PUBLIC_URL/docs`, which now answers 404 in production unless docs are enabled; frontends pass `return_to`.

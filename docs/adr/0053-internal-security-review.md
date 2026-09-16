# ADR-0053: Internal security review before the external one

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0007, ADR-0017, ADR-0020, ADR-0027, ADR-0029, ADR-0030, ADR-0033, ADR-0036, ADR-0037, ADR-0038, ADR-0041, ADR-0043, ADR-0046, ADR-0048, ADR-0050, ADR-0051, ADR-0052 (each gained a "Security review fixes (2026-09-16)" section; ADR-0029's rows and status were updated)

## Context

v1.0 requires a security review before the API is declared stable (roadmap v1.0 item 1). The threat model says "an external security review of authentication, sessions, tenancy and the generator is required before 1.0" ([ADR-0029](0029-threat-model.md)). Where things stood:

| Area | Today | Evidence |
|---|---|---|
| External review | None booked; it needs a third party and a budget, and can take months to schedule | Roadmap v1.0 "Not included" |
| Security-sensitive code | Email and password sign-in, sessions, codes, TOTP, recovery codes, passkeys, Google and Apple sign-in, organisations, `/ops`, runtime settings, jobs, audit, rate limits, the generator, `orb upgrade` merge bases, release workflows | `modules/auth`, `modules/orgs`, `examples/full-*`, `cli/`, `.github/workflows` |
| Earlier reviews | Per milestone, against each milestone's threat model rows; no review across areas | ADR-0029 status sections |
| Known leak | The template generator copied a golden app's git-ignored `.env` into templates, so a locally built `orb` could write a developer's secrets into new apps; found while planning v1.0 | Commit `67261e3` |
| Stable surfaces | Error codes, audit actions, setting keys and the Go API freeze at 1.0; security fixes that change them are cheaper before | [ADR-0054](0054-api-freeze-and-scaffold-compatibility.md) |
| Threat model accuracy | Some rows claimed controls generated apps don't have (CI in generated apps) | ADR-0029 rows 9, 10, 21 |

The maintainer decided (2026-09-16): review internally first, fix everything found, and keep v1.0 "awaiting external sign-off" until the external review is done.

## Options

| Option | Verdict |
|---|---|
| Wait for the external review and fix only what it finds | Rejected: v1.0's other work would freeze public names before the review; an external reviewer's time goes on findings the project could have found itself |
| **Internal review first, area by area, every finding fixed or accepted in writing; the external review afterwards** | **Chosen** |
| Skip a dedicated review: rely on the per-milestone threat model checks | Rejected: those checks confirm listed controls exist; they don't look for what the list misses, such as the `.env` leak or pre-registration takeover |

## Decision

### Method

- **Six areas**, each reviewed separately and read-only: email and password sign-in with sessions and codes; strong authentication (TOTP, recovery codes, keyring, passkeys, Google and Apple); organisations and resources; operations (settings, jobs, audit, mail, releases, rate limits); HTTP core, telemetry and app configuration; CLI, generator, upgrades and supply chain.
- Each reviewer reads the code against ADR-0029 and the area's ADRs, writes proof-of-concept tests against Docker PostgreSQL for findings it confirms (without changing the repository), and lists what it checked and found sound. govulncheck runs on every module.
- A lead triages: severity, fix or accept, and the design of each fix where the recommended one is unclear.
- Fixes land in groups by area, each on its own branch merged into `v1`. Shared documents (threat model, upgrade notes, changelog, roadmap, the report) are written once from each group's notes, so groups don't conflict.

### Severity scale

| Severity | Meaning |
|---|---|
| Critical | Remote compromise of every app or of users' machines without interaction |
| High | An account or a secret can be taken without special access |
| Medium | A security control can be bypassed or abused at scale |
| Low | A weakness that needs an unusual position (a stolen session, operator access, a tampered file) or has limited impact |
| Info | Hardening or a documentation gap |

"Latent" marks a weakness no shipped configuration reaches yet (for example, one that needs an app-added role); it keeps its severity and is fixed like any other.

### Fix rule

- Every finding is **fixed with a regression test that fails without the fix**, or **accepted in writing** with the reason and a review point.
- The fixer may choose a different design from the reviewer's; the report says why.
- Every fix states how existing apps receive it (upgrade notes) and records its design in the ADR it amends.
- The report is published under `docs/security/`: scope and method, every finding with status, fix and test, accepted risks, deviations, open maintainer actions and limitations. Exploit details are described only as far as needed to understand impact and fix.

## Why

- Fixing before the freeze keeps security changes to error codes, settings and APIs additive and cheap.
- Splitting by area gives each reviewer a boundary small enough to read fully, and lets fix groups work in parallel without touching the same files.
- A test that fails without its fix is the only evidence a later change can't silently undo.
- An internal review makes the external one cheaper and sharper: the reviewer starts from a documented threat model, a published report and fixed known issues.

## Trade-offs

- Internal reviewers share the project's assumptions; blind spots in the design can survive. The external review stays required.
- Fixing everything, including Info findings, delays v1.0 and adds settings and behaviour changes existing apps must take (upgrade notes).
- Publishing the report tells attackers what was wrong; everything in it is fixed or accepted, and apps still on older code are told what to upgrade.

## Consequences

- v1.0 stays **awaiting external sign-off**. The external review is a maintainer action, as are the repository settings the fixes rely on (release environment, tag rulesets, `CODEOWNERS` teams).
- A review like this one is repeated before each major release, with its report added to `docs/security/`.
- ADR-0029 is corrected where it claimed controls that don't exist, gains rows for threats the review found, and records the review's status per row.

## Implementation notes (2026-09-16)

Report: [docs/security/2026-09-internal-review.md](../security/2026-09-internal-review.md). Commits: `67261e3` (generator `.env` fix before the review), `f13b22a` (CLI and supply chain), `887ad97` and merge `80ca809` (organisations), `258106b` and merge `4f96cb4` (HTTP core), `69c7790` and merge `d6b8891` (ops), `454b735`, `060b33a`, `7d6d1aa` and merge `2012609` (authentication), `25a1fe5` (authentication follow-ups).

Findings: 45 IDs; AUTH-M-2 duplicates AUTH-S-5 and OPS-1 duplicates AUTH-S-7, so 43 distinct.

| Area | High | Medium | Low | Info | Fixed | Part accepted |
|---|---|---|---|---|---|---|
| Sign-in, sessions, codes (AUTH-S) | 1 | 2 | 4 | 1 | 8 | 0 |
| Strong authentication (AUTH-M) | 0 | 1 | 2 | 0 | 3 | 0 |
| Organisations (ORG) | 0 | 3 | 3 | 1 | 7 | 0 |
| Operations (OPS) | 0 | 0 | 7 | 2 | 9 | 1 |
| HTTP core (HTTP) | 0 | 1 | 6 | 1 | 8 | 1 |
| CLI and supply chain (CLI) | 1 | 0 | 6 | 1 | 8 | 1 |
| **Total** | **2** | **7** | **28** | **6** | **43** | **3** |

| Check | Result |
|---|---|
| Every finding fixed or accepted | 43 fixed; parts of OPS-10 (instance host names in `/ops/releases`, audit-write durability), HTTP-7 (`/version` detail) and CLI-7 (`go 1.26.0` directive) accepted with reasons in the report |
| Regression tests | Every code fix has a named test; each group checked it fails with the fix reverted or with the reviewers' PoC. CLI-6 (workflow) and HTTP-8, CLI-8 (documentation) have none |
| Designs different from the recommendation | AUTH-S-1 (same-password rule instead of per-attempt passwords), AUTH-S-4 (minimum response time instead of a job), AUTH-M-1 (ID token link endpoint), AUTH-M-4 (80-bit codes instead of HMAC), HTTP-1 and HTTP-5 (`APP_TRUSTED_CALLERS` separate from trusted proxies), OPS-3 (runs waiting to retry stay retryable), ORG-1 (acceptance check instead of revocation) |
| Public names added | Error codes `social_link_required`, `identity_in_use`, `too_many_orgs`, `job_run_limited`, `job_not_retryable`, `audit_query_timeout`; audit actions `auth.reauth.failed`, `auth.accounts.unverified_expired`; settings `auth.login_address_attempts`, `auth.reauth_attempts`, `auth.code_attempts`, `auth.code_window`, `auth.unverified_account_ttl`, `orgs.max_owned`, `orgs.user_invitations_per_hour`; env var `APP_TRUSTED_CALLERS`. Nothing removed or renamed; `api/surface.json` and API listings re-recorded |
| Behaviour changes to stable API | `ratelimit.ByRemoteIP` groups IPv6 by /64; `auth.NormalizeEmail` refuses more input; `auth.NewRecoveryCodes` format; `httpx.RequestID` ignores incoming IDs; `jobs.Manager.PauseQueue` returns `ErrReasonRequired` (deprecated for `PauseQueueWithReason`) |
| Tests | Each group ran `gofmt`, `go vet` and `go test` (race detector for library modules) in the modules and apps it touched, the root module with `internal/archtest`, and `cli`; `ORB_E2E=1` for generator changes. the end-to-end upgrade test couldn't run from pre-rename tags and became `TestUpgradeFromRelease` ([ADR-0050](0050-upgrades-and-adding-features.md)) |
| Not run | golangci-lint (not installed locally); GitHub Actions (paused), so the release workflow change is unexercised; no load testing |
| Open maintainer actions | External review sign-off; GitHub `release` environment with required reviewers; tag rulesets for `v*`, `cli/v*`, `modules/**/v*`; `maintainers` and `security` teams for `CODEOWNERS`; domain hardening (threat model rows 1 and 8); code of conduct contact address |

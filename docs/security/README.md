# Security

How gorbital handles security, and the results of its reviews.

| Document | What it covers |
|---|---|
| [Threat model](../adr/0029-threat-model.md) (ADR-0029) | Assets, trust boundaries, every threat with its control and status per milestone |
| [Internal security review, September 2026](2026-09-internal-review.md) | Pre-1.0 review of the library, `orb` and the golden apps: 43 findings, every one fixed or accepted with a regression test, deviations from recommended fixes, open maintainer actions |
| [ADR-0053](../adr/0053-internal-security-review.md) | Why the review was internal first, its method, severity scale and fix rule |
| [Upgrade notes](../guides/upgrade-notes.md) | What existing apps must do for each release, including the review's fixes |
| [Secrets and keys](../guides/secrets-and-keys.md) | Encryption keys, rotation and where secrets live |
| [Running in production](../guides/production.md) | TLS, trusted proxies and callers, rate limits, health endpoints |
| [Go-live checklist](../sign-in/go-live.md) | Every production value to check before real people use an app |

## Reporting a vulnerability

Report privately through the repository's **Security** tab (**Report a vulnerability**), never in public issues. Supported versions and response times: [SECURITY.md](../../SECURITY.md).

## Reviews

| Date | Kind | Scope | Report | Status |
|---|---|---|---|---|
| 2026-09-16 | Internal | Library, CLI and generator, release pipeline, golden apps | [2026-09-internal-review.md](2026-09-internal-review.md) | Done: every finding fixed or accepted |
| Before v1.0.0 | External | Authentication, sessions, tenancy, generator | Not yet | **Open:** `v1.0.0` awaits its sign-off |

A review is repeated before each major release ([ADR-0053](../adr/0053-internal-security-review.md)).

# ADR-0055: Governance and contribution

**Status:** Accepted (2026-09-16)

## Context

v1.0 must ship a governance and contribution guide (roadmap). Until now the repository said "not accepting code contributions yet", and the controls the threat model depends on had no written process behind them:

| Area | Today | Evidence |
|---|---|---|
| Contributions | "Not accepting code contributions yet" | `README.md` |
| Decisions | ADRs exist, but nothing says who accepts one or how | `docs/adr/README.md` |
| Security-sensitive review | Threat model row 7 asks for two-person approval; row 8 for `CODEOWNERS` and branch protection | `docs/adr/0029-threat-model.md` |
| Supported versions | "No released versions yet" | `SECURITY.md` |
| Changelog | None; the landing page links to a changelog page that doesn't exist | `gorbital-web/apps/landing/lib/content.ts` |
| Code of conduct | Contributor Covenant 2.1, with the contact address still a TODO | `CODE_OF_CONDUCT.md` |

## Options

| Option | Verdict |
|---|---|
| Benevolent dictator: one maintainer decides everything | Rejected: a single point of failure for security releases, and contradicts two-person approval |
| Foundation-style steering committee and votes | Rejected: too heavy for a project with a handful of maintainers |
| **Maintainers team, lazy consensus on decision records, two approvals for security-sensitive code** | **Chosen** |

## Decision

- `GOVERNANCE.md`: contributor, reviewer and maintainer roles; ADRs accepted by lazy consensus (a maintainer approves, no maintainer objects within five working days, majority vote if that fails); public-surface changes need an accepted ADR; security-sensitive paths need two approvals, one from a maintainer; releases only from CI.
- `CONTRIBUTING.md`: when to open an issue or ADR first, local setup, the golden app and template workflow, pull request checks, style. Inbound licence equals outbound (Apache 2.0); no contributor licence agreement.
- `.github/CODEOWNERS` with `@gorbital/maintainers` everywhere and `@gorbital/security` on authentication, organisations, HTTP core, rate limits, the generator, upgrades and workflows; a pull request template asking how existing apps receive the change (ADR-0016); issue forms that send vulnerability reports to private reporting.
- `SECURITY.md`: supported versions per ADR-0016 (latest minor of the current major; the previous major for 12 months).
- `CHANGELOG.md` in Keep a Changelog form, one section per release, linking upgrade notes and ADRs; published on the docs site.

## Why

Lazy consensus keeps decisions moving with few maintainers while leaving a record. Tying review rules to the paths the threat model names makes rows 7 and 8 enforceable by GitHub settings instead of memory.

## Trade-offs

- Two approvals slow security-sensitive changes.
- `CODEOWNERS` names teams that must exist in the GitHub organisation before it has effect.

## Consequences

- Maintainer actions before a public release: create the `maintainers` and `security` teams, enable branch protection requiring code owner review, and fill in the code of conduct contact address (threat model row 8).
- Every release adds a changelog section; every pull request answers "how do existing apps receive it".
